package agent

import (
	"context"
	"fmt"
	"mini-code/pkg/tools"
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
	tools.Executors[toolName] = func(args interface{}) (interface{}, error) {
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
