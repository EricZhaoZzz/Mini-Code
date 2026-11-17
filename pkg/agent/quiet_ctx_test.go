package agent

import (
	"context"
	"mini-code/pkg/tools"
	"testing"

	"github.com/sashabaranov/go-openai"
)

// 工具执行器必须拿到 Run 的 ctx，否则任务取消无法传到正在运行的工具。
func TestBaseAgent_RunPassesContextToTools(t *testing.T) {
	type key struct{}
	const toolName = "test_ctx_probe"
	var got any
	tools.Executors[toolName] = func(ctx context.Context, _ interface{}) (interface{}, error) {
		got = ctx.Value(key{})
		return "ok", nil
	}
	defer delete(tools.Executors, toolName)

	fake := &fakeChatCompletionClientBase{responses: []openai.ChatCompletionResponse{
		{Choices: []openai.ChatCompletionChoice{{Message: openai.ChatCompletionMessage{
			Role: openai.ChatMessageRoleAssistant,
			ToolCalls: []openai.ToolCall{{ID: "c1", Type: openai.ToolTypeFunction,
				Function: openai.FunctionCall{Name: toolName, Arguments: `{}`}}},
		}}}},
		{Choices: []openai.ChatCompletionChoice{{
			Message:      openai.ChatCompletionMessage{Role: openai.ChatMessageRoleAssistant, Content: "done"},
			FinishReason: openai.FinishReasonStop,
		}}},
	}}

	a := NewBaseAgent(fake, "m", nil)
	a.SetQuiet(true)
	ctx := context.WithValue(context.Background(), key{}, "run-ctx")
	reply, _, err := a.Run(ctx, []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: "s"},
		{Role: openai.ChatMessageRoleUser, Content: "u"},
	}, nil)
	if err != nil || reply != "done" {
		t.Fatalf("reply=%q err=%v", reply, err)
	}
	if got != "run-ctx" {
		t.Errorf("工具未收到 Run 的 ctx, 得到 %v", got)
	}
}

func TestToolArgsSummary(t *testing.T) {
	cases := map[string]string{
		`{"path":"a/b.go"}`:    "a/b.go",
		`{"query":"hello"}`:    "hello",
		`{"command":"go vet"}`: "go vet",
		`{"other":1}`:          "",
		`not json`:             "",
	}
	for in, want := range cases {
		if got := toolArgsSummary(in); got != want {
			t.Errorf("toolArgsSummary(%s) = %q, want %q", in, got, want)
		}
	}
}
