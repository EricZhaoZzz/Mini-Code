package agent

import (
	"context"
	"io"
	"mini-code/pkg/tools"
	"os"
	"strings"
	"testing"

	"github.com/sashabaranov/go-openai"
)

// captureStdout 捕获 fn 执行期间写入 os.Stdout 的内容。
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	done := make(chan string)
	go func() {
		data, _ := io.ReadAll(r)
		done <- string(data)
	}()
	defer func() { os.Stdout = orig }()
	fn()
	w.Close()
	return <-done
}

// 工具面板不能用光标上移去「清除」内容：旧实现实际擦掉的是工具列表，
// 且「完成」与「等待响应」挤在同一行。
func TestRunToolPanelLayout(t *testing.T) {
	const toolA, toolB = "test_display_a", "test_display_b"
	for _, name := range []string{toolA, toolB} {
		tools.Executors[name] = func(context.Context, interface{}) (interface{}, error) { return "ok", nil }
	}
	defer delete(tools.Executors, toolA)
	defer delete(tools.Executors, toolB)

	fake := &fakeChatCompletionClientBase{responses: []openai.ChatCompletionResponse{
		{Choices: []openai.ChatCompletionChoice{{Message: openai.ChatCompletionMessage{
			Role: openai.ChatMessageRoleAssistant,
			ToolCalls: []openai.ToolCall{
				{ID: "1", Type: openai.ToolTypeFunction, Function: openai.FunctionCall{Name: toolA, Arguments: `{"path":"a.go"}`}},
				{ID: "2", Type: openai.ToolTypeFunction, Function: openai.FunctionCall{Name: toolB, Arguments: `{"path":"b.go"}`}},
			},
		}}}},
		{Choices: []openai.ChatCompletionChoice{{
			Message:      openai.ChatCompletionMessage{Role: openai.ChatMessageRoleAssistant, Content: "done"},
			FinishReason: openai.FinishReasonStop,
		}}},
	}}

	out := captureStdout(t, func() {
		a := NewBaseAgent(fake, "m", nil)
		if _, _, err := a.Run(context.Background(), []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: "s"},
			{Role: openai.ChatMessageRoleUser, Content: "u"},
		}, nil); err != nil {
			t.Fatal(err)
		}
	})

	if strings.Contains(out, "\033[1A") {
		t.Error("不应使用光标上移")
	}
	var listA, listB bool
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "a.go") && strings.Contains(line, "b.go") {
			t.Errorf("工具列表挤在同一行: %q", line)
		}
		listA = listA || strings.Contains(line, "a.go")
		listB = listB || strings.Contains(line, "b.go")
		if strings.Contains(line, "完成") && strings.Contains(line, "等待响应") && !strings.Contains(line, "\r\033[K") {
			t.Errorf("「完成」与「等待响应」挤在同一行: %q", line)
		}
	}
	if !listA || !listB {
		t.Errorf("工具列表应保留在输出中:\n%s", out)
	}
	// 等待状态行必须在收到响应后被擦除
	if i := strings.LastIndex(out, "等待响应"); i >= 0 && !strings.Contains(out[i:], "\r\033[K") {
		t.Error("等待状态行未被擦除")
	}
}

// 静默模式（子 Agent）只输出单行进度，不画面板。
func TestRunQuietModeOutput(t *testing.T) {
	const tool = "test_display_quiet"
	tools.Executors[tool] = func(context.Context, interface{}) (interface{}, error) { return "ok", nil }
	defer delete(tools.Executors, tool)

	fake := &fakeChatCompletionClientBase{responses: []openai.ChatCompletionResponse{
		{Choices: []openai.ChatCompletionChoice{{Message: openai.ChatCompletionMessage{
			Role:      openai.ChatMessageRoleAssistant,
			ToolCalls: []openai.ToolCall{{ID: "1", Type: openai.ToolTypeFunction, Function: openai.FunctionCall{Name: tool, Arguments: `{"path":"x.go"}`}}},
		}}}},
		{Choices: []openai.ChatCompletionChoice{{
			Message:      openai.ChatCompletionMessage{Role: openai.ChatMessageRoleAssistant, Content: "done"},
			FinishReason: openai.FinishReasonStop,
		}}},
	}}

	out := captureStdout(t, func() {
		a := NewBaseAgent(fake, "m", nil)
		a.SetQuiet(true)
		a.Run(context.Background(), []openai.ChatCompletionMessage{{Role: openai.ChatMessageRoleUser, Content: "u"}}, nil)
	})

	if strings.Contains(out, "工具调用") || strings.Contains(out, "等待响应") {
		t.Errorf("静默模式不应绘制面板或状态行: %q", out)
	}
	if !strings.Contains(out, "x.go\n") {
		t.Errorf("静默模式应输出以换行结尾的单行进度: %q", out)
	}
}
