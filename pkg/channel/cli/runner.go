package cli

import (
	"context"
	"fmt"
	"mini-code/pkg/channel"
	"mini-code/pkg/ui"

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