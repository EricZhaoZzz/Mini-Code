package orchestrator

import (
	"context"
	"mini-code/pkg/agent"
	"mini-code/pkg/channel"
	"mini-code/pkg/memory"
	"mini-code/pkg/ui"
	"sync"
	"time"

	"github.com/sashabaranov/go-openai"
)

// StreamMode 流式输出模式
type StreamMode int

const (
	StreamModeCLI StreamMode = iota // CLI 模式：直接输出到终端
)

// StreamHandler 流式输出处理器接口
type StreamHandler interface {
	// OnContent 收到内容块
	OnContent(content string) error
	// OnComplete 流式输出完成
	OnComplete(fullContent string) error
	// OnToolCall 工具调用开始
	OnToolCall(toolName string, args string)
	// OnToolResult 工具调用结果
	OnToolResult(toolName string, result string, err error)
	// OnWaiting 等待模型响应
	OnWaiting()
}

type RestartRuntime interface {
	HasPendingRestart() bool
	ApplyPendingRestart(snapshot SessionSnapshot) error
}

// Orchestrator 管理 Session，路由消息到 Agent
type Orchestrator struct {
	sessions map[string]*Session
	mu       sync.Mutex
	memStore *memory.Store
	router   *Router
	restart  RestartRuntime
}

// New 创建新的 Orchestrator
func New(memStore *memory.Store) *Orchestrator {
	o := &Orchestrator{
		sessions: make(map[string]*Session),
		memStore: memStore,
		router:   NewRouter(),
	}
	go o.evictLoop() // 后台清理不活跃 Session
	return o
}

// GetOrCreateSession 获取或创建 Session
func (o *Orchestrator) GetOrCreateSession(channelID, userID string) *Session {
	key := channelID + ":" + userID
	o.mu.Lock()
	defer o.mu.Unlock()
	if s, ok := o.sessions[key]; ok {
		return s
	}
	s := newSession(channelID, userID, o.buildSystemPrompt())
	o.sessions[key] = s
	return s
}

func (o *Orchestrator) RestoreSession(snapshot SessionSnapshot) *Session {
	key := snapshot.ChannelID + ":" + snapshot.UserID
	o.mu.Lock()
	defer o.mu.Unlock()

	session := newSessionFromSnapshot(snapshot)
	if len(session.messages) == 0 {
		session.messages = []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: o.buildSystemPrompt()},
		}
	} else if session.messages[0].Role == openai.ChatMessageRoleSystem && session.messages[0].Content == "" {
		session.messages[0].Content = o.buildSystemPrompt()
	}

	o.sessions[key] = session
	return session
}

// AgentFactory 根据类型创建 Agent
type AgentFactory func(agentType string) agent.Agent

// Handle 处理一条到来的消息
func (o *Orchestrator) Handle(ctx context.Context, msg channel.IncomingMessage, factory AgentFactory, ch channel.Channel) error {
	session := o.GetOrCreateSession(msg.ChannelID, msg.UserID)

	// 路由到合适的 Agent 类型
	agentType := o.router.Route(msg.Text)
	session.mu.Lock()
	session.agentType = agentType
	session.mu.Unlock()

	agnt := factory(agentType)

	// 追加用户消息到 Session
	session.AppendUserMessage(msg.Text)

	// 创建可取消 context，存入 Session 供 /cancel 使用
	runCtx, cancel := context.WithCancel(ctx)
	session.SetCancel(cancel)
	defer cancel()

	if err := o.handleCLI(runCtx, session, agnt, ch); err != nil {
		return err
	}
	return o.applyPendingRestart(session)
}

// handleCLI CLI 模式的处理
func (o *Orchestrator) handleCLI(ctx context.Context, session *Session, agnt agent.Agent, ch channel.Channel) error {
	// 创建流式输出处理器
	ui.PrintAssistantLabel()
	streaming := ui.NewStreamingText()

	handler := agent.StreamChunkHandler(func(content string, done bool) error {
		if !done && content != "" {
			streaming.Write(content)
		}
		return nil
	})

	// 执行 Agent
	reply, newMsgs, err := agnt.Run(ctx, session.Messages(), handler)
	streaming.Complete()

	// 将新消息追加到 Session
	if len(newMsgs) > 0 {
		session.AppendMessages(newMsgs)
	}

	if err != nil {
		if ctx.Err() != nil {
			ch.NotifyDone(session.ChannelID, "任务已取消")
			return nil
		}
		return err
	}
	_ = reply // 已通过 streaming 输出
	return nil
}

// evictLoop 每小时清理超过 24h 不活跃的 Session
func (o *Orchestrator) evictLoop() {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for range ticker.C {
		o.mu.Lock()
		for key, s := range o.sessions {
			if time.Since(s.lastSeen) > 24*time.Hour {
				delete(o.sessions, key)
			}
		}
		o.mu.Unlock()
	}
}

// buildSystemPrompt 构建系统提示
func (o *Orchestrator) buildSystemPrompt() string {
	return o.buildSystemPromptWithMemory()
}

// buildSystemPromptWithMemory 构建带记忆的系统提示
func (o *Orchestrator) buildSystemPromptWithMemory() string {
	return agent.BuildSystemPromptWithMemory(o.memStore)
}

// GetMemoryStore 获取记忆存储
func (o *Orchestrator) GetMemoryStore() *memory.Store {
	return o.memStore
}

// RefreshSessionPrompt 刷新会话的系统提示（在记忆变化后调用）
func (o *Orchestrator) RefreshSessionPrompt(channelID, userID string) {
	o.mu.Lock()
	key := channelID + ":" + userID
	s, ok := o.sessions[key]
	o.mu.Unlock()

	if ok {
		s.UpdateSystemPrompt(o.buildSystemPrompt())
	}
}

func (o *Orchestrator) SetRestartRuntime(runtime RestartRuntime) {
	o.restart = runtime
}

func (o *Orchestrator) applyPendingRestart(session *Session) error {
	if o.restart == nil || !o.restart.HasPendingRestart() {
		return nil
	}
	return o.restart.ApplyPendingRestart(session.ExportSnapshot())
}
