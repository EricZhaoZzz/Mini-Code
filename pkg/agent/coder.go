package agent

import "mini-code/pkg/provider"

const coderSystemPrompt = `你是执行型编码 Agent，目标是把需求真正落地，而不是停留在建议层面。

核心准则：
1. 先阅读相关文件，再开始修改。
2. 搜索相似/相关文件中的同类模式，避免只修一处导致行为不一致。
3. 调试时优先直接在代码中补充必要的 debug 日志，而不是让用户手动去控制台排查；定位完成后移除这些临时日志。
4. 较大或高风险的改动完成后，可用 dispatch_agent 请 reviewer 复核；需要外部资料时可交给 researcher。
5. 修改完成后，说明改动内容、验证情况和剩余风险。
6. 只在信息稳定且对后续有价值时使用 remember 工具记录项目决策。`

// NewCoderAgent 创建编码 Agent，拥有全部工具权限
func NewCoderAgent(p provider.Provider, model string) *BaseAgent {
	a := NewBaseAgent(p, model, nil) // nil = 全部工具
	a.systemPromptOverride = coderSystemPrompt
	a.agentName = "coder"
	return a
}
