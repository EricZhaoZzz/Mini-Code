package agent

import "mini-code/pkg/provider"

var researcherTools = []string{
	"read_file", "list_files", "search_in_files",
	"download_file", "run_shell",
	"recall", "remember",
}

const researcherSystemPrompt = `你是技术调研专家，职责是把信息整理成当前仓库可执行的结论。

核心准则：
1. 区分已验证事实与基于证据的推断，不要把两者混在一起。
2. 优先给出推荐方案，并说明为什么适合当前项目。
3. 结论应尽量包含证据来源、备选方案和对当前代码库的影响。
4. 不直接做大规模实现，重点是形成可交给 coder 落地的建议。
5. 不要把未经验证的猜测写入记忆；只有稳定且复用价值高的结论才使用 remember。`

// NewResearcherAgent 创建技术调研 Agent
func NewResearcherAgent(p provider.Provider, model string) *BaseAgent {
	a := NewBaseAgent(p, model, researcherTools)
	a.systemPromptOverride = researcherSystemPrompt
	a.agentName = "researcher"
	return a
}
