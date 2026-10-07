package agent

import (
	"strings"
	"testing"

	"github.com/sashabaranov/go-openai"
)

// buildMsgs 构造 n 轮对话（每轮 user+assistant），system 在首条。
func buildMsgs(n int, contentSize int) []openai.ChatCompletionMessage {
	body := strings.Repeat("x", contentSize)
	msgs := []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: "系统提示词"},
	}
	for i := 0; i < n; i++ {
		msgs = append(msgs,
			openai.ChatCompletionMessage{Role: openai.ChatMessageRoleUser, Content: body},
			openai.ChatCompletionMessage{Role: openai.ChatMessageRoleAssistant, Content: body},
		)
	}
	return msgs
}

func TestEstimateTokens(t *testing.T) {
	if got := EstimateTokens(""); got != 0 {
		t.Errorf("空串估算 = %d, 期望 0", got)
	}

	// ASCII 约 4 字符/token：26 字符应在 4..10 之间
	if got := EstimateTokens("abcdefghijklmnopqrstuvwxyz"); got < 4 || got > 10 {
		t.Errorf("ASCII 估算 = %d, 期望落在 4..10", got)
	}

	// CJK 约 1 字符/token
	if got := EstimateTokens(strings.Repeat("中", 100)); got < 70 || got > 130 {
		t.Errorf("CJK 估算 = %d, 期望落在 70..130", got)
	}

	// 混合文本应为正数
	if EstimateTokens("中文 mixed 内容 123") <= 0 {
		t.Error("混合文本估算应大于 0")
	}

	// 单字符也应至少记 1 token，避免空结果被当成 0
	if got := EstimateTokens("a"); got < 1 {
		t.Errorf("单字符估算 = %d, 期望 >= 1", got)
	}
}

func TestEstimateMessagesTokens(t *testing.T) {
	msgs := buildMsgs(10, 500)

	small := EstimateMessagesTokens(msgs)
	if small <= 0 {
		t.Fatalf("消息总估算应大于 0, 得到 %d", small)
	}

	// 消息更多 / 内容更长时估算应随之增长
	if larger := EstimateMessagesTokens(buildMsgs(20, 500)); larger <= small {
		t.Error("消息更多时估算未增长")
	}
	if bigger := EstimateMessagesTokens(buildMsgs(10, 1000)); bigger <= small {
		t.Error("内容更长时估算未增长")
	}

	// 空切片应为 0
	if got := EstimateMessagesTokens(nil); got != 0 {
		t.Errorf("空切片估算 = %d, 期望 0", got)
	}
}

func TestNeedsCompaction(t *testing.T) {
	msgs := buildMsgs(10, 500)

	if NeedsCompaction(msgs, DefaultContextTokenBudget) {
		t.Error("默认预算下短会话不应触发压缩")
	}
	// 预算设得极小则应触发
	if !NeedsCompaction(msgs, 10) {
		t.Error("预算极小时应触发压缩")
	}
	// budget <= 0 时使用默认值
	if NeedsCompaction(msgs, 0) {
		t.Error("budget=0 应回退默认值，短会话不应触发")
	}
}

