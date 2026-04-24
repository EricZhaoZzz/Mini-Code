package telegram

import (
	"context"
	"fmt"
	"mini-code/pkg/channel"
	"mini-code/pkg/memory"
	"mini-code/pkg/orchestrator"
	"sync"
)

// Runner Telegram Bot 运行器
type Runner struct {
	channel     *TelegramChannel
	cmdHandler  *CommandHandler
	orch        *orchestrator.Orchestrator
	memStore    *memory.Store
	factory     orchestrator.AgentFactory

	// 会话管理
	sessions    map[string]*sessionState
	sessionsMu  sync.Mutex
}

type sessionState struct {
	running bool
	cancel  context.CancelFunc
}

// NewRunner 创建 Telegram 运行器
func NewRunner(cfg Config, memStore *memory.Store) (*Runner, error) {
	ch, err := New(cfg)
	if err != nil {
		return nil, err
	}

	orch := orchestrator.New(memStore)

	return &Runner{
		channel:    ch,
		orch:      orch,
		memStore:  memStore,
		sessions:  make(map[string]*sessionState),
	}, nil
}

// SetAgentFactory 设置 Agent 工厂
func (r *Runner) SetAgentFactory(factory orchestrator.AgentFactory) {
	r.factory = factory
}

// Start 启动 Telegram Bot
func (r *Runner) Start(ctx context.Context) error {
	// 初始化命令处理器
	r.cmdHandler = NewCommandHandler(r.orch, r.memStore)

	// 注册命令菜单（让用户在客户端看到可用命令）
	if err := r.channel.RegisterCommands(); err != nil {
		// 注册失败不阻止启动，只打印警告
		fmt.Printf("⚠️ 注册命令菜单失败: %v\n", err)
	} else {
		fmt.Println("✅ 已注册命令菜单")
	}

	// 启动消息处理循环
	go r.processMessages(ctx)

	// 启动 Telegram 长轮询
	return r.channel.Start(ctx)
}

// processMessages 处理接收到的消息
func (r *Runner) processMessages(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-r.channel.Messages():
			if !ok {
				return
			}
			go r.handleMessage(ctx, msg)
		}
	}
}

// handleMessage 处理单条消息
func (r *Runner) handleMessage(ctx context.Context, msg channel.IncomingMessage) {
	// 获取或创建会话
	session := r.orch.GetOrCreateSession(msg.ChannelID, msg.UserID)

	// 检查是否有正在运行的任务
	r.sessionsMu.Lock()
	state, exists := r.sessions[msg.ChannelID]
	if exists && state.running {
		r.sessionsMu.Unlock()
		r.channel.NotifyDone(msg.ChannelID, "⚠️ 当前有任务正在执行，请等待完成或发送 /cancel 取消。")
		return
	}
	// 创建新的会话状态
	state = &sessionState{running: true}
	r.sessions[msg.ChannelID] = state
	r.sessionsMu.Unlock()

	// 清理函数
	defer func() {
		r.sessionsMu.Lock()
		delete(r.sessions, msg.ChannelID)
		r.sessionsMu.Unlock()
	}()

	// 处理命令
	if cmdResult := r.cmdHandler.Handle(msg, session); cmdResult.Handled {
		if cmdResult.ResetSession {
			r.orch.RefreshSessionPrompt(msg.ChannelID, msg.UserID)
		}
		if cmdResult.CancelTask {
			// 取消当前任务（如果有）
			session.Cancel()
		}
		if cmdResult.Response != "" {
			if _, err := r.channel.Send(channel.OutgoingMessage{
				ChatID: msg.ChannelID,
				Text:   cmdResult.Response,
			}); err != nil {
				fmt.Printf("❌ 发送命令响应失败: %v\n", err)
			}
		}
		return
	}

	// 创建可取消的任务上下文
	taskCtx, cancel := context.WithCancel(ctx)
	state.cancel = cancel
	defer cancel()

	// 执行 Agent
	if r.factory == nil {
		r.channel.NotifyDone(msg.ChannelID, "❌ Agent 工厂未初始化")
		return
	}

	if err := r.orch.Handle(taskCtx, msg, r.factory, r.channel); err != nil {
		r.channel.NotifyDone(msg.ChannelID, fmt.Sprintf("❌ 执行失败: %v", err))
	}
}

// CancelSession 取消指定会话的当前任务
func (r *Runner) CancelSession(channelID string) {
	r.sessionsMu.Lock()
	defer r.sessionsMu.Unlock()

	if state, exists := r.sessions[channelID]; exists && state.cancel != nil {
		state.cancel()
	}
}

// GetChannel 获取 Telegram Channel
func (r *Runner) GetChannel() *TelegramChannel {
	return r.channel
}

// GetOrchestrator 获取 Orchestrator
func (r *Runner) GetOrchestrator() *orchestrator.Orchestrator {
	return r.orch
}