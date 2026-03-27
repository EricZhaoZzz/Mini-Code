# Phase 4: Telegram Channel Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 Telegram Bot Channel，支持长轮询接收消息、白名单鉴权、附件下载、流式刷新（edit_message）、任务完成推送通知，以及 /reset /memory /status /cancel 五个命令。

**Architecture:** `pkg/channel/telegram/bot.go` 实现 `Channel` 接口，Long Polling 接收消息；同一 chat_id 串行处理（Lane Queue）；流式响应通过周期性 edit_message 刷新；`main.go` 通过 `--channel telegram` 启动参数切换到 Telegram 模式。

**Tech Stack:** `github.com/go-telegram-bot-api/telegram-bot-api/v5`，Go 标准库 os/filepath/time。

**前置条件：** Phase 1、2、3 全部完成，测试通过。你需要一个真实的 Telegram Bot Token（通过 @BotFather 创建）才能做手动验收测试，但单元测试不需要。

---

## 文件变动清单

| 操作 | 文件 | 说明 |
|------|------|------|
| Modify | `pkg/channel/types.go` | 添加 `StreamingWriter` 接口（避免循环依赖）|
| Create | `pkg/channel/telegram/bot.go` | Telegram Bot 主实现 |
| Create | `pkg/channel/telegram/commands.go` | /命令处理逻辑 |
| Create | `pkg/channel/telegram/streaming.go` | 流式刷新（edit_message 每 1.5s）|
| Create | `pkg/channel/telegram/bot_test.go` | Bot 逻辑单元测试（mock API）|
| Modify | `go.mod` / `go.sum` | 添加 telegram-bot-api 依赖 |
| Modify | `cmd/agent/main.go` | 支持 `--channel telegram` 启动参数 |
| Modify | `.env.example` | 添加 TELEGRAM_* 环境变量说明 |

---

## Task 1: 添加 Telegram Bot API 依赖

- [ ] **Step 1.1: 安装依赖**

```bash
cd C:/Users/Eric/dev/mini-code
go get github.com/go-telegram-bot-api/telegram-bot-api/v5
```
预期：`go.mod` 和 `go.sum` 更新

- [ ] **Step 1.2: 验证编译**

```bash
go build ./... 2>&1
```
预期：无报错

- [ ] **Step 1.3: 更新 .env.example**

在 `.env.example` 中追加：

```env
# Telegram Channel 配置（--channel telegram 时必填）
TELEGRAM_BOT_TOKEN=<BotFather 颁发的 token>
TELEGRAM_ALLOWED_USERS=123456789,987654321   # 允许访问的 Telegram 用户 ID，逗号分隔
```

- [ ] **Step 1.4: Commit**

```bash
git add go.mod go.sum .env.example
git commit -m "chore: 添加 telegram-bot-api 依赖"
```

---

## Task 1.5: 在 Channel 接口中添加 StreamingWriter 接口（避免循环依赖）

> **为什么需要这步：** Orchestrator 需要在 Handle 方法中根据 Channel 类型选择流式输出策略。若直接 import `pkg/channel/telegram`，会造成 orchestrator → telegram → channel → orchestrator 潜在循环。通过在 `pkg/channel/types.go` 定义接口，双方只依赖 `pkg/channel`。

- [ ] **Step 1.5.1: 在 `pkg/channel/types.go` 末尾添加接口**

```go
// StreamingWriter 支持流式输出刷新的 Channel 可选实现
// Telegram Channel 实现此接口；CLI Channel 不需要（流式直接写终端）
type StreamingWriter interface {
	// NewStreamingReply 创建流式回复容器，返回一个 Writer
	NewStreamingReply(chatID string) (StreamingReply, error)
}

// StreamingReply 流式回复的写入接口
type StreamingReply interface {
	Write(content string)
	Complete(success bool) // success=false 时发送错误通知而非"✅ 任务完成"
}
```

- [ ] **Step 1.5.2: Commit**

```bash
git add pkg/channel/types.go
git commit -m "feat(channel): 添加 StreamingWriter 接口，支持 Telegram 流式刷新"
```

---

## Task 2: Telegram Bot 核心实现

**Files:**
- Create: `pkg/channel/telegram/bot.go`
- Create: `pkg/channel/telegram/bot_test.go`

