package agent

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"mini-claw/pkg/tools"

	"github.com/sashabaranov/go-openai"
)

type fakeChatCompletionClient struct {
	responses []openai.ChatCompletionResponse
	requests  []openai.ChatCompletionRequest
	err       error
}

func (f *fakeChatCompletionClient) CreateChatCompletion(_ context.Context, request openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error) {
	f.requests = append(f.requests, request)

	if f.err != nil {
		return openai.ChatCompletionResponse{}, f.err
	}

	if len(f.responses) == 0 {
		return openai.ChatCompletionResponse{}, errors.New("unexpected CreateChatCompletion call")
	}

	resp := f.responses[0]
	f.responses = f.responses[1:]
	return resp, nil
}

func (f *fakeChatCompletionClient) CreateChatCompletionStream(_ context.Context, request openai.ChatCompletionRequest) (*openai.ChatCompletionStream, error) {
	return nil, errors.New("CreateChatCompletionStream not implemented in fake client")
}

func TestRunTurnExecutesToolCallAndContinuesConversation(t *testing.T) {
	const toolName = "test_echo_engine"

	originalExecutor, hadOriginal := tools.Executors[toolName]
	tools.Executors[toolName] = func(args interface{}) (interface{}, error) {
		if args != `{"value":"ok"}` {
			t.Fatalf("unexpected tool args: %#v", args)
		}
		return "tool-output", nil
	}
	defer func() {
		if hadOriginal {
			tools.Executors[toolName] = originalExecutor
			return
		}
		delete(tools.Executors, toolName)
	}()

	fakeClient := &fakeChatCompletionClient{
		responses: []openai.ChatCompletionResponse{
			{
				Choices: []openai.ChatCompletionChoice{
					{
						Message: openai.ChatCompletionMessage{
							Role: openai.ChatMessageRoleAssistant,
							ToolCalls: []openai.ToolCall{
								{
									ID:   "call-1",
									Type: openai.ToolTypeFunction,
									Function: openai.FunctionCall{
										Name:      toolName,
										Arguments: `{"value":"ok"}`,
									},
								},
							},
						},
					},
				},
			},
			{
				Choices: []openai.ChatCompletionChoice{
					{
						Message: openai.ChatCompletionMessage{
							Role:    openai.ChatMessageRoleAssistant,
							Content: "任务完成",
						},
					},
				},
			},
		},
	}

	engine := NewClawEngine(fakeClient, "test-model")
	engine.debugLogger = nil
	engine.AddUserMessage("请执行测试工具")

	reply, err := engine.RunTurn(context.Background())
	if err != nil {
		t.Fatalf("RunTurn returned error: %v", err)
	}
	if reply != "任务完成" {
		t.Fatalf("expected final reply to be %q, got %q", "任务完成", reply)
	}

	if len(fakeClient.requests) != 2 {
		t.Fatalf("expected 2 chat completion requests, got %d", len(fakeClient.requests))
	}

	secondRequest := fakeClient.requests[1]
	if len(secondRequest.Messages) < 4 {
		t.Fatalf("expected tool result to be appended before second request, got %d messages", len(secondRequest.Messages))
	}

	toolMessage := secondRequest.Messages[len(secondRequest.Messages)-1]
	if toolMessage.Role != openai.ChatMessageRoleTool {
		t.Fatalf("expected last message role to be tool, got %q", toolMessage.Role)
	}
	if toolMessage.Content != "tool-output" {
		t.Fatalf("expected tool output to be %q, got %q", "tool-output", toolMessage.Content)
	}
	if toolMessage.ToolCallID != "call-1" {
		t.Fatalf("expected tool call id to be %q, got %q", "call-1", toolMessage.ToolCallID)
	}
}

