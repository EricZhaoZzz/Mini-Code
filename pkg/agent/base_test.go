package agent

import (
	"context"
	"fmt"
	"mini-code/pkg/tools"
	"strings"
	"sync"
	"testing"

	"github.com/sashabaranov/go-openai"
)

type fakeChatCompletionClientBase struct {
	responses []openai.ChatCompletionResponse
	requests  []openai.ChatCompletionRequest
	err       error
}

func (f *fakeChatCompletionClientBase) Chat(_ context.Context, req openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error) {
	f.requests = append(f.requests, req)
	if f.err != nil {
		return openai.ChatCompletionResponse{}, f.err
	}
	if len(f.responses) == 0 {
		return openai.ChatCompletionResponse{}, fmt.Errorf("unexpected call")
	}
	resp := f.responses[0]
	f.responses = f.responses[1:]
	return resp, nil
}

func (f *fakeChatCompletionClientBase) ChatStream(_ context.Context, _ openai.ChatCompletionRequest) (*openai.ChatCompletionStream, error) {
	// 测试时传 handler=nil，走非流式路径，不调用此方法
	return nil, fmt.Errorf("streaming not supported in fake")
}

func TestBaseAgent_RunExecutesToolAndContinues(t *testing.T) {
	const toolName = "test_echo_base"
	tools.Executors[toolName] = func(_ context.Context, args interface{}) (interface{}, error) {
		return "tool-output", nil
	}
	defer delete(tools.Executors, toolName)

	fakeClient := &fakeChatCompletionClientBase{
		responses: []openai.ChatCompletionResponse{
			{Choices: []openai.ChatCompletionChoice{{
				Message: openai.ChatCompletionMessage{
					Role: openai.ChatMessageRoleAssistant,
					ToolCalls: []openai.ToolCall{{
						ID: "call-1", Type: openai.ToolTypeFunction,
						Function: openai.FunctionCall{Name: toolName, Arguments: `{}`},
					}},
				},
			}}},
			{Choices: []openai.ChatCompletionChoice{{
				Message:      openai.ChatCompletionMessage{Role: openai.ChatMessageRoleAssistant, Content: "完成"},
				FinishReason: openai.FinishReasonStop,
			}}},
		},
	}

	a := NewBaseAgent(fakeClient, "test-model", nil)
	messages := []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: "你是助手"},
		{Role: openai.ChatMessageRoleUser, Content: "执行工具"},
	}

	// handler=nil → 非流式路径
	reply, newMsgs, err := a.Run(context.Background(), messages, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reply != "完成" {
		t.Errorf("expected reply '完成', got %q", reply)
	}
	// newMsgs: assistant(toolcall) + tool(result) + assistant(完成) = 3
	if len(newMsgs) != 3 {
		t.Errorf("expected 3 new messages, got %d", len(newMsgs))
	}
}

func TestBaseAgent_RunPassesUnknownToolErrorBackToModel(t *testing.T) {
	fakeClient := &fakeChatCompletionClientBase{
		responses: []openai.ChatCompletionResponse{
			{Choices: []openai.ChatCompletionChoice{{
				Message: openai.ChatCompletionMessage{
					Role: openai.ChatMessageRoleAssistant,
					ToolCalls: []openai.ToolCall{{
						ID:   "call-missing",
						Type: openai.ToolTypeFunction,
						Function: openai.FunctionCall{
							Name:      "missing_tool",
							Arguments: `{}`,
						},
					}},
				},
			}}},
			{Choices: []openai.ChatCompletionChoice{{
				Message: openai.ChatCompletionMessage{
					Role:    openai.ChatMessageRoleAssistant,
					Content: "我已收到工具错误并停止调用",
				},
			}}},
		},
	}

	a := NewBaseAgent(fakeClient, "test-model", nil)
	a.debugLogger = nil
	messages := []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: "你是助手"},
		{Role: openai.ChatMessageRoleUser, Content: "调用一个不存在的工具"},
	}

	reply, _, err := a.Run(context.Background(), messages, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reply != "我已收到工具错误并停止调用" {
		t.Fatalf("unexpected final reply: %q", reply)
	}

	if len(fakeClient.requests) != 2 {
		t.Fatalf("expected 2 chat completion requests, got %d", len(fakeClient.requests))
	}

	secondRequest := fakeClient.requests[1]
	toolMessage := secondRequest.Messages[len(secondRequest.Messages)-1]
	if toolMessage.Role != openai.ChatMessageRoleTool {
		t.Fatalf("expected last message role to be tool, got %q", toolMessage.Role)
	}
	if !strings.Contains(toolMessage.Content, "未知工具 missing_tool") {
		t.Fatalf("expected tool error message to mention missing tool, got %q", toolMessage.Content)
	}
}

