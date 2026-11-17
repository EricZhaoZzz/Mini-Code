package agent

import (
	"mini-code/pkg/textutil"
	"os"
	"strconv"
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

	// compactSourcePreviewChars 构造摘要提示词时，每条消息的预览长度上限（rune）。
	compactSourcePreviewChars = 500

	// prunedToolResultPlaceholder 运行中为腾出上下文而省略旧工具结果时的占位文本。
	prunedToolResultPlaceholder = "[该工具结果已省略以节省上下文；如仍需要请重新调用工具]"
)

// ContextBudget 返回上下文预算，可用 LM_CONTEXT_BUDGET 覆盖（单位：token）。
func ContextBudget() int {
	if v := os.Getenv("LM_CONTEXT_BUDGET"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return DefaultContextTokenBudget
}

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
		total += estimateMessageTokens(m)
	}
	return total
}

// estimateMessageTokens 估算单条消息。
//
// 工具调用的参数必须计入：assistant 发起 write_file 时 Content 为空，
// 但 Arguments 里是整份文件内容，漏算会让压缩触发得过晚。
func estimateMessageTokens(m openai.ChatCompletionMessage) int {
	n := EstimateTokens(m.Content) + EstimateTokens(m.Name) + EstimateTokens(m.Role) +
		EstimateTokens(m.ToolCallID) + 4
	for _, tc := range m.ToolCalls {
		n += EstimateTokens(tc.ID) + EstimateTokens(tc.Function.Name) +
			EstimateTokens(tc.Function.Arguments) + 4
	}
	return n
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

	cut := adjustCutForToolPairs(rest, len(rest)-keep)
	if cut <= 0 {
		return append(kept, rest...), nil
	}

	kept = append(kept, rest[cut:]...)
	dropped = rest[:cut]
	return kept, dropped
}

// adjustCutForToolPairs 调整切分点，保证保留区不以孤立的 tool 消息开头。
//
// OpenAI 兼容接口要求每条 tool 消息前必须有发起该调用的 assistant(tool_calls)，
// 否则直接返回 400，会话就此卡死。优先向前回退以把整组调用留在保留区；
// 回退到头仍找不到发起者时，改为向后跳过这些孤立的 tool 消息。
func adjustCutForToolPairs(rest []openai.ChatCompletionMessage, cut int) int {
	if cut <= 0 || cut >= len(rest) || rest[cut].Role != openai.ChatMessageRoleTool {
		return cut
	}
	for i := cut - 1; i >= 0; i-- {
		if rest[i].Role == openai.ChatMessageRoleTool {
			continue
		}
		if rest[i].Role == openai.ChatMessageRoleAssistant && len(rest[i].ToolCalls) > 0 {
			return i
		}
		break
	}
	for cut < len(rest) && rest[cut].Role == openai.ChatMessageRoleTool {
		cut++
	}
	return cut
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
	b.WriteString("请把以下对话历史压缩成不超过 400 字的摘要。")
	b.WriteString("必须保留：已修改的文件路径、关键决策及原因、未解决的问题或报错要点、下一步计划。\n")
	b.WriteString("只输出摘要正文，不要加标题或解释。\n\n")

	for _, m := range dropped {
		b.WriteString("[")
		b.WriteString(m.Role)
		b.WriteString("] ")
		b.WriteString(textutil.TruncateWithEllipsis(m.Content, compactSourcePreviewChars))
		// 工具调用常常 Content 为空，必须把调用本身写进去，
		// 否则摘要会丢失「改了哪些文件」这类关键信息。
		for _, tc := range m.ToolCalls {
			b.WriteString(" 调用 ")
			b.WriteString(tc.Function.Name)
			b.WriteString(" ")
			b.WriteString(textutil.TruncateWithEllipsis(tc.Function.Arguments, 200))
		}
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

// PruneToolResults 在单次任务的多轮工具调用中，消息超出预算时，
// 从最早开始把 tool 结果替换为占位文本，直到回到预算内。
//
// 与 Orchestrator 的摘要压缩互补：那里只在任务开始前触发一次，
// 而一次任务内部可能有几十轮工具调用。这里不调用 LLM、不改变消息结构
// （tool_call 配对保持完整），只缩短内容，因此可以每轮执行。
// 最后一组工具结果（模型尚未看到）永远不省略。返回新切片，不修改入参。
func PruneToolResults(msgs []openai.ChatCompletionMessage, budget int) []openai.ChatCompletionMessage {
	if budget <= 0 {
		budget = DefaultContextTokenBudget
	}
	total := EstimateMessagesTokens(msgs)
	if total <= budget {
		return msgs
	}

	// 最后一条 assistant 之后的 tool 消息属于本轮，必须保留
	protectFrom := len(msgs)
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == openai.ChatMessageRoleAssistant {
			protectFrom = i
			break
		}
	}

	out := make([]openai.ChatCompletionMessage, len(msgs))
	copy(out, msgs)
	placeholderTokens := EstimateTokens(prunedToolResultPlaceholder)
	for i := 0; i < protectFrom && total > budget; i++ {
		if out[i].Role != openai.ChatMessageRoleTool || out[i].Content == prunedToolResultPlaceholder {
			continue
		}
		saved := EstimateTokens(out[i].Content) - placeholderTokens
		if saved <= 0 {
			continue
		}
		out[i].Content = prunedToolResultPlaceholder
		total -= saved
	}
	return out
}