- [ ] **Step 2.1: 写 Bot 单元测试（白名单鉴权）**

```go
// pkg/channel/telegram/bot_test.go
package telegram_test

import (
	"testing"
	"mini-code/pkg/channel/telegram"
)

func TestParseAllowedUsers(t *testing.T) {
	tests := []struct {
		input    string
		expected []int64
	}{
		{"123456789", []int64{123456789}},
		{"123,456,789", []int64{123, 456, 789}},
		{"", nil},
		{"abc,123", []int64{123}}, // 非法 ID 跳过
	}

	for _, tt := range tests {
		result := telegram.ParseAllowedUsers(tt.input)
		if len(result) != len(tt.expected) {
			t.Errorf("input %q: expected %v, got %v", tt.input, tt.expected, result)
			continue
		}
		for i, v := range result {
			if v != tt.expected[i] {
				t.Errorf("input %q [%d]: expected %d, got %d", tt.input, i, tt.expected[i], v)
			}
		}
	}
}

func TestIsAllowed_EmptyListAllowsAll(t *testing.T) {
	bot := &telegram.TelegramChannel{AllowedUsers: nil}
	if !bot.IsAllowed(99999) {
		t.Error("empty allowlist should allow all users")
	}
}

func TestIsAllowed_NonEmptyListFilters(t *testing.T) {
	bot := &telegram.TelegramChannel{AllowedUsers: []int64{111, 222}}
	if !bot.IsAllowed(111) {
		t.Error("111 should be allowed")
	}
	if bot.IsAllowed(333) {
		t.Error("333 should not be allowed")
	}
}

func TestSplitMessage_LongTextIsSplit(t *testing.T) {
	longText := make([]byte, 5000)
	for i := range longText {
		longText[i] = 'a'
	}
	parts := telegram.SplitMessage(string(longText), 4096)
	if len(parts) < 2 {
		t.Errorf("expected at least 2 parts for 5000-char message, got %d", len(parts))
	}
	for _, p := range parts {
		if len(p) > 4096 {
			t.Errorf("part too long: %d chars", len(p))
		}
	}
}
```

- [ ] **Step 2.2: 运行测试确认失败**

```bash
go test ./pkg/channel/telegram/... 2>&1
```
预期：`cannot find package`

- [ ] **Step 2.3: 创建 bot.go**

