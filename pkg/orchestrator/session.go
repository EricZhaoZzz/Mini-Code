package orchestrator

import (
	"context"
	"sync"
	"time"

	"github.com/sashabaranov/go-openai"
)

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

// UpdateSystemPrompt 更新系统提示
func (s *Session) UpdateSystemPrompt(prompt string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.messages) > 0 && s.messages[0].Role == openai.ChatMessageRoleSystem {
		s.messages[0].Content = prompt
	}
}