func TestSplitMessagesForCompaction(t *testing.T) {
	msgs := buildMsgs(6, 100) // 1 system + 12 条

	t.Run("保留最近 keep 条", func(t *testing.T) {
		kept, dropped := SplitMessagesForCompaction(msgs, 4)
		if len(kept) != 1+4 {
			t.Errorf("保留数量 = %d, 期望 %d", len(kept), 1+4)
		}
		if kept[0].Role != openai.ChatMessageRoleSystem {
			t.Error("system 消息必须始终保留在头部")
		}
		if len(dropped) != 8 {
			t.Errorf("待压缩数量 = %d, 期望 8", len(dropped))
		}
		if kept[len(kept)-1].Content != msgs[len(msgs)-1].Content {
			t.Error("保留的应是最近的消息")
		}
		// 保持 = 1(待压缩) + 保留 数一致
		if len(kept)+len(dropped) != len(msgs) {
			t.Errorf("保持 + 压缩 = %d, 期望等于原长度 %d",
				len(kept)+len(dropped), len(msgs))
		}
	})

	t.Run("keep 足够大时不压缩", func(t *testing.T) {
		kept, dropped := SplitMessagesForCompaction(msgs, 100)
		if len(dropped) != 0 {
			t.Errorf("keep 足够大时不应有待压缩消息, 得到 %d", len(dropped))
		}
		if len(kept) != len(msgs) {
			t.Errorf("keep 足够大时应原样保留, 得到 %d 条", len(kept))
		}
	})

	t.Run("keep 为 0 时只保留 system", func(t *testing.T) {
		kept, dropped := SplitMessagesForCompaction(msgs, 0)
		if len(kept) != 1 {
			t.Errorf("keep=0 时只应保留 system, 得到 %d", len(kept))
		}
		if kept[0].Role != openai.ChatMessageRoleSystem {
			t.Error("保留的第一条应是 system")
		}
		if len(dropped) != len(msgs)-1 {
			t.Errorf("待压缩 = %d, 期望 %d", len(dropped), len(msgs)-1)
		}
	})

	t.Run("负数 keep 按 0 处理", func(t *testing.T) {
		kept, _ := SplitMessagesForCompaction(msgs, -5)
		if len(kept) != 1 {
			t.Errorf("负数 keep 时只应保留 system, 得到 %d", len(kept))
		}
	})

	t.Run("多条 system 全部保留", func(t *testing.T) {
		multi := []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: "sys1"},
			{Role: openai.ChatMessageRoleSystem, Content: "sys2"},
			{Role: openai.ChatMessageRoleUser, Content: "u1"},
			{Role: openai.ChatMessageRoleAssistant, Content: "a1"},
		}
		kept, dropped := SplitMessagesForCompaction(multi, 2)
		if len(kept) != 4 {
			t.Errorf("两条 system 应都在保留区, kept = %d", len(kept))
		}
		if len(dropped) != 0 {
			t.Errorf("keep=2 恰好覆盖全部, 不应有压缩项, 得到 %d", len(dropped))
		}
	})

	t.Run("空切片不 panic", func(t *testing.T) {
		kept, dropped := SplitMessagesForCompaction(nil, 4)
		if len(kept) != 0 || len(dropped) != 0 {
			t.Errorf("空切片应返回空, 得到 kept=%d dropped=%d", len(kept), len(dropped))
		}
	})
}

func TestBuildCompactionSummaryPrompt(t *testing.T) {
	dropped := []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleUser, Content: "帮我实现登录接口"},
		{Role: openai.ChatMessageRoleAssistant, Content: "已创建 pkg/auth/login.go"},
	}

	prompt := BuildCompactionSummaryPrompt(dropped)

	if !strings.Contains(prompt, "帮我实现登录接口") {
		t.Error("摘要提示词应包含待压缩的消息内容")
	}
	if !strings.Contains(prompt, "已创建 pkg/auth/login.go") {
		t.Error("摘要提示词应包含 assistant 消息内容")
	}
	if !strings.Contains(prompt, "摘要") {
		t.Error("摘要提示词应说明任务目标")
	}

	// 超长内容应被截断，避免提示词本身爆炸
	long := []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleUser, Content: strings.Repeat("x", 2000)},
	}
	if got := BuildCompactionSummaryPrompt(long); len(got) > 1000 {
		t.Errorf("超长内容未截断, 长度 %d", len(got))
	}
}

func TestTruncateSummary(t *testing.T) {
	if got := TruncateSummary("  简短摘要  "); got != "简短摘要" {
		t.Errorf("应去除首尾空白, 得到 %q", got)
	}

	long := strings.Repeat("字", MaxSummaryChars*2)
	got := TruncateSummary(long)
	runes := []rune(got)
	if len(runes) > MaxSummaryChars+1 { // +1 为省略号
		t.Errorf("截断后长度 = %d, 期望 <= %d", len(runes), MaxSummaryChars+1)
	}
	if !strings.HasSuffix(got, "…") {
		t.Error("截断后应带省略号")
	}

	// 恰好等于上限不应被截断
	exact := strings.Repeat("字", MaxSummaryChars)
	if TruncateSummary(exact) != exact {
		t.Error("恰好等于上限时不应被截断")
	}
}