```go
// pkg/channel/telegram/bot.go
package telegram

import (
	"context"
	"fmt"
	"io"
	"mini-code/pkg/channel"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// TelegramChannel 实现 channel.Channel 接口
type TelegramChannel struct {
	bot          *tgbotapi.BotAPI
	AllowedUsers []int64
	messages     chan channel.IncomingMessage
	// Lane Queue：每个 chat_id 一个串行队列
	lanes    map[int64]chan struct{}
	lanesMu  sync.Mutex
}

// New 创建 Telegram Channel
// token: Bot Token；allowedUsers: 白名单用户 ID 列表（nil 表示允许所有人）
func New(token string, allowedUsers []int64) (*TelegramChannel, error) {
	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, fmt.Errorf("创建 Telegram Bot 失败: %w", err)
	}
	return &TelegramChannel{
		bot:          bot,
		AllowedUsers: allowedUsers,
		messages:     make(chan channel.IncomingMessage, 10),
		lanes:        make(map[int64]chan struct{}),
	}, nil
}

func (t *TelegramChannel) ChannelID() string { return "telegram" }

func (t *TelegramChannel) Messages() <-chan channel.IncomingMessage { return t.messages }

// IsAllowed 检查用户是否在白名单中
func (t *TelegramChannel) IsAllowed(userID int64) bool {
	if len(t.AllowedUsers) == 0 {
		return true
	}
	for _, id := range t.AllowedUsers {
		if id == userID {
			return true
		}
	}
	return false
}

// Start 启动 Long Polling 消息监听（阻塞）
func (t *TelegramChannel) Start(ctx context.Context) error {
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := t.bot.GetUpdatesChan(u)

	for {
		select {
		case <-ctx.Done():
			t.bot.StopReceivingUpdates()
			return nil
		case update := <-updates:
			if update.Message == nil {
				continue
			}
			msg := update.Message
			chatID := msg.Chat.ID
			userID := msg.From.ID

			// 白名单鉴权
			if !t.IsAllowed(userID) {
				continue // 静默丢弃
			}

			// 处理附件
			var files []string
			if msg.Document != nil || msg.Photo != nil {
				downloaded := t.downloadAttachment(msg)
				if downloaded != "" {
					files = append(files, downloaded)
				}
			}

			t.messages <- channel.IncomingMessage{
				ChannelID: fmt.Sprintf("%d", chatID),
				UserID:    fmt.Sprintf("%d", userID),
				Text:      msg.Text + msg.Caption,
				Files:     files,
			}
		}
	}
}

// downloadAttachment 下载 Telegram 附件到本地临时目录
func (t *TelegramChannel) downloadAttachment(msg *tgbotapi.Message) string {
	var fileID string
	var fileName string

	if msg.Document != nil {
		fileID = msg.Document.FileID
		fileName = msg.Document.FileName
	} else if len(msg.Photo) > 0 {
		// 取最大分辨率的图片
		photo := msg.Photo[len(msg.Photo)-1]
		fileID = photo.FileID
		fileName = fileID + ".jpg"
	}

	if fileID == "" {
		return ""
	}

	fileURL, err := t.bot.GetFileDirectURL(fileID)
	if err != nil {
		return ""
	}

	// 保存到临时目录
	tmpDir := filepath.Join(os.TempDir(), "mini-code", fmt.Sprintf("%d", msg.Chat.ID))
	os.MkdirAll(tmpDir, 0o755)
	localPath := filepath.Join(tmpDir, fileName)

	// 下载文件（使用 http 包）
	if err := downloadURL(fileURL, localPath); err != nil {
		return ""
	}
	return localPath
}

func (t *TelegramChannel) Send(msg channel.OutgoingMessage) error {
	chatID, err := strconv.ParseInt(msg.ChatID, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid chat_id: %w", err)
	}
	parts := SplitMessage(msg.Text, 4096)
	for _, part := range parts {
		m := tgbotapi.NewMessage(chatID, part)
		m.ParseMode = "Markdown"
		if _, err := t.bot.Send(m); err != nil {
			// Markdown 失败时回退到纯文本
			m.ParseMode = ""
			_, err = t.bot.Send(m)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (t *TelegramChannel) SendFile(chatID string, path string) error {
	id, err := strconv.ParseInt(chatID, 10, 64)
	if err != nil {
		return err
	}
	doc := tgbotapi.NewDocument(id, tgbotapi.FilePath(path))
	_, err = t.bot.Send(doc)
	return err
}

func (t *TelegramChannel) EditMessage(msgID string, text string) error {
	// msgID 格式为 "chatID:messageID"
	parts := strings.SplitN(msgID, ":", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid msgID format: %s", msgID)
	}
	chatID, _ := strconv.ParseInt(parts[0], 10, 64)
	messageID, _ := strconv.Atoi(parts[1])

	// 截断到 4096 字符
	if len(text) > 4096 {
		text = text[:4096]
	}

	edit := tgbotapi.NewEditMessageText(chatID, messageID, text)
	_, err := t.bot.Send(edit)
	return err
}

func (t *TelegramChannel) NotifyDone(chatID string, text string) error {
	// 发送新消息（触发手机推送通知）
	return t.Send(channel.OutgoingMessage{ChatID: chatID, Text: text})
}

// CleanupSession 清理指定 chat_id 的临时文件
func (t *TelegramChannel) CleanupSession(chatID string) {
	tmpDir := filepath.Join(os.TempDir(), "mini-code", chatID)
	os.RemoveAll(tmpDir)
}

// NewStreamingReply 实现 channel.StreamingWriter 接口
func (t *TelegramChannel) NewStreamingReply(chatID string) (channel.StreamingReply, error) {
	return NewStreamingReply(t, chatID)
}

// 编译期确认 *TelegramChannel 实现了 channel.StreamingWriter 接口
var _ channel.StreamingWriter = (*TelegramChannel)(nil)

// ParseAllowedUsers 解析逗号分隔的用户 ID 字符串
func ParseAllowedUsers(s string) []int64 {
	if s == "" {
		return nil
	}
	var ids []int64
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		id, err := strconv.ParseInt(part, 10, 64)
		if err != nil {
			continue // 跳过非法 ID
		}
		ids = append(ids, id)
	}
	return ids
}

// SplitMessage 将长消息分割为不超过 maxLen 字符的片段
func SplitMessage(text string, maxLen int) []string {
	if len(text) <= maxLen {
		return []string{text}
	}
	var parts []string
	for len(text) > 0 {
		if len(text) <= maxLen {
			parts = append(parts, text)
			break
		}
		parts = append(parts, text[:maxLen])
		text = text[maxLen:]
	}
	return parts
}

// downloadURL 下载 URL 到本地文件
func downloadURL(url, localPath string) error {
	// 使用标准库 http 下载
	resp, err := http.Get(url) //nolint:gosec
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	f, err := os.Create(localPath)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(f, resp.Body)
	return err
}
```

