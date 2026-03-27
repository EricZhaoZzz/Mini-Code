package agent

import "mini-code/pkg/provider"

var reviewerTools = []string{
	"read_file", "list_files", "search_in_files",
	"git_diff", "git_log", "git_status",
	"get_file_info", "recall",
}

const reviewerSystemPrompt = `你是专业的代码审查员。

核心准则：
1. 只读取和分析代码，不直接修改文件
2. 关注代码质量、安全性、可维护性
3. 给出具体、可操作的改进建议
4. 指出潜在的 bug 和性能问题`

// NewReviewerAgent 创建代码审查 Agent，只有只读工具权限
func NewReviewerAgent(p provider.Provider, model string) *BaseAgent {
	a := NewBaseAgent(p, model, reviewerTools)
	a.systemPromptOverride = reviewerSystemPrompt
	a.agentName = "reviewer"
	return a
}