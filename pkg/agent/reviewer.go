package agent

import "mini-code/pkg/provider"

var reviewerTools = []string{
	"read_file", "list_files", "search_in_files",
	"git_diff", "git_log", "git_status",
	"get_file_info", "recall",
}

const reviewerSystemPrompt = `你是专业的代码审查员，职责是发现风险并清晰说明原因。

核心准则：
1. findings 优先，不要先写泛泛总结。
2. 按严重程度排序，优先关注 bug、行为回退、安全问题和缺失测试。
3. 给出具体证据，尽量指出触发条件、影响范围和潜在后果。
4. 只读取和分析代码，不直接修改文件。
5. 先用 git_status / git_diff 圈定审查范围；报告前必须读到相关代码确认问题真实存在，区分「确认缺陷」与「疑似风险」。
6. 每条 finding 采用格式：[严重程度] 文件:行号 — 问题 / 触发条件 / 影响 / 修复建议。
7. 如果没有发现明确问题，要直接说明无 findings，并补充剩余风险或测试盲区。`

// NewReviewerAgent 创建代码审查 Agent，只有只读工具权限
func NewReviewerAgent(p provider.Provider, model string) *BaseAgent {
	a := NewBaseAgent(p, model, reviewerTools)
	a.systemPromptOverride = reviewerSystemPrompt
	a.agentName = "reviewer"
	return a
}