- [ ] **Step 2.4: 运行测试**

```bash
go test ./pkg/channel/telegram/... -v
```
预期：白名单测试和消息分割测试 PASS

- [ ] **Step 2.5: Commit**

```bash
git add pkg/channel/telegram/bot.go pkg/channel/telegram/bot_test.go
git commit -m "feat(telegram): 实现 Telegram Bot Channel 基础（Long Polling + 白名单 + 附件下载）"
```

---

## Task 3: 流式响应刷新

**Files:**
- Create: `pkg/channel/telegram/streaming.go`

- [ ] **Step 3.1: 创建 streaming.go**

```go
// pkg/channel/telegram/streaming.go
package telegram

import (
	"fmt"
	"mini-code/pkg/channel"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// StreamingReply 管理 Telegram 流式响应（周期性 edit_message）
type StreamingReply struct {
	ch        *TelegramChannel
	chatID    string
	messageID string // "chatID:msgID" 格式

	mu      sync.Mutex
	content strings.Builder
	done    chan struct{}
}

// NewStreamingReply 发送初始占位消息，开始周期刷新
func NewStreamingReply(ch *TelegramChannel, chatID string) (*StreamingReply, error) {
	// 先发一条带省略号的消息作为"容器"
	msg, err := ch.bot.Send(newMessage(chatID, "..."))
	if err != nil {
		return nil, fmt.Errorf("发送初始消息失败: %w", err)
	}

	sr := &StreamingReply{
		ch:        ch,
		chatID:    chatID,
		messageID: fmt.Sprintf("%s:%d", chatID, msg.MessageID),
		done:      make(chan struct{}),
	}
	go sr.refreshLoop()
	return sr, nil
}

// Write 追加流式内容
func (sr *StreamingReply) Write(content string) {
	sr.mu.Lock()
	sr.content.WriteString(content)
	sr.mu.Unlock()
}

// Complete 完成流式输出，停止刷新，发送最终完整通知
// 实现 channel.StreamingReply 接口
func (sr *StreamingReply) Complete(success bool) {
	close(sr.done)

	// 最终更新消息内容
	sr.mu.Lock()
	finalContent := sr.content.String()
	sr.mu.Unlock()

	_ = sr.ch.EditMessage(sr.messageID, finalContent)

	// 发送新消息触发手机推送通知
	if success {
		_ = sr.ch.NotifyDone(sr.chatID, "✅ 任务完成")
	} else {
		_ = sr.ch.NotifyDone(sr.chatID, "❌ 任务出错，请查看上方输出")
	}
}

// 编译期确认 *StreamingReply 实现了 channel.StreamingReply 接口
var _ channel.StreamingReply = (*StreamingReply)(nil)

// refreshLoop 每 1.5 秒刷新一次消息
func (sr *StreamingReply) refreshLoop() {
	ticker := time.NewTicker(1500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-sr.done:
			return
		case <-ticker.C:
			sr.mu.Lock()
			content := sr.content.String()
			sr.mu.Unlock()

			if content == "" {
				continue
			}
			// EditMessage 失败时静默忽略（常见：消息内容未变化）
			_ = sr.ch.EditMessage(sr.messageID, content+"▌") // 追加光标
		}
	}
}

func newMessage(chatID string, text string) tgbotapi.MessageConfig {
	// 解析 chatID 为 int64
	var id int64
	fmt.Sscanf(chatID, "%d", &id)
	return tgbotapi.NewMessage(id, text)
}
```

- [ ] **Step 3.2: Commit**

```bash
git add pkg/channel/telegram/streaming.go
git commit -m "feat(telegram): 实现流式响应刷新（edit_message 每 1.5s）"
```

