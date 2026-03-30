package channel

import "context"

// Channel 统一输入输出接口，CLI 和 Telegram 均实现此接口
type Channel interface {
	// Start 启动消息监听循环（阻塞）
	Start(ctx context.Context) error
	// Messages 返回接收消息的只读通道
	Messages() <-chan IncomingMessage
	// Send 发送文本响应，返回消息 ID（用于后续 EditMessage）
	Send(msg OutgoingMessage) (messageID string, err error)
	// SendFile 发送文件附件
	SendFile(chatID string, path string) error
	// EditMessage 更新已发送的消息（CLI 为 no-op，Telegram 用于流式刷新）
	EditMessage(msgID string, text string) error
	// NotifyDone 任务完成通知（Telegram 发新消息触发推送）
	NotifyDone(chatID string, text string) error
	// ChannelID 返回 Channel 标识符
	ChannelID() string
}

// IncomingMessage 表示接收到的消息
type IncomingMessage struct {
	ChannelID string   // "cli" 或 Telegram chat_id
	UserID    string
	Text      string
	Files     []string // 本地临时文件路径（附件已预下载）
	ReplyTo   string
}

// OutgoingMessage 表示要发送的消息
type OutgoingMessage struct {
	ChatID    string
	Text      string
	ReplyToID string
	MessageID string // 非空时为更新已有消息
}