package orchestrator

import "strings"

// Router 根据用户消息内容路由到合适的 Agent 类型
type Router struct{}

func NewRouter() *Router { return &Router{} }

// reviewerKeywords 触发 ReviewerAgent 的关键词
var reviewerKeywords = []string{
	"审查", "review", "检查代码", "代码质量", "code review",
	"有什么问题", "找问题", "看看代码", "分析代码",
}

// researcherKeywords 触发 ResearcherAgent 的关键词
var researcherKeywords = []string{
	"调研", "搜索", "了解", "查找", "research",
	"怎么实现", "如何实现", "什么是", "比较", "对比",
}

// Route 返回应处理该消息的 Agent 类型名称
// 返回值："coder" | "reviewer" | "researcher"
func (r *Router) Route(message string) string {
	lower := strings.ToLower(message)

	for _, kw := range reviewerKeywords {
		if strings.Contains(lower, kw) {
			return "reviewer"
		}
	}
	for _, kw := range researcherKeywords {
		if strings.Contains(lower, kw) {
			return "researcher"
		}
	}
	return "coder" // 默认
}