---

## Task 4: Telegram 命令处理

**Files:**
- Create: `pkg/channel/telegram/commands.go`

- [ ] **Step 4.1: 写命令测试**

```go
// pkg/channel/telegram/commands_test.go
package telegram_test

import (
	"testing"
	"mini-code/pkg/channel/telegram"
)

func TestIsBotCommand(t *testing.T) {
	cases := []struct {
		text     string
		expected bool
	}{
		{"/start", true},
		{"/reset", true},
		{"/memory", true},
		{"/status", true},
		{"/cancel", true},
		{"hello world", false},
		{"/unknown", false},
	}
	for _, tt := range cases {
		result := telegram.IsBotCommand(tt.text)
		if result != tt.expected {
			t.Errorf("IsBotCommand(%q) = %v, want %v", tt.text, result, tt.expected)
		}
	}
}
```

- [ ] **Step 4.2: 创建 commands.go**

```go
// pkg/channel/telegram/commands.go
package telegram

import "strings"

// 支持的 Bot 命令列表
var supportedCommands = map[string]bool{
	"/start":  true,
	"/reset":  true,
	"/memory": true,
	"/status": true,
	"/cancel": true,
}

// IsBotCommand 判断消息是否为已知 Bot 命令
func IsBotCommand(text string) bool {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return false
	}
	cmd := strings.ToLower(fields[0])
	return supportedCommands[cmd]
}

// CommandType 解析命令类型
func CommandType(text string) string {
	if len(text) == 0 {
		return ""
	}
	fields := strings.Fields(text)
	return strings.ToLower(fields[0])
}
```

命令实际执行逻辑（/reset、/cancel、/memory、/status）在 Orchestrator 的 Handle 方法中处理，TelegramChannel 仅负责识别命令并通过 IncomingMessage 传递，由 Orchestrator 路由到对应处理函数。

- [ ] **Step 4.3: 运行测试**

```bash
go test ./pkg/channel/telegram/... -v
```
预期：PASS

- [ ] **Step 4.4: Commit**

```bash
git add pkg/channel/telegram/commands.go pkg/channel/telegram/commands_test.go
git commit -m "feat(telegram): 实现 Bot 命令识别（/reset /memory /status /cancel）"
```

---

## Task 5: 更新 Orchestrator 处理 Telegram 命令

**Files:**
- Modify: `pkg/orchestrator/orchestrator.go`

- [ ] **Step 5.1: 在 Handle 中拦截命令消息**

在 `Handle()` 方法开头添加命令检测：

```go
func (o *Orchestrator) Handle(ctx context.Context, msg channel.IncomingMessage, factory AgentFactory, ch channel.Channel) error {
	session := o.GetOrCreateSession(msg.ChannelID, msg.UserID)

	// 处理 Bot 命令（Telegram /命令 和 CLI 内置命令统一在此处理）
	if handled, err := o.handleCommand(ctx, msg, session, ch); handled {
		return err
	}
	// ... 正常 Agent 处理流程
}

func (o *Orchestrator) handleCommand(ctx context.Context, msg channel.IncomingMessage, session *Session, ch channel.Channel) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(msg.Text)) {
	case "/reset", "reset", "r", "new", "n":
		session.Reset()
		// 清理临时文件（如果是 Telegram channel）
		if tc, ok := ch.(interface{ CleanupSession(string) }); ok {
			tc.CleanupSession(msg.ChannelID)
		}
		return true, ch.NotifyDone(msg.ChannelID, "会话已重置，上下文已清空（长期记忆保留）")

	case "/cancel", "cancel":
		session.Cancel()
		return true, ch.NotifyDone(msg.ChannelID, "任务已取消")

	case "/status", "status", "history", "hist":
		agentType, turn, lastTool := session.StatusInfo()
		status := fmt.Sprintf("Agent: %s | 已执行轮次: %d | 最近工具: %s",
			agentType, turn, lastTool)
		return true, ch.Send(channel.OutgoingMessage{ChatID: msg.ChannelID, Text: status})

	case "/memory", "memory":
		if o.memStore == nil {
			return true, ch.Send(channel.OutgoingMessage{ChatID: msg.ChannelID, Text: "记忆系统未启用"})
		}
		workspace, _ := os.Getwd() // 不使用 tools.workspaceRoot() 避免循环依赖
		suffix := o.memStore.BuildPromptSuffix(workspace)
		if suffix == "" {
			suffix = "暂无记忆"
		}
		return true, ch.Send(channel.OutgoingMessage{ChatID: msg.ChannelID, Text: suffix})

	case "/start":
		workspace, _ := os.Getwd()
		welcome := fmt.Sprintf("👋 Mini-Code 已就绪\n工作区: %s\n输入任务开始工作", workspace)
		return true, ch.Send(channel.OutgoingMessage{ChatID: msg.ChannelID, Text: welcome})
	}

	return false, nil
}
```