func TestBaseAgent_RunConcurrentToolCallsPreserveOrder(t *testing.T) {
	var mu sync.Mutex
	var executed []string

	toolsToRegister := []string{"tool_a", "tool_b", "tool_c"}
	for _, name := range toolsToRegister {
		toolName := name
		tools.Executors[toolName] = func(_ context.Context, args interface{}) (interface{}, error) {
			mu.Lock()
			executed = append(executed, toolName)
			mu.Unlock()
			return toolName + "_result", nil
		}
	}
	defer func() {
		for _, name := range toolsToRegister {
			delete(tools.Executors, name)
		}
	}()

	fakeClient := &fakeChatCompletionClientBase{
		responses: []openai.ChatCompletionResponse{
			{Choices: []openai.ChatCompletionChoice{{
				Message: openai.ChatCompletionMessage{
					Role: openai.ChatMessageRoleAssistant,
					ToolCalls: []openai.ToolCall{
						{ID: "call-1", Type: openai.ToolTypeFunction, Function: openai.FunctionCall{Name: "tool_a", Arguments: `{}`}},
						{ID: "call-2", Type: openai.ToolTypeFunction, Function: openai.FunctionCall{Name: "tool_b", Arguments: `{}`}},
						{ID: "call-3", Type: openai.ToolTypeFunction, Function: openai.FunctionCall{Name: "tool_c", Arguments: `{}`}},
					},
				},
			}}},
			{Choices: []openai.ChatCompletionChoice{{
				Message:      openai.ChatCompletionMessage{Role: openai.ChatMessageRoleAssistant, Content: "done"},
				FinishReason: openai.FinishReasonStop,
			}}},
		},
	}

	a := NewBaseAgent(fakeClient, "test-model", nil)
	a.debugLogger = nil
	messages := []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: "你是助手"},
		{Role: openai.ChatMessageRoleUser, Content: "test concurrent"},
	}

	reply, _, err := a.Run(context.Background(), messages, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reply != "done" {
		t.Fatalf("expected reply %q, got %q", "done", reply)
	}

	// 三个工具都应被执行
	mu.Lock()
	executedCount := len(executed)
	mu.Unlock()
	if executedCount != 3 {
		t.Errorf("expected 3 tools executed, got %d", executedCount)
	}

	// 验证结果消息顺序正确（工具结果必须按原始顺序追加）
	secondRequest := fakeClient.requests[1]
	var toolMessages []openai.ChatCompletionMessage
	for _, msg := range secondRequest.Messages {
		if msg.Role == openai.ChatMessageRoleTool {
			toolMessages = append(toolMessages, msg)
		}
	}

	if len(toolMessages) != 3 {
		t.Fatalf("expected 3 tool messages, got %d", len(toolMessages))
	}

	expectedOrder := []string{"tool_a_result", "tool_b_result", "tool_c_result"}
	expectedIDs := []string{"call-1", "call-2", "call-3"}
	for i, expected := range expectedOrder {
		if toolMessages[i].Content != expected {
			t.Errorf("tool message %d: expected %q, got %q", i, expected, toolMessages[i].Content)
		}
		if toolMessages[i].ToolCallID != expectedIDs[i] {
			t.Errorf("tool message %d: expected ToolCallID %q, got %q", i, expectedIDs[i], toolMessages[i].ToolCallID)
		}
	}
}
