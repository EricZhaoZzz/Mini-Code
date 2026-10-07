package orchestrator

import (
	"context"
	"os"
	"strings"
	"testing"

	"mini-code/pkg/agent"

	"github.com/sashabaranov/go-openai"
)

// fakeSummarizerProvider 返回固定摘要的假 provider。
type fakeSummarizerProvider struct {
	calls int
	reply string
	err   error
}

func (f *fakeSummarizerProvider) Model() string { return "fake" }

func (f *fakeSummarizerProvider) Chat(_ context.Context, _ openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error) {
	f.calls++
	if f.err != nil {
		return openai.ChatCompletionResponse{}, f.err
	}
	return openai.ChatCompletionResponse{
		Choices: []openai.ChatCompletionChoice{
			{Message: openai.ChatCompletionMessage{Content: f.reply}},
		},
	}, nil
}

func (f *fakeSummarizerProvider) ChatStream(context.Context, openai.ChatCompletionRequest) (*openai.ChatCompletionStream, error) {
	return nil, nil
}

// fillSession 用大消息填满 Session，触发压缩。
func fillSession(s *Session, rounds, size int) {
	for i := 0; i < rounds; i++ {
		s.AppendUserMessage(strings.Repeat("u", size))
		s.AppendMessages([]openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleAssistant, Content: strings.Repeat("a", size)},
		})
	}
}

// TestCompactIfNeeded_CompressesWhenOverBudget 验证超限时压缩历史。
func TestCompactIfNeeded_CompressesWhenOverBudget(t *testing.T) {
	// 把预算压到很小，强制触发压缩
	t.Setenv("LM_CONTEXT_BUDGET", "200")

	o := New(nil)
	defer o.Close()

	p := &fakeSummarizerProvider{reply: "早期完成了文件工具的沙箱修复"}
	o.SetProvider(p, "fake")

	s := o.GetOrCreateSession("cli", "local")
	fillSession(s, 20, 500)

	before := s.MessageCount()
	o.compactIfNeeded(context.Background(), s)
	after := s.MessageCount()

	if p.calls != 1 {
		t.Errorf("摘要调用次数 = %d, 期望 1", p.calls)
	}
	if after >= before {
		t.Errorf("压缩后消息数 = %d, 应小于压缩前 %d", after, before)
	}

	// system 应仍在首位，且紧随其后是摘要
	msgs := s.Messages()
	if msgs[0].Role != openai.ChatMessageRoleSystem {
		t.Errorf("首条应为 system, 得到 %q", msgs[0].Role)
	}
	if len(msgs) < 2 || !strings.Contains(msgs[1].Content, "历史对话摘要") {
		t.Errorf("第二条应为历史摘要, 得到 %+v", msgs)
	}
	if !strings.Contains(msgs[1].Content, "沙箱修复") {
		t.Errorf("摘要内容应来自 provider 返回, 得到 %q", msgs[1].Content)
	}
	if s.Summary() != "早期完成了文件工具的沙箱修复" {
		t.Errorf("摘要未写回 Session, 得到 %q", s.Summary())
	}

	// 压缩后仍应保留最近的原文
	if !strings.Contains(msgs[len(msgs)-1].Content, "a") {
		t.Error("压缩后应仍保留最近的原文消息")
	}
}

// TestCompactIfNeeded_SkipsWhenWithinBudget 验证未超限时不压缩。
func TestCompactIfNeeded_SkipsWhenWithinBudget(t *testing.T) {
	t.Setenv("LM_CONTEXT_BUDGET", "1000000")

	o := New(nil)
	defer o.Close()

	p := &fakeSummarizerProvider{reply: "不应被调用"}
	o.SetProvider(p, "fake")

	s := o.GetOrCreateSession("cli", "local")
	s.AppendUserMessage("短消息")

	before := s.MessageCount()
	o.compactIfNeeded(context.Background(), s)

	if p.calls != 0 {
		t.Errorf("未超限时不应调用摘要, 调用了 %d 次", p.calls)
	}
	if s.MessageCount() != before {
		t.Errorf("未超限时消息数不应变化: %d -> %d", before, s.MessageCount())
	}
}

// TestCompactIfNeeded_NoProviderSilentlySkips 未注入 provider 时应跳过而非报错。
func TestCompactIfNeeded_NoProviderSilentlySkips(t *testing.T) {
	t.Setenv("LM_CONTEXT_BUDGET", "100")

	o := New(nil) // 未调用 SetProvider
	defer o.Close()

	s := o.GetOrCreateSession("cli", "local")
	fillSession(s, 10, 500)

	before := s.MessageCount()
	o.compactIfNeeded(context.Background(), s) // 不应 panic

	if s.MessageCount() != before {
		t.Error("未注入 provider 时不应修改消息历史")
	}
}