- [ ] **Step 5.2: 在 Handle 集成 Telegram 流式刷新**

在 Handle 方法中，通过 `channel.StreamingWriter` 接口判断，**不直接 import telegram 包**，避免循环依赖：

```go
// Handle 方法中的流式输出策略选择
var handler agent.StreamChunkHandler
var streamingReply channel.StreamingReply
success := true

if sw, ok := ch.(channel.StreamingWriter); ok {
	// Telegram Channel：通过接口创建流式回复容器
	sr, err := sw.NewStreamingReply(msg.ChannelID)
	if err != nil {
		return err
	}
	streamingReply = sr
	handler = agent.StreamChunkHandler(func(content string, done bool) error {
		if !done {
			sr.Write(content)
		}
		return nil
	})
} else {
	// CLI Channel：直接流式打印到终端
	ui.PrintAssistantLabel()
	streaming := ui.NewStreamingText()
	handler = agent.StreamChunkHandler(func(content string, done bool) error {
		if !done && content != "" {
			streaming.Write(content)
		}
		return nil
	})
	defer streaming.Complete()
}

// 执行 Agent
reply, newMsgs, err := agnt.Run(runCtx, session.Messages(), handler)

// 完成流式回复（在 err 判断之后，以便传递 success=false）
if streamingReply != nil {
	success = err == nil && runCtx.Err() == nil
	streamingReply.Complete(success)
}
```

> **注意：** orchestrator.go 中**不需要** import `pkg/channel/telegram`，仅需 `pkg/channel` 即可。这是避免循环依赖的关键。

- [ ] **Step 5.3: 运行全部测试**

```bash
go test ./... -count=1
```
预期：全部 PASS

- [ ] **Step 5.4: Commit**

```bash
git add pkg/orchestrator/orchestrator.go
git commit -m "feat(orchestrator): 集成 Telegram 流式刷新和命令处理"
```

---

## Task 6: main.go 支持 --channel 启动参数

**Files:**
- Modify: `cmd/agent/main.go`

- [ ] **Step 6.1: 添加 --channel 参数解析**

```go
// cmd/agent/main.go
import (
	"flag"
	"mini-code/pkg/channel/telegram"
)

func main() {
	loadDotEnv(".env")

	// 命令行参数
	channelMode := flag.String("channel", "cli", "启动模式：cli | telegram")
	flag.Parse()

	// ... 初始化 provider、memory、orchestrator（与之前相同）

	switch *channelMode {
	case "telegram":
		runTelegramMode(ctx, orch, factory)
	default:
		runCLIMode(ctx, orch, factory)
	}
}

func runTelegramMode(ctx context.Context, orch *orchestrator.Orchestrator, factory orchestrator.AgentFactory) {
	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	if token == "" {
		ui.PrintError("缺少环境变量 TELEGRAM_BOT_TOKEN")
		os.Exit(1)
	}

	allowedUsers := telegram.ParseAllowedUsers(os.Getenv("TELEGRAM_ALLOWED_USERS"))
	tgCh, err := telegram.New(token, allowedUsers)
	if err != nil {
		ui.PrintError("Telegram Bot 初始化失败: %v", err)
		os.Exit(1)
	}

	ui.PrintSuccess("Telegram Bot 已启动，等待消息...")
	if len(allowedUsers) > 0 {
		ui.PrintInfo("白名单用户数: %d", len(allowedUsers))
	} else {
		ui.PrintInfo("警告：未设置 TELEGRAM_ALLOWED_USERS，所有用户均可访问")
	}

	// 在 goroutine 中处理消息
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-tgCh.Messages():
				if !ok {
					return
				}
				// 每条消息在新 goroutine 中处理（不同 chat_id 并发，同 chat_id 由 Session.mu 串行）
				go func(m channel.IncomingMessage) {
					if err := orch.Handle(ctx, m, factory, tgCh); err != nil {
						ui.PrintError("处理消息失败: %v", err)
					}
				}(msg)
			}
		}
	}()

	// 启动 Long Polling（阻塞）
	tgCh.Start(ctx)
}

func runCLIMode(ctx context.Context, orch *orchestrator.Orchestrator, factory orchestrator.AgentFactory) {
	rl, err := readline.NewEx(&readline.Config{
		Prompt:       ui.SprintColor(ui.User, "➜ "),
		HistoryFile:  ".mini-code-history",
		HistoryLimit: 1000,
		AutoComplete: getCompleter(),
	})
	if err != nil {
		ui.PrintError("初始化输入失败: %v", err)
		return
	}
	defer rl.Close()

	cliCh := cli.New(rl)
	printWelcome()

	go func() {
		if err := cliCh.Start(ctx); err != nil {
			ui.PrintError("CLI 输入出错: %v", err)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-cliCh.Messages():
			if !ok {
				ui.PrintInfo("再见！感谢使用 Mini-Code。")
				return
			}
			// handleBuiltinCommand 在 main.go 中定义，处理 reset/help/status/cancel
			if handleBuiltinCommand(msg.Text, orch, "cli", "local") {
				continue
			}
			if err := orch.Handle(ctx, msg, factory, cliCh); err != nil {
				ui.PrintError("运行失败: %v", err)
			}
			fmt.Println()
		}
	}
}
```

