package telegram

import (
	"fmt"
	"mini-code/pkg/channel"
	"mini-code/pkg/memory"
	"mini-code/pkg/orchestrator"
	"strings"
)

// CommandHandler 处理 Telegram 命令
type CommandHandler struct {
	orch    *orchestrator.Orchestrator
	memStore *memory.Store
}

// NewCommandHandler 创建命令处理器
func NewCommandHandler(orch *orchestrator.Orchestrator, memStore *memory.Store) *CommandHandler {
	return &CommandHandler{
		orch:    orch,
		memStore: memStore,
	}
}

// CommandResult 命令处理结果
type CommandResult struct {
	Handled      bool   // 是否已处理
	Response     string // 响应文本
	ResetSession bool   // 是否需要重置会话
	CancelTask   bool   // 是否需要取消当前任务
}

// Handle 处理命令
func (h *CommandHandler) Handle(msg channel.IncomingMessage, session *orchestrator.Session) CommandResult {
	text := strings.TrimSpace(msg.Text)

	// 只处理以 / 开头的消息
	if !strings.HasPrefix(text, "/") {
		return CommandResult{Handled: false}
	}

	// 解析命令和参数
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return CommandResult{Handled: false}
	}

	cmd := strings.ToLower(parts[0])
	args := parts[1:]

	switch cmd {
	case "/start":
		return h.handleStart(msg)
	case "/reset":
		return h.handleReset(session)
	case "/memory":
		return h.handleMemory(args, msg.UserID)
	case "/status":
		return h.handleStatus(session)
	case "/cancel":
		return h.handleCancel(session)
	case "/help":
		return h.handleHelp()
	default:
		return CommandResult{
			Handled:  true,
			Response: fmt.Sprintf("未知命令: %s\n发送 /help 查看可用命令。", cmd),
		}
	}
}

// handleStart 处理 /start 命令
func (h *CommandHandler) handleStart(msg channel.IncomingMessage) CommandResult {
	response := `👋 欢迎使用 Mini-Code Bot！

我是一个专业的 AI 编程助手，可以帮助你：

• 📝 编写和修改代码
• 🔍 审查代码质量
• 📚 进行技术调研
• 🛠️ 执行各种开发任务

直接发送消息描述你的需求，我会尽力帮助你。

发送 /help 查看更多命令。`

	return CommandResult{
		Handled:  true,
		Response: response,
	}
}

// handleReset 处理 /reset 命令
func (h *CommandHandler) handleReset(session *orchestrator.Session) CommandResult {
	session.Reset()
	return CommandResult{
		Handled:      true,
		Response:     "✅ 会话已重置，对话历史已清空。",
		ResetSession: true,
	}
}

// handleMemory 处理 /memory 命令
func (h *CommandHandler) handleMemory(args []string, userID string) CommandResult {
	if h.memStore == nil {
		return CommandResult{
			Handled:  true,
			Response: "⚠️ 记忆系统未启用。",
		}
	}

	// 子命令处理
	if len(args) > 0 {
		switch args[0] {
		case "list", "ls", "":
			// 默认行为：列出记忆
		case "clear":
			return CommandResult{
				Handled:  true,
				Response: "⚠️ 清除记忆功能暂未实现。\n如需删除特定记忆，请在对话中让 AI 使用 forget 工具。",
			}
		}
	}

	// 获取所有记忆
	records, err := h.memStore.GetAll()
	if err != nil {
		return CommandResult{
			Handled:  true,
			Response: fmt.Sprintf("❌ 获取记忆失败: %v", err),
		}
	}

	if len(records) == 0 {
		return CommandResult{
			Handled:  true,
			Response: "📭 暂无记忆记录。\n\n在对话中可以让 AI 使用 remember 工具保存重要信息。",
		}
	}

	// 格式化输出
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📚 共 %d 条记忆记录：\n\n", len(records)))

	for i, r := range records {
		scopeIcon := map[string]string{
			"project": "📁",
			"user":    "👤",
			"global":  "🌍",
		}[r.Scope]

		if scopeIcon == "" {
			scopeIcon = "📄"
		}

		sb.WriteString(fmt.Sprintf("%d. %s [%s] #%d\n", i+1, scopeIcon, r.Scope, r.ID))
		sb.WriteString(fmt.Sprintf("   %s\n", truncate(r.Content, 100)))
		if len(r.Tags) > 0 {
			sb.WriteString(fmt.Sprintf("   标签: %s\n", strings.Join(r.Tags, ", ")))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("\n💡 在对话中可以让 AI 使用 remember/forget/recall 工具管理记忆。")

	return CommandResult{
		Handled:  true,
		Response: sb.String(),
	}
}

// handleStatus 处理 /status 命令
func (h *CommandHandler) handleStatus(session *orchestrator.Session) CommandResult {
	agentType, turn, lastTool := session.StatusInfo()
	msgCount := session.MessageCount()

	var status strings.Builder
	status.WriteString("📊 当前会话状态：\n\n")
	status.WriteString(fmt.Sprintf("• Agent 类型: %s\n", agentType))
	status.WriteString(fmt.Sprintf("• 对话消息数: %d\n", msgCount))

	if turn > 0 {
		status.WriteString(fmt.Sprintf("• 当前轮次: %d\n", turn))
	}
	if lastTool != "" {
		status.WriteString(fmt.Sprintf("• 最近工具: %s\n", lastTool))
	}

	return CommandResult{
		Handled:  true,
		Response: status.String(),
	}
}

// handleCancel 处理 /cancel 命令
func (h *CommandHandler) handleCancel(session *orchestrator.Session) CommandResult {
	session.Cancel()
	return CommandResult{
		Handled:    true,
		Response:   "🛑 已发送取消信号，正在终止当前任务...",
		CancelTask: true,
	}
}

// handleHelp 处理 /help 命令
func (h *CommandHandler) handleHelp() CommandResult {
	response := `📖 Mini-Code Bot 命令帮助

基本命令：
/start - 显示欢迎信息
/help - 显示此帮助信息
/reset - 重置当前会话（清空对话历史）
/status - 查看当前会话状态
/cancel - 取消当前正在执行的任务
/memory - 查看已保存的记忆

使用方式：
直接发送消息描述你的需求，例如：
• "帮我创建一个 HTTP 服务"
• "审查一下 pkg/utils 目录的代码"
• "调研一下 Go 语言的 ORM 框架"

提示：
• 支持发送文件/图片作为附件
• 对话上下文会保持，可以连续提问
• 使用 /reset 开始新的任务`

	return CommandResult{
		Handled:  true,
		Response: response,
	}
}

// truncate 截断字符串
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}