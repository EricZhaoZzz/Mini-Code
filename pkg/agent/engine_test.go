package agent

import (
	"context"
	"errors"
	"strings"
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
