package cli

import (
	"context"
	"fmt"
	"mini-code/pkg/channel"
	"mini-code/pkg/ui"
	"strings"

	"github.com/ergochat/readline"
)

// CLIChannel 实现 CLI 输入输出的 Channel
type CLIChannel struct {
	rl       *readline.Instance
	messages chan channel.IncomingMessage
}

// New 创建新的 CLI Channel
func New(rl *readline.Instance) *CLIChannel {
	return &CLIChannel{
		rl:       rl,
		messages: make(chan channel.IncomingMessage, 1),
	}
}

// ChannelID 返回 Channel 标识符
func (c *CLIChannel) ChannelID() string { return "cli" }

// Messages 返回接收消息的只读通道
func (c *CLIChannel) Messages() <-chan channel.IncomingMessage { return c.messages }

// Start 启动消息监听循环（阻塞）
func (c *CLIChannel) Start(ctx context.Context) error {
	defer close(c.messages)
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		line, err := c.rl.Readline()
		if err != nil {
			return nil // EOF 或 Ctrl+D
		}
		if line == "" {
			continue
		}
		select {
		case c.messages <- channel.IncomingMessage{
			ChannelID: "cli",
			UserID:    "local",
			Text:      line,
		}:
		case <-ctx.Done():
			return nil
		}
	}
}

// Confirm 在任务执行中向用户请求 y/n 确认。
//
// 任务执行期间主循环阻塞在 Orchestrator.Handle 上，而 Start 循环仍在
// 读取下一行输入，因此直接从 messages 通道取用户的下一行作为回答，
// 无需与 readline 争抢 stdin。确认期间关闭历史记录，避免 y/n 污染历史。
// 只有明确输入 y/yes/是 才视为同意，其余一律拒绝。
func (c *CLIChannel) Confirm(ctx context.Context, prompt string) (bool, error) {
	c.rl.DisableHistory()
	defer c.rl.EnableHistory()

	fmt.Fprintf(c.rl.Stdout(), "\n%s\n", ui.SprintColor(ui.Warning, "⚠ 需要确认: "+prompt))
	fmt.Fprintf(c.rl.Stdout(), "%s\n", ui.SprintColor(ui.Dim, "  输入 y 确认，其他任意内容取消，然后回车"))

	select {
	case msg, ok := <-c.messages:
		if !ok {
			return false, fmt.Errorf("输入已关闭")
		}
		return IsAffirmative(msg.Text), nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

// Ask 在主循环中向用户提一个问题并返回下一行输入（不进入历史记录）。
// 与 Confirm 相同，回答直接取自 messages 通道，不与 readline 争抢 stdin。
func (c *CLIChannel) Ask(ctx context.Context, prompt string) (string, error) {
	c.rl.DisableHistory()
	defer c.rl.EnableHistory()

	fmt.Fprintf(c.rl.Stdout(), "%s\n", ui.SprintColor(ui.Info, prompt))
	select {
	case msg, ok := <-c.messages:
		if !ok {
			return "", fmt.Errorf("输入已关闭")
		}
		return strings.TrimSpace(msg.Text), nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// IsAffirmative 判断用户回答是否为明确同意。
func IsAffirmative(answer string) bool {
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes", "是":
		return true
	}
	return false
}

// Send 发送文本响应
func (c *CLIChannel) Send(msg channel.OutgoingMessage) (string, error) {
	// 流式输出由 Orchestrator 的 handler 直接调用 ui，此处处理非流式响应
	fmt.Print(msg.Text)
	return "", nil // CLI 不需要消息 ID
}

// SendFile 发送文件附件
func (c *CLIChannel) SendFile(_ string, path string) error {
	ui.PrintInfo("文件: %s", path)
	return nil
}

// EditMessage 更新已发送的消息（CLI 为 no-op）
func (c *CLIChannel) EditMessage(_ string, _ string) error { return nil }

// NotifyDone 任务完成通知
func (c *CLIChannel) NotifyDone(_ string, text string) error {
	ui.PrintSuccess("%s", text)
	return nil
}
