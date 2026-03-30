package orchestrator

import (
	"context"
	"fmt"
	"mini-code/pkg/agent"
	"mini-code/pkg/channel"
	"mini-code/pkg/memory"
	"mini-code/pkg/ui"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"
)

// StreamMode 流式输出模式
type StreamMode int

const (
	StreamModeCLI      StreamMode = iota // CLI 模式：直接输出到终端
	StreamModeTelegram                   // Telegram 模式：定时 EditMessage
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

// Orchestrator 管理 Session，路由消息到 Agent
type Orchestrator struct {
	sessions map[string]*Session
	mu        sync.Mutex
	memStore  *memory.Store
	router    *Router
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

	// 根据 Channel 类型选择不同的处理方式
	isTelegram := strings.HasPrefix(msg.ChannelID, "telegram:")
	
	if isTelegram {
		return o.handleTelegram(runCtx, session, agnt, msg, ch)
	}
	return o.handleCLI(runCtx, session, agnt, ch)
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

// handleTelegram Telegram 模式的处理（支持流式 EditMessage）
func (o *Orchestrator) handleTelegram(ctx context.Context, session *Session, agnt agent.Agent, msg channel.IncomingMessage, ch channel.Channel) error {
	chatID := msg.ChannelID
	
	// 发送初始占位消息
	msgID, err := ch.Send(channel.OutgoingMessage{
		ChatID: chatID,
		Text:  "⏳ 正在处理...",
	})
	if err != nil {
		return fmt.Errorf("send initial message: %w", err)
	}

	// 流式更新状态
	var contentBuffer strings.Builder
	var lastUpdate time.Time
	updateInterval := 1500 * time.Millisecond // Telegram 更新间隔 1.5s

	// 创建流式处理器
	handler := agent.StreamChunkHandler(func(content string, done bool) error {
		if done {
			// 流结束，更新最终消息内容
			fullContent := contentBuffer.String()
			if fullContent == "" {
				fullContent = "✅ 任务完成"
			}
			// 更新消息为最终内容
			if msgID != "" {
				ch.EditMessage(msgID, fullContent)
			}
			return nil
		}

		contentBuffer.WriteString(content)

		// 定时更新消息（避免频繁调用 API，每 1.5 秒更新一次）
		if time.Since(lastUpdate) > updateInterval && contentBuffer.Len() > 0 {
			text := contentBuffer.String()
			// Telegram 消息长度限制
			if len(text) > 4000 {
				text = text[:3990] + "..."
			}
			if msgID != "" {
				ch.EditMessage(msgID, text)
			}
			lastUpdate = time.Now()
		}
		return nil
	})

	// 执行 Agent
	reply, newMsgs, err := agnt.Run(ctx, session.Messages(), handler)

	// 将新消息追加到 Session
	if len(newMsgs) > 0 {
		session.AppendMessages(newMsgs)
	}

	if err != nil {
		if ctx.Err() != nil {
			// 任务被取消，更新消息并通知
			ch.EditMessage(msgID, "🛑 任务已取消")
			ch.NotifyDone(chatID, "任务已取消")
			return nil
		}
		// 发送错误消息
		ch.EditMessage(msgID, fmt.Sprintf("❌ 执行失败: %v", err))
		return err
	}

	// 发送完成通知（触发手机推送）
	// 这是一个简短的完成提示，实际内容已通过 EditMessage 更新
	if reply != "" {
		// 如果 reply 有内容，说明已通过流式更新显示
		// 发送简短完成通知触发推送
		ch.NotifyDone(chatID, "✅ 任务已完成")
	}
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
	osName := runtime.GOOS
	var shellHint string
	switch osName {
	case "windows":
		shellHint = "如果必须执行 shell 命令，请使用 Windows CMD 语法。"
	default:
		shellHint = "如果必须执行 shell 命令，请使用 Unix shell 语法。"
	}

	basePrompt := fmt.Sprintf(`你是一个专业的中文 AI 编程助手 Mini-Code。你的核心职责是帮助用户完成各类软件开发任务。

## 运行环境
- 操作系统: %s
- %s

## 核心工作流程

### 1. 理解项目（必须首先执行）
在开始任何代码修改之前，你必须先理解项目结构：
- 使用 list_files 浏览目录结构，了解项目组织方式
- 使用 search_in_files 搜索关键代码，定位相关模块
- 使用 read_file 阅读关键文件，理解现有实现

### 2. 分析需求
- 仔细理解用户的任务目标
- 识别需要修改的文件和范围
- 考虑对现有代码的影响

### 3. 执行修改
- 优先使用 replace_in_file 进行最小化修改，保持代码的一致性和可追溯性
- 只有在创建新文件或文件需要大规模重写时才使用 write_file
- 保持代码风格与项目现有风格一致

### 4. 验证结果
- 修改完成后，运行 go test ./... 验证代码正确性
- 如果测试失败，分析错误并修复

## 工具使用规范

### 文件操作
- 所有文件路径必须是工作区内的相对路径，禁止访问工作区外的文件
- 使用 replace_in_file 时，old 参数必须与文件中的原始文本完全匹配
- 写入文件时保持合理的缩进和格式

### Shell 命令
- 仅在必要时使用 shell 命令
- 命令应该简洁明确，避免复杂的管道操作
- 注意处理命令的输出和错误

### 搜索操作
- 使用 search_in_files 时，query 应该精确匹配目标文本
- 可以通过 path 参数限定搜索范围，提高效率

## 代码质量要求

1. **保持一致性**: 新代码应与现有代码风格保持一致
2. **最小修改原则**: 只修改必要的部分，避免不必要的重构
3. **错误处理**: 适当处理边界情况和错误
4. **代码注释**: 在复杂逻辑处添加必要的注释

## 沟通规范

1. 用中文回复用户
2. 说明你正在执行的操作和原因
3. 如果遇到问题，清晰地描述问题并提供解决建议
4. 完成任务后简要总结所做的修改`, osName, shellHint)

	// 注入记忆后缀
	if o.memStore != nil {
		workspace, _ := os.Getwd()
		suffix := o.memStore.BuildPromptSuffix(workspace)
		if suffix != "" {
			basePrompt += suffix
		}
	}

	return basePrompt
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