package orchestrator

import (
	"regexp"
	"strings"
)

// Router 根据用户消息内容路由到合适的 Agent 类型。
//
// 判定刻意设计为非对称：coder 拥有全部工具，是唯一不会因误判而让任务失败的角色；
// reviewer / researcher 持有工具子集，误判会导致「模型想改代码却没有权限」，
// 而且不会给出任何报错提示。因此只要检测到写入意图就直接归 coder，
// 哪怕消息里同时出现「对比」「看看」这类本该触发子角色的词。
//
// 代价：纯研究请求里含「实现」二字会被判成 coder（如「怎么实现闭包」）。
// 这是可接受的——coder 同样持有 web_search，只是少一段角色提示词。
type Router struct{}

func NewRouter() *Router { return &Router{} }

// writeIntentKeywords 表示「需要产出代码改动」的意图，优先级最高。
// 注意「写」「改」「加」「删」「错」等单字容易误命中无关词，
// 但由于 coder 是安全的兜底角色，误命中的代价远小于漏判。
var writeIntentKeywords = []string{
	// 中文双字词优先
	"实现", "修改", "重构", "修复", "添加", "新增", "删除", "重命名",
	"优化", "调整", "编写", "创建", "生成", "更新", "改进", "完善",
	"迁移", "补充", "重写", "替换", "合并", "安装", "卸载",
	"报错", "不对", "崩溃", "失败", "修一下", "哪里错", "有问题",
	// 中文单字（兜底）
	"写", "改", "加", "删", "错",
	// 英文
	"implement", "fix", "refactor", "add", "create", "update", "modify",
	"write", "change", "delete", "remove", "rename", "migrate", "patch",
	"bug", "debug", "broken", "error", "crash", "failing", "should be", "modified",
}

// reviewIntentKeywords 表示「只读审查」意图。
var reviewIntentKeywords = []string{
	"审查", "review", "代码质量", "code review", "找问题",
	"有什么问题", "有没有问题", "看看代码", "分析代码", "检查代码",
	"评估", "风险点", "能不能合并", "值得合并",
}

// researcherIntentKeywords 表示「需要外部信息或方案对比」意图。
var researcherIntentKeywords = []string{
	"调研", "搜索", "了解", "查找", "research", "什么是",
	"怎么实现", "如何实现", "哪种方案", "选型", "文档",
	"对比", "比较", "优劣", "权衡", "区别", "差异",
	"哪个更", "更适合", "选哪个", "有什么区别",
}

// 预编译的关键词匹配器，与上面的关键词表一一对应。
var (
	writeIntentMatcher      = newKeywordMatcher(writeIntentKeywords)
	reviewIntentMatcher     = newKeywordMatcher(reviewIntentKeywords)
	researcherIntentMatcher = newKeywordMatcher(researcherIntentKeywords)
)

// Route 返回应处理该消息的 Agent 类型名称。
// 返回值："coder" | "reviewer" | "researcher"
func (r *Router) Route(message string) string {
	lower := strings.ToLower(message)

	// 写入意图优先：coder 能完成所有任务，误判代价最低
	if writeIntentMatcher.match(lower) {
		return "coder"
	}
	if reviewIntentMatcher.match(lower) {
		return "reviewer"
	}
	if researcherIntentMatcher.match(lower) {
		return "researcher"
	}
	return "coder" // 兜底
}

// keywordMatcher 匹配关键词表。
//
// 中文关键词按子串匹配（中文无词边界）；英文关键词按整词匹配并允许常见词形变化。
// 旧实现对英文也用子串匹配，导致 add 命中 address、fix 命中 prefix、
// patch 命中 dispatch_agent，把明显的只读请求误判为写入意图。
type keywordMatcher struct {
	cjk     []string
	english *regexp.Regexp
}

func newKeywordMatcher(keywords []string) keywordMatcher {
	var m keywordMatcher
	var alts []string
	for _, kw := range keywords {
		if isASCII(kw) {
			alts = append(alts, englishKeywordPattern(kw))
		} else {
			m.cjk = append(m.cjk, kw)
		}
	}
	if len(alts) > 0 {
		// \b 在 RE2 中是 ASCII 词边界：中文字符视为非单词字符，
		// 因此「review一下」「fix一下」中英混写仍能命中。
		m.english = regexp.MustCompile(`\b(?:` + strings.Join(alts, "|") + `)\b`)
	}
	return m
}

// englishKeywordPattern 生成允许常见屈折变化的模式：
// fix → fixes/fixed/fixing，create → created/creating，add → adds/added/adding。
func englishKeywordPattern(kw string) string {
	pattern := regexp.QuoteMeta(kw) + `(?:s|es|d|ed|ing)?`
	if strings.HasSuffix(kw, "e") && len(kw) > 2 {
		pattern += `|` + regexp.QuoteMeta(kw[:len(kw)-1]) + `ing`
	}
	return pattern
}

func (m keywordMatcher) match(text string) bool {
	for _, kw := range m.cjk {
		if strings.Contains(text, kw) {
			return true
		}
	}
	return m.english != nil && m.english.MatchString(text)
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}
