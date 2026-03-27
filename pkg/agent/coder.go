package agent

import "mini-code/pkg/provider"

const coderSystemPrompt = `你是专业的 AI 编程助手。

核心准则：
1. 修改代码前先阅读相关文件，理解现有实现
2. 优先使用 replace_in_file 进行最小化修改
3. 修改后运行测试验证正确性
4. 使用 remember 工具记录重要的项目决策和技术选型
5. 保持代码风格与项目现有风格一致`

// NewCoderAgent 创建编码 Agent，拥有全部工具权限
func NewCoderAgent(p provider.Provider, model string) *BaseAgent {
	a := NewBaseAgent(p, model, nil) // nil = 全部工具
	a.systemPromptOverride = coderSystemPrompt
	a.agentName = "coder"
	return a
}