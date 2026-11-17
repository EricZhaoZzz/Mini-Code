package agent

import (
	"strings"
	"testing"

	"github.com/sashabaranov/go-openai"
)

func toolRound(id, args, result string) []openai.ChatCompletionMessage {
	return []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleAssistant, ToolCalls: []openai.ToolCall{{
			ID: id, Type: openai.ToolTypeFunction,
			Function: openai.FunctionCall{Name: "read_file", Arguments: args},
		}}},
		{Role: openai.ChatMessageRoleTool, ToolCallID: id, Content: result},
	}
}

// 保留区不能以孤立的 tool 消息开头，否则 API 返回 400。
func TestSplitMessagesForCompaction_KeepsToolPairsTogether(t *testing.T) {
	msgs := []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: "sys"},
		{Role: openai.ChatMessageRoleUser, Content: "u1"},
	}
	msgs = append(msgs, toolRound("c1", `{}`, "r1")...)
	msgs = append(msgs, openai.ChatCompletionMessage{Role: openai.ChatMessageRoleAssistant, Content: "a1"})
	// rest = [u1, asst(tc), tool, a1]；keep=2 原本会从 tool 开始切
	kept, dropped := SplitMessagesForCompaction(msgs, 2)

	if kept[1].Role == openai.ChatMessageRoleTool {
		t.Fatal("保留区以孤立的 tool 消息开头")
	}
	if kept[1].Role != openai.ChatMessageRoleAssistant || len(kept[1].ToolCalls) == 0 {
		t.Errorf("应回退到发起调用的 assistant，实际首条为 %+v", kept[1])
	}
	if len(kept)+len(dropped) != len(msgs) {
		t.Errorf("kept+dropped = %d, 期望 %d", len(kept)+len(dropped), len(msgs))
	}
}

// 多个并行 tool 结果时，回退要跨过全部 tool 消息。
func TestSplitMessagesForCompaction_MultipleToolResults(t *testing.T) {
	msgs := []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: "sys"},
		{Role: openai.ChatMessageRoleUser, Content: "u1"},
		{Role: openai.ChatMessageRoleAssistant, ToolCalls: []openai.ToolCall{
			{ID: "a", Function: openai.FunctionCall{Name: "x"}},
			{ID: "b", Function: openai.FunctionCall{Name: "y"}},
		}},
		{Role: openai.ChatMessageRoleTool, ToolCallID: "a", Content: "ra"},
		{Role: openai.ChatMessageRoleTool, ToolCallID: "b", Content: "rb"},
		{Role: openai.ChatMessageRoleAssistant, Content: "done"},
	}
	kept, dropped := SplitMessagesForCompaction(msgs, 2) // 原切点落在第二条 tool
	if kept[1].Role != openai.ChatMessageRoleAssistant || len(kept[1].ToolCalls) != 2 {
		t.Fatalf("应保留完整调用组，实际首条 %+v", kept[1])
	}
	if len(dropped) != 1 {
		t.Errorf("dropped = %d, 期望 1", len(dropped))
	}
}

// 回退到头仍找不到发起者（历史本身已残缺）时，向后跳过孤立 tool。
func TestSplitMessagesForCompaction_OrphanToolsSkippedForward(t *testing.T) {
	msgs := []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: "sys"},
		{Role: openai.ChatMessageRoleTool, ToolCallID: "x", Content: "orphan1"},
		{Role: openai.ChatMessageRoleTool, ToolCallID: "y", Content: "orphan2"},
		{Role: openai.ChatMessageRoleUser, Content: "u"},
		{Role: openai.ChatMessageRoleAssistant, Content: "a"},
	}
	kept, _ := SplitMessagesForCompaction(msgs, 3)
	for _, m := range kept[1:] {
		if m.Role == openai.ChatMessageRoleTool {
			t.Fatalf("保留区不应包含孤立 tool: %+v", kept)
		}
	}
}

func TestEstimateMessagesTokens_CountsToolCallArguments(t *testing.T) {
	bigArgs := `{"path":"a.go","content":"` + strings.Repeat("x", 40_000) + `"}`
	withArgs := toolRound("c1", bigArgs, "ok")
	withoutArgs := toolRound("c1", `{}`, "ok")
	diff := EstimateMessagesTokens(withArgs) - EstimateMessagesTokens(withoutArgs)
	if diff < 9_000 {
		t.Errorf("工具调用参数未计入估算，差值仅 %d", diff)
	}
}

func TestBuildCompactionSummaryPrompt_UTF8AndToolCalls(t *testing.T) {
	dropped := []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleUser, Content: strings.Repeat("中", 600)},
	}
	dropped = append(dropped, toolRound("c1", `{"path":"pkg/foo.go"}`, "ok")...)
	prompt := BuildCompactionSummaryPrompt(dropped)

	if !strings.Contains(prompt, "pkg/foo.go") || !strings.Contains(prompt, "read_file") {
		t.Error("摘要提示词应包含工具调用名与参数")
	}
	if strings.ContainsRune(prompt, '�') {
		t.Error("截断破坏了 UTF-8 字符")
	}
}

func TestPruneToolResults(t *testing.T) {
	big := strings.Repeat("y", 4_000) // ≈1000 token
	msgs := []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: "sys"},
		{Role: openai.ChatMessageRoleUser, Content: "u"},
	}
	msgs = append(msgs, toolRound("c1", `{}`, big)...)
	msgs = append(msgs, toolRound("c2", `{}`, big)...)
	msgs = append(msgs, toolRound("c3", `{}`, big)...)

	t.Run("预算内原样返回", func(t *testing.T) {
		out := PruneToolResults(msgs, 1_000_000)
		if &out[0] != &msgs[0] {
			t.Error("预算内不应复制")
		}
	})

	t.Run("超预算从最早开始省略且保护最新一轮", func(t *testing.T) {
		out := PruneToolResults(msgs, 2_200)
		if out[3].Content != prunedToolResultPlaceholder {
			t.Error("最早的工具结果应被省略")
		}
		if out[7].Content != big {
			t.Error("最新一轮工具结果不应被省略")
		}
		if EstimateMessagesTokens(out) > 2_200 {
			t.Errorf("省略后仍超预算: %d", EstimateMessagesTokens(out))
		}
		if msgs[3].Content != big {
			t.Error("不应修改入参")
		}
		if len(out) != len(msgs) || out[3].ToolCallID != "c1" {
			t.Error("消息结构必须保持不变")
		}
	})
}

func TestCapToolResult(t *testing.T) {
	if s := capToolResult("short"); s != "short" {
		t.Errorf("短结果应原样返回, 得到 %q", s)
	}
	long := "HEAD" + strings.Repeat("中", MaxToolResultChars*2) + "TAIL"
	out := capToolResult(long)
	if !strings.HasPrefix(out, "HEAD") || !strings.HasSuffix(out, "TAIL") {
		t.Error("应保留头尾")
	}
	if !strings.Contains(out, "已省略中间") {
		t.Error("应包含省略提示")
	}
	if n := len([]rune(out)); n > MaxToolResultChars+200 {
		t.Errorf("截断后长度 %d 超出上限", n)
	}
}
