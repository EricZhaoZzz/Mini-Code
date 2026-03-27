package agent

import "mini-code/pkg/provider"

var researcherTools = []string{
	"read_file", "list_files", "search_in_files",
	"download_file", "run_shell",
	"recall", "remember",
}

const researcherSystemPrompt = `你是技术调研专家。

核心准则：
1. 广泛搜索相关信息后再给出结论
2. 区分已确认的事实和推测
3. 提供有依据的技术建议
4. 将重要的调研结论用 remember 工具保存`

// NewResearcherAgent 创建技术调研 Agent
func NewResearcherAgent(p provider.Provider, model string) *BaseAgent {
	a := NewBaseAgent(p, model, researcherTools)
	a.systemPromptOverride = researcherSystemPrompt
	a.agentName = "researcher"
	return a
}