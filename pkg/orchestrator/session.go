package orchestrator

import (
	"context"
	"sync"
	"time"

	"github.com/sashabaranov/go-openai"
)

type SessionSnapshot struct {
	ID        string                         `json:"id"`
	ChannelID string                         `json:"channel_id"`
	UserID    string                         `json:"user_id"`
	AgentType string                         `json:"agent_type"`
	Messages  []openai.ChatCompletionMessage `json:"messages"`
	LastSeen  time.Time                      `json:"last_seen"`
}

// Session 管理单个用户的对话状态
type Session struct {
	ID        string
	ChannelID string
	UserID    string
	agentType string
	messages  []openai.ChatCompletionMessage
	cancel    context.CancelFunc
	lastSeen  time.Time
	mu        sync.Mutex

	// summary 是被压缩掉的早期历史摘要，跨轮复用，避免每轮重新摘要
	summary string

	// 运行时状态（用于 /status 命令）
	currentTurn  int
	lastToolName string
}

// newSession 创建新 Session
func newSession(channelID, userID, systemPrompt string) *Session {
	return &Session{
		ID:        channelID + ":" + userID,
		ChannelID: channelID,
		UserID:    userID,
		agentType: "coder",
		messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: systemPrompt},
		},
		lastSeen: time.Now(),
	}
}

// Messages 返回消息历史副本
func (s *Session) Messages() []openai.ChatCompletionMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]openai.ChatCompletionMessage, len(s.messages))
	copy(cp, s.messages)
	return cp
}

// AppendUserMessage 追加用户消息
func (s *Session) AppendUserMessage(text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleUser,
		Content: text,
	})
	s.lastSeen = time.Now()
}

// AppendMessages 批量追加消息
func (s *Session) AppendMessages(msgs []openai.ChatCompletionMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, msgs...)
}

// Reset 重置会话，保留系统消息
func (s *Session) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	// 保留 system 消息
	if len(s.messages) > 0 && s.messages[0].Role == openai.ChatMessageRoleSystem {
		s.messages = []openai.ChatCompletionMessage{s.messages[0]}
	} else {
		s.messages = nil
	}
	// 历史摘要同样失效，否则会残留上一段对话的内容
	s.summary = ""
}

// SetCancel 设置取消函数
func (s *Session) SetCancel(cancel context.CancelFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancel = cancel
}

// Cancel 取消当前运行
func (s *Session) Cancel() {
	s.mu.Lock()
	f := s.cancel
	s.mu.Unlock()
	if f != nil {
		f()
	}
}

// StatusInfo 返回状态信息
func (s *Session) StatusInfo() (agentType string, turn int, lastTool string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.agentType, s.currentTurn, s.lastToolName
}

// MessageCount 返回消息数量
func (s *Session) MessageCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.messages)
}

// LastSeen 返回最后活跃时间。
//
// 必须通过它访问 lastSeen：后台 evict 循环会读该字段，
// 直接访问会与 AppendUserMessage / AppendMessages 的写产生数据竞争。
func (s *Session) LastSeen() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastSeen
}

// UpdateSystemPrompt 更新系统提示
func (s *Session) UpdateSystemPrompt(prompt string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.messages) > 0 && s.messages[0].Role == openai.ChatMessageRoleSystem {
		s.messages[0].Content = prompt
	}
}

// Summary 返回历史摘要
func (s *Session) Summary() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.summary
}

// SetSummary 设置历史摘要
func (s *Session) SetSummary(summary string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.summary = summary
}

// ReplaceMessages 整体替换消息历史（上下文压缩后调用）
func (s *Session) ReplaceMessages(msgs []openai.ChatCompletionMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]openai.ChatCompletionMessage, len(msgs))
	copy(cp, msgs)
	s.messages = cp
}

func (s *Session) ExportSnapshot() SessionSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()

	messages := make([]openai.ChatCompletionMessage, len(s.messages))
	copy(messages, s.messages)

	return SessionSnapshot{
		ID:        s.ID,
		ChannelID: s.ChannelID,
		UserID:    s.UserID,
		AgentType: s.agentType,
		Messages:  messages,
		LastSeen:  s.lastSeen,
	}
}

func newSessionFromSnapshot(snapshot SessionSnapshot) *Session {
	messages := make([]openai.ChatCompletionMessage, len(snapshot.Messages))
	copy(messages, snapshot.Messages)

	lastSeen := snapshot.LastSeen
	if lastSeen.IsZero() {
		lastSeen = time.Now()
	}

	agentType := snapshot.AgentType
	if agentType == "" {
		agentType = "coder"
	}

	return &Session{
		ID:        snapshot.ID,
		ChannelID: snapshot.ChannelID,
		UserID:    snapshot.UserID,
		agentType: agentType,
		messages:  messages,
		lastSeen:  lastSeen,
	}
}
