package telegram

import (
	"context"
	"fmt"
	"mini-code/pkg/channel"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// TelegramChannel 实现 Telegram Bot 的 Channel 接口
type TelegramChannel struct {
	bot           *tgbotapi.BotAPI
	messages      chan channel.IncomingMessage
	allowedUsers  map[int64]bool
	tempDir       string

	// 消息管理：每个 chatID 对应当前正在更新的消息 ID
	editMsgs      map[int64]int64 // chatID -> messageID
	editMsgsMu    sync.Mutex

	// 完成消息管理：用于 NotifyDone
	pendingText   map[int64]string // chatID -> 当前累积的流式文本
	pendingMu     sync.Mutex

	// 流式更新控制
	updateInterval time.Duration // 流式刷新间隔
}

// Config Telegram Channel 配置
type Config struct {
	Token         string
	AllowedUsers  []int64
	TempDir       string
	UpdateInterval time.Duration // 流式刷新间隔，默认 1.5s
}

// New 创建新的 Telegram Channel
func New(cfg Config) (*TelegramChannel, error) {
	if cfg.Token == "" {
		return nil, fmt.Errorf("TELEGRAM_BOT_TOKEN is required")
	}

	bot, err := tgbotapi.NewBotAPI(cfg.Token)
	if err != nil {
		return nil, fmt.Errorf("failed to create bot: %w", err)
	}

	// 设置允许的用户
	allowed := make(map[int64]bool, len(cfg.AllowedUsers))
	for _, uid := range cfg.AllowedUsers {
		allowed[uid] = true
	}

	// 设置临时目录
	tempDir := cfg.TempDir
	if tempDir == "" {
		tempDir = filepath.Join(os.TempDir(), "mini-code-telegram")
	}
	if err := os.MkdirAll(tempDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}

	// 设置更新间隔
	interval := cfg.UpdateInterval
	if interval <= 0 {
		interval = 1500 * time.Millisecond // 默认 1.5 秒
	}

	return &TelegramChannel{
		bot:           bot,
		messages:      make(chan channel.IncomingMessage, 10),
		allowedUsers:  allowed,
		tempDir:       tempDir,
		editMsgs:      make(map[int64]int64),
		pendingText:   make(map[int64]string),
		updateInterval: interval,
	}, nil
}

// ChannelID 返回 Channel 标识符
func (tc *TelegramChannel) ChannelID() string { return "telegram" }

// Messages 返回接收消息的只读通道
func (tc *TelegramChannel) Messages() <-chan channel.IncomingMessage { return tc.messages }

// Start 启动消息监听循环（阻塞）
func (tc *TelegramChannel) Start(ctx context.Context) error {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := tc.bot.GetUpdatesChan(u)

	for {
		select {
		case <-ctx.Done():
			return nil
		case update := <-updates:
			if update.Message == nil {
				continue
			}

			// 检查用户权限
			if !tc.isAllowed(update.Message.From.ID) {
				tc.sendReply(update.Message.Chat.ID, update.Message.MessageID, "⚠️ 抱歉，您没有使用此 Bot 的权限。")
				continue
			}

			// 处理消息
			tc.handleMessage(update.Message)
		}
	}
}

// isAllowed 检查用户是否在白名单
func (tc *TelegramChannel) isAllowed(userID int64) bool {
	// 如果没有设置白名单，允许所有用户
	if len(tc.allowedUsers) == 0 {
		return true
	}
	return tc.allowedUsers[userID]
}

// handleMessage 处理收到的消息
func (tc *TelegramChannel) handleMessage(msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	userID := msg.From.ID

	// 调试：打印用户信息（帮助获取 User ID）
	if os.Getenv("TELEGRAM_DEBUG") != "" {
		fmt.Printf("[DEBUG] Chat ID: %d, User ID: %d, Username: @%s\n",
			chatID, userID, msg.From.UserName)
	}

	// 构建消息
	incoming := channel.IncomingMessage{
		ChannelID: fmt.Sprintf("telegram:%d", chatID),
		UserID:    fmt.Sprintf("telegram:%d", userID),
		Text:      msg.Text,
		ReplyTo:   fmt.Sprintf("%d", msg.MessageID),
	}

	// 处理附件
	if msg.Document != nil {
		path, err := tc.downloadFile(msg.Document.FileID, msg.Document.FileName)
		if err == nil {
			incoming.Files = append(incoming.Files, path)
		}
	}
	if msg.Photo != nil && len(msg.Photo) > 0 {
		// 获取最大尺寸的图片
		photo := msg.Photo[len(msg.Photo)-1]
		filename := fmt.Sprintf("photo_%d.jpg", time.Now().Unix())
		path, err := tc.downloadFile(photo.FileID, filename)
		if err == nil {
			incoming.Files = append(incoming.Files, path)
		}
	}

	// 发送到消息通道
	select {
	case tc.messages <- incoming:
	case <-time.After(5 * time.Second):
		// 通道满了，丢弃消息
		tc.sendReply(chatID, 0, "⚠️ 系统繁忙，请稍后重试。")
	}
}

// downloadFile 下载 Telegram 文件到本地临时目录
func (tc *TelegramChannel) downloadFile(fileID, filename string) (string, error) {
	url, err := tc.bot.GetFileDirectURL(fileID)
	if err != nil {
		return "", fmt.Errorf("get file url failed: %w", err)
	}

	// 创建临时文件
	tempPath := filepath.Join(tc.tempDir, fmt.Sprintf("%d_%s", time.Now().UnixNano(), filename))
	file, err := os.Create(tempPath)
	if err != nil {
		return "", fmt.Errorf("create temp file failed: %w", err)
	}
	defer file.Close()

	// 下载文件
	resp, err := http.Get(url)
	if err != nil {
		os.Remove(tempPath)
		return "", fmt.Errorf("download file failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		os.Remove(tempPath)
		return "", fmt.Errorf("download failed with status: %s", resp.Status)
	}

	_, err = file.ReadFrom(resp.Body)
	if err != nil {
		os.Remove(tempPath)
		return "", fmt.Errorf("save file failed: %w", err)
	}

	return tempPath, nil
}

// Send 发送文本响应，返回消息 ID（格式 "chatID:messageID"）
func (tc *TelegramChannel) Send(msg channel.OutgoingMessage) (string, error) {
	chatID, err := tc.parseChatID(msg.ChatID)
	if err != nil {
		return "", err
	}

	// 保存待发送文本用于流式更新
	tc.pendingMu.Lock()
	tc.pendingText[chatID] = msg.Text
	tc.pendingMu.Unlock()

	// 如果有消息 ID，尝试编辑
	if msg.MessageID != "" {
		msgID, _ := strconv.ParseInt(msg.MessageID, 10, 64)
		if err := tc.editMessage(chatID, msgID, msg.Text); err != nil {
			return "", err
		}
		return fmt.Sprintf("%d:%d", chatID, msgID), nil
	}

	// 否则发送新消息
	sentMsgID, err := tc.sendReplyWithID(chatID, 0, msg.Text)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d:%d", chatID, int64(sentMsgID)), nil
}

// SendFile 发送文件附件
func (tc *TelegramChannel) SendFile(chatID string, path string) error {
	cid, err := tc.parseChatID(chatID)
	if err != nil {
		return err
	}

	// 检查文件是否存在
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("file not found: %s", path)
	}

	doc := tgbotapi.NewDocument(cid, tgbotapi.FilePath(path))
	_, err = tc.bot.Send(doc)
	return err
}

// EditMessage 更新已发送的消息（流式刷新用）
func (tc *TelegramChannel) EditMessage(msgID string, text string) error {
	// msgID 格式为 "chatID:messageID"
	parts := strings.SplitN(msgID, ":", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid message ID format: %s", msgID)
	}

	chatID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid chat ID: %s", parts[0])
	}

	msgIDInt, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid message ID: %s", parts[1])
	}

	return tc.editMessage(chatID, msgIDInt, text)
}