// TestCompactIfNeeded_SummaryFailureKeepsHistory 摘要失败时保留原历史。
func TestCompactIfNeeded_SummaryFailureKeepsHistory(t *testing.T) {
	t.Setenv("LM_CONTEXT_BUDGET", "200")

	o := New(nil)
	defer o.Close()

	p := &fakeSummarizerProvider{err: context.DeadlineExceeded}
	o.SetProvider(p, "fake")

	s := o.GetOrCreateSession("cli", "local")
	fillSession(s, 20, 500)

	before := s.MessageCount()
	o.compactIfNeeded(context.Background(), s)

	if s.MessageCount() != before {
		t.Error("摘要失败时不应丢弃历史（压缩失败不应阻塞主流程）")
	}
}

// TestCompactIfNeeded_ReusesSummary 摘要应跨轮复用，避免每轮重新生成。
func TestCompactIfNeeded_ReusesSummary(t *testing.T) {
	t.Setenv("LM_CONTEXT_BUDGET", "200")

	o := New(nil)
	defer o.Close()

	p := &fakeSummarizerProvider{reply: "第一轮摘要"}
	o.SetProvider(p, "fake")

	s := o.GetOrCreateSession("cli", "local")
	fillSession(s, 20, 500)

	o.compactIfNeeded(context.Background(), s)
	if p.calls != 1 {
		t.Fatalf("首次压缩调用 %d 次摘要, 期望 1", p.calls)
	}

	// 第二轮：已有摘要，应把旧摘要并入新摘要请求
	p.reply = "第二轮摘要"
	fillSession(s, 20, 500)
	o.compactIfNeeded(context.Background(), s)

	if p.calls != 2 {
		t.Errorf("第二轮应再调用 1 次摘要, 累计 %d 次", p.calls)
	}
	if s.Summary() != "第二轮摘要" {
		t.Errorf("摘要应被更新, 得到 %q", s.Summary())
	}
}

// TestSession_ReplaceMessagesAndSummary 覆盖新增的 Session 方法。
func TestSession_ReplaceMessagesAndSummary(t *testing.T) {
	s := newSession("cli", "local", "原始系统提示")

	s.AppendUserMessage("u1")
	s.AppendUserMessage("u2")

	// Summary 默认应为空
	if s.Summary() != "" {
		t.Errorf("初始摘要应为空, 得到 %q", s.Summary())
	}

	s.SetSummary("摘要内容")
	if s.Summary() != "摘要内容" {
		t.Errorf("摘要未生效: %q", s.Summary())
	}

	// ReplaceMessages 应整体替换
	replacement := []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: "新系统提示"},
		{Role: openai.ChatMessageRoleUser, Content: "u1"},
	}
	s.ReplaceMessages(replacement)

	msgs := s.Messages()
	if len(msgs) != 2 {
		t.Fatalf("消息数 = %d, 期望 2", len(msgs))
	}
	if msgs[0].Content != "新系统提示" {
		t.Errorf("替换后首条 = %q", msgs[0].Content)
	}

	// 传入的切片不应被持有（避免外部修改影响内部状态）
	replacement[0].Content = "被外部篡改"
	if s.Messages()[0].Content != "新系统提示" {
		t.Error("ReplaceMessages 应复制切片，不能持有调用方的引用")
	}

	// Reset 应同时清空摘要
	s.Reset()
	if s.Summary() != "" {
		t.Errorf("Reset 后摘要应清空, 得到 %q", s.Summary())
	}
}

// TestContextBudget 验证预算读取与回退。
func TestContextBudget(t *testing.T) {
	original := agent.DefaultContextTokenBudget

	t.Run("默认预算", func(t *testing.T) {
		os.Unsetenv("LM_CONTEXT_BUDGET")
		if got := contextBudget(); got != original {
			t.Errorf("默认预算 = %d, 期望 %d", got, original)
		}
	})

	t.Run("环境变量覆盖", func(t *testing.T) {
		t.Setenv("LM_CONTEXT_BUDGET", "12345")
		if got := contextBudget(); got != 12345 {
			t.Errorf("覆盖预算 = %d, 期望 12345", got)
		}
	})

	t.Run("非法值回退默认", func(t *testing.T) {
		for _, v := range []string{"abc", "0", "-100"} {
			t.Setenv("LM_CONTEXT_BUDGET", v)
			if got := contextBudget(); got != original {
				t.Errorf("非法值 %q 得到 %d, 期望回退 %d", v, got, original)
			}
		}
	})
}