func TestRunTurnPassesUnknownToolErrorBackToModel(t *testing.T) {
	fakeClient := &fakeChatCompletionClient{
		responses: []openai.ChatCompletionResponse{
			{
				Choices: []openai.ChatCompletionChoice{
					{
						Message: openai.ChatCompletionMessage{
							Role: openai.ChatMessageRoleAssistant,
							ToolCalls: []openai.ToolCall{
								{
									ID:   "call-missing",
									Type: openai.ToolTypeFunction,
									Function: openai.FunctionCall{
										Name:      "missing_tool",
										Arguments: `{}`,
									},
								},
							},
						},
					},
				},
			},
			{
				Choices: []openai.ChatCompletionChoice{
					{
						Message: openai.ChatCompletionMessage{
							Role:    openai.ChatMessageRoleAssistant,
							Content: "我已收到工具错误并停止调用",
						},
					},
				},
			},
		},
	}

	engine := NewClawEngine(fakeClient, "test-model")
	engine.debugLogger = nil
	engine.AddUserMessage("调用一个不存在的工具")

	reply, err := engine.RunTurn(context.Background())
	if err != nil {
		t.Fatalf("RunTurn returned error: %v", err)
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

func TestRunTurnConcurrentToolCallsPreserveOrder(t *testing.T) {
	// 测试并发工具调用时结果顺序保持正确
	var callOrder []string
	var mu sync.Mutex

	// 注册三个测试工具
	toolsToRegister := []string{"tool_a", "tool_b", "tool_c"}
	for _, name := range toolsToRegister {
		toolName := name
		tools.Executors[toolName] = func(args interface{}) (interface{}, error) {
			mu.Lock()
			callOrder = append(callOrder, toolName)
			mu.Unlock()
			return toolName + "_result", nil
		}
	}
	defer func() {
		for _, name := range toolsToRegister {
			delete(tools.Executors, name)
		}
	}()

	fakeClient := &fakeChatCompletionClient{
		responses: []openai.ChatCompletionResponse{
			{
				Choices: []openai.ChatCompletionChoice{
					{
						Message: openai.ChatCompletionMessage{
							Role: openai.ChatMessageRoleAssistant,
							ToolCalls: []openai.ToolCall{
								{ID: "call-1", Type: openai.ToolTypeFunction, Function: openai.FunctionCall{Name: "tool_a", Arguments: `{}`}},
								{ID: "call-2", Type: openai.ToolTypeFunction, Function: openai.FunctionCall{Name: "tool_b", Arguments: `{}`}},
								{ID: "call-3", Type: openai.ToolTypeFunction, Function: openai.FunctionCall{Name: "tool_c", Arguments: `{}`}},
							},
						},
					},
				},
			},
			{
				Choices: []openai.ChatCompletionChoice{
					{
						Message: openai.ChatCompletionMessage{
							Role:    openai.ChatMessageRoleAssistant,
							Content: "done",
						},
					},
				},
			},
		},
	}

	engine := NewClawEngine(fakeClient, "test-model")
	engine.debugLogger = nil
	engine.AddUserMessage("test concurrent")

	reply, err := engine.RunTurn(context.Background())
	if err != nil {
		t.Fatalf("RunTurn returned error: %v", err)
	}
	if reply != "done" {
		t.Fatalf("expected reply %q, got %q", "done", reply)
	}

	// 验证消息顺序正确（工具结果必须按原始顺序添加）
	secondRequest := fakeClient.requests[1]
	toolMessages := []openai.ChatCompletionMessage{}
	for _, msg := range secondRequest.Messages {
		if msg.Role == openai.ChatMessageRoleTool {
			toolMessages = append(toolMessages, msg)
		}
	}

	if len(toolMessages) != 3 {
		t.Fatalf("expected 3 tool messages, got %d", len(toolMessages))
	}

	// 验证顺序：tool_a, tool_b, tool_c
	expectedOrder := []string{"tool_a_result", "tool_b_result", "tool_c_result"}
	for i, expected := range expectedOrder {
		if toolMessages[i].Content != expected {
			t.Errorf("tool message %d: expected %q, got %q", i, expected, toolMessages[i].Content)
		}
	}

	// 验证工具调用 ID 顺序正确
	expectedIDs := []string{"call-1", "call-2", "call-3"}
	for i, expectedID := range expectedIDs {
		if toolMessages[i].ToolCallID != expectedID {
			t.Errorf("tool message %d: expected ToolCallID %q, got %q", i, expectedID, toolMessages[i].ToolCallID)
		}
	}
}