// editMessage 编辑消息
func (tc *TelegramChannel) editMessage(chatID, messageID int64, text string) error {
	// Telegram 消息长度限制
	if len(text) > 4096 {
		text = text[:4090] + "..."
	}

	edit := tgbotapi.NewEditMessageText(chatID, int(messageID), text)
	edit.ParseMode = "Markdown"
	_, err := tc.bot.Send(edit)
	return err
}

// NotifyDone 任务完成通知（发新消息触发推送）
func (tc *TelegramChannel) NotifyDone(chatID string, text string) error {
	cid, err := tc.parseChatID(chatID)
	if err != nil {
		return err
	}

	// 清理流式状态
	tc.pendingMu.Lock()
	delete(tc.pendingText, cid)
	tc.pendingMu.Unlock()

	tc.editMsgsMu.Lock()
	delete(tc.editMsgs, cid)
	tc.editMsgsMu.Unlock()

	// 发送新消息（触发推送）
	return tc.sendReply(cid, 0, text)
}

// sendReply 发送回复消息
func (tc *TelegramChannel) sendReply(chatID int64, replyToMsgID int, text string) error {
	_, err := tc.sendReplyWithID(chatID, replyToMsgID, text)
	return err
}

// sendReplyWithID 发送回复消息并返回消息 ID
func (tc *TelegramChannel) sendReplyWithID(chatID int64, replyToMsgID int, text string) (int, error) {
	// 自动分段
	if len(text) <= 4096 {
		msg := tgbotapi.NewMessage(chatID, text)
		if replyToMsgID > 0 {
			msg.ReplyToMessageID = replyToMsgID
		}
		msg.ParseMode = "Markdown"
		sent, err := tc.bot.Send(msg)
		if err != nil {
			return 0, err
		}
		return sent.MessageID, nil
	}

	// 分段发送，返回最后一条消息的 ID
	segments := splitMessage(text, 4000)
	var lastMsgID int
	for i, segment := range segments {
		msg := tgbotapi.NewMessage(chatID, segment)
		if i == 0 && replyToMsgID > 0 {
			msg.ReplyToMessageID = replyToMsgID
		}
		msg.ParseMode = "Markdown"
		sent, err := tc.bot.Send(msg)
		if err != nil {
			return lastMsgID, err
		}
		lastMsgID = sent.MessageID
	}
	return lastMsgID, nil
}