- [ ] **Step 6.2: 编译验证**

```bash
go build -o mini-code.exe ./cmd/agent && echo "BUILD OK"
```
预期：`BUILD OK`

- [ ] **Step 6.3: 运行全部测试**

```bash
go test ./... -count=1
```
预期：全部 PASS

- [ ] **Step 6.4: Commit**

```bash
git add cmd/agent/main.go
git commit -m "feat(main): 支持 --channel telegram 启动参数"
```

---

## Task 7: 手动验收测试

> 需要真实的 Telegram Bot Token，通过 @BotFather 创建。

- [ ] **Step 7.1: 配置环境变量**

```bash
# 在 .env 中添加
TELEGRAM_BOT_TOKEN=your_bot_token_here
TELEGRAM_ALLOWED_USERS=your_telegram_user_id
```

获取自己的 Telegram User ID：向 @userinfobot 发送任意消息，它会回复你的 ID。

- [ ] **Step 7.2: 启动 Telegram 模式**

```bash
go run ./cmd/agent --channel telegram
```
预期：`Telegram Bot 已启动，等待消息...`

- [ ] **Step 7.3: 验收清单**

在手机 Telegram 中测试：

```
□ 发送 "/start" → 收到欢迎消息 + 工作区信息
□ 发送 "列出当前目录下的所有 Go 文件" → 收到流式响应，消息每 1.5s 刷新
□ 任务完成后收到 "✅ 任务完成" 新消息（触发推送通知）
□ 发送 "/reset" → 收到会话重置确认
□ 发送 "记住：这个项目使用 Go 1.22" → Agent 调用 remember 工具
□ 发送 "/memory" → 看到刚才记住的信息
□ 发送长任务 → 发送 "/cancel" → 任务停止
□ 上传一个代码文件 → Agent 能读取并分析其内容
□ 发送一条来自未授权用户的消息（如果配置了白名单）→ 静默忽略
```

- [ ] **Step 7.4: 最终 Commit**

```bash
rm -f mini-code.exe
git add .
git commit -m "feat(phase4): 完成 Telegram Channel 全功能实现"
```

---

## Phase 4 验收标准

```bash
# 1. 全部自动化测试通过
go test ./... -count=1

# 2. 两种模式均可编译启动
go build ./cmd/agent
./mini-code          # CLI 模式（原有行为）
./mini-code --channel telegram  # Telegram 模式

# 3. 手动验收清单全部通过（见 Task 7.3）
```

---

## 全项目完成后的收尾检查

```bash
# 代码格式化
gofmt -w ./cmd ./pkg

# 静态检查
go vet ./...

# 全量测试 + 覆盖率
go test -cover ./...

# 清理临时文件
rm -f mini-code.exe mini-code lm_debug.log
```
