package agent

import (
	"strings"
	"unicode"

	"github.com/sashabaranov/go-openai"
)

const (
	// DefaultContextTokenBudget 默认上下文预算（token）。
	//
	// 多数模型窗口为 128k，但需为模型响应预留空间，故按 96k 规划。
	// 可用 LM_CONTEXT_BUDGET 覆盖。
	DefaultContextTokenBudget = 96_000

	// DefaultKeepRecentTurns 压缩时保留的最近消息条数。
	DefaultKeepRecentTurns = 12

	// MaxSummaryChars 摘要最大字符数。
	MaxSummaryChars = 800

	// compactSourcePreviewChars 构造摘要提示词时，每条消息的预览长度上限。
	compactSourcePreviewChars = 500
)

// EstimateTokens 粗略估算 token 数。
//
// 英文约 4 字符/token，CJK 约 1 字符/token。不追求精确，
// 只用于判断是否逼近窗口上限，因此宁可高估也不要低估。
func EstimateTokens(s string) int {
	if s == "" {
		return 0
	}

	var ascii, cjk int
	for _, r := range s {
		switch {
		case unicode.Is(unicode.Han, r),
			unicode.Is(unicode.Hiragana, r),
			unicode.Is(unicode.Katakana, r),
			unicode.Is(unicode.Hangul, r):
			cjk++
		default:
			ascii++
		}
	}

	tokens := cjk + ascii/4
	if tokens < 1 {
		// 非空串至少记 1 token，避免「有内容但估算为 0」被误判为无需压缩
		return 1
	}
	return tokens
}

// EstimateMessagesTokens 估算整条消息列表的 token 总量。
// 每条消息额外计入固定开销（role、分隔符等）。
func EstimateMessagesTokens(msgs []openai.ChatCompletionMessage) int {
	total := 0
	for _, m := range msgs {
		total += EstimateTokens(m.Content) + EstimateTokens(m.Name) + EstimateTokens(m.Role) + 4
	}
	return total
}

// SplitMessagesForCompaction 把消息切分为「保留」与「待压缩」两部分。
//
// system 消息始终保留在头部（模型需要它维持角色与规则），
// 保留区取尾部最近 keep 条 —— 最近的上下文对当前任务最相关。
func SplitMessagesForCompaction(msgs []openai.ChatCompletionMessage, keep int) (kept, dropped []openai.ChatCompletionMessage) {
	var system []openai.ChatCompletionMessage
	rest := msgs

	// 提取开头的 system 消息（可能有 0 或多条）
	for len(rest) > 0 && rest[0].Role == openai.ChatMessageRoleSystem {
		system = append(system, rest[0])
		rest = rest[1:]
	}

	if keep < 0 {
		keep = 0
	}

	kept = append(kept, system...)
	if keep >= len(rest) {
		// 无需压缩
		return append(kept, rest...), nil
	}

	kept = append(kept, rest[len(rest)-keep:]...)
	dropped = rest[:len(rest)-keep]
	return kept, dropped
}

// NeedsCompaction 判断消息是否超出预算。budget <= 0 时使用默认值。
func NeedsCompaction(msgs []openai.ChatCompletionMessage, budget int) bool {
	if budget <= 0 {
		budget = DefaultContextTokenBudget
	}
	return EstimateMessagesTokens(msgs) > budget
}

// BuildCompactionSummaryPrompt 构造摘要提示词，要求模型输出结构化摘要。
func BuildCompactionSummaryPrompt(dropped []openai.ChatCompletionMessage) string {
	var b strings.Builder
	b.WriteString("请把以下对话历史压缩成不超过 200 字的摘要，")
	b.WriteString("保留关键决策、已修改的文件、遗留问题和下一步计划。\n")
	b.WriteString("只输出摘要正文，不要加标题或解释。\n\n")

	for _, m := range dropped {
		content := m.Content
		if len(content) > compactSourcePreviewChars {
			content = content[:compactSourcePreviewChars] + "…"
		}
		b.WriteString("[")
		b.WriteString(m.Role)
		b.WriteString("] ")
		b.WriteString(content)
		b.WriteString("\n")
	}

	return b.String()
}

// TruncateSummary 将摘要截断到 MaxSummaryChars（按 rune 计数，不破坏 UTF-8）。
func TruncateSummary(s string) string {
	s = strings.TrimSpace(s)

	runes := []rune(s)
	if len(runes) <= MaxSummaryChars {
		return s
	}
	return string(runes[:MaxSummaryChars]) + "…"
}