// parseChatID 解析 chat ID
func (tc *TelegramChannel) parseChatID(chatID string) (int64, error) {
	// 格式可能为 "telegram:123456" 或 "123456"
	parts := strings.SplitN(chatID, ":", 2)
	idStr := chatID
	if len(parts) == 2 {
		idStr = parts[1]
	}
	return strconv.ParseInt(idStr, 10, 64)
}

// splitMessage 将长消息分割成多段
func splitMessage(text string, maxLen int) []string {
	var segments []string
	lines := strings.Split(text, "\n")
	var current strings.Builder

	for _, line := range lines {
		if current.Len()+len(line)+1 > maxLen {
			if current.Len() > 0 {
				segments = append(segments, current.String())
				current.Reset()
			}
			// 如果单行超长，直接截断
			if len(line) > maxLen {
				for len(line) > maxLen {
					segments = append(segments, line[:maxLen])
					line = line[maxLen:]
				}
				if line != "" {
					current.WriteString(line)
					current.WriteString("\n")
				}
			} else {
				current.WriteString(line)
				current.WriteString("\n")
			}
		} else {
			current.WriteString(line)
			current.WriteString("\n")
		}
	}

	if current.Len() > 0 {
		segments = append(segments, current.String())
	}

	return segments
}

// StartStreaming 开始流式输出会话
// 返回一个可用于更新的消息 ID（格式 "chatID:messageID"）
func (tc *TelegramChannel) StartStreaming(chatID int64) (string, error) {
	// 发送初始占位消息
	msg := tgbotapi.NewMessage(chatID, "⏳ 正在处理...")
	sent, err := tc.bot.Send(msg)
	if err != nil {
		return "", err
	}

	tc.editMsgsMu.Lock()
	tc.editMsgs[chatID] = int64(sent.MessageID)
	tc.editMsgsMu.Unlock()

	return fmt.Sprintf("%d:%d", chatID, sent.MessageID), nil
}

// UpdateStreaming 更新流式输出内容
func (tc *TelegramChannel) UpdateStreaming(chatID int64, text string) error {
	tc.editMsgsMu.Lock()
	msgID, ok := tc.editMsgs[chatID]
	tc.editMsgsMu.Unlock()

	if !ok {
		return nil // 没有活动的流式会话
	}

	return tc.editMessage(chatID, msgID, text)
}

// GetBotInfo 获取 Bot 信息
func (tc *TelegramChannel) GetBotInfo() (string, error) {
	me, err := tc.bot.GetMe()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("@%s (%s)", me.UserName, me.FirstName), nil
}