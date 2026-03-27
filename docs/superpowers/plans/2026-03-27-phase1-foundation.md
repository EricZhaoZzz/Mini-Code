# Phase 1: 基础重构 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将现有 ClawEngine 重构为分层架构（Provider / Agent / Channel / Orchestrator），保持全部现有功能不变，为后续 Phase 打基础。

**Architecture:** 提取 Provider 接口解耦 LLM 调用；将 ClawEngine 重构为 BaseAgent 实现 Agent 接口，消息历史上移至 Session 管理；提取 CLI readline 逻辑为 CLIChannel 实现 Channel 接口；引入 Orchestrator 骨架串联整体。

**Tech Stack:** Go 1.22，go-openai v1，ergochat/readline，标准库 context/sync

---

## 文件变动清单

| 操作 | 文件 | 说明 |
|------|------|------|
| Create | `pkg/provider/interface.go` | Provider 接口 + 请求/响应类型 |
| Create | `pkg/provider/openai.go` | OpenAI 兼容实现（从 engine.go 提取） |
| Create | `pkg/provider/openai_test.go` | Provider 接口契约测试 |
| Create | `pkg/agent/agent.go` | Agent 接口定义 |
| Modify | `pkg/agent/engine.go` → `pkg/agent/base.go` | 重命名 + 接受外部 messages 参数 |
| Modify | `pkg/agent/engine_test.go` | 迁移测试到新签名 |
| Create | `pkg/channel/types.go` | Channel 接口 + Message 类型 |
| Create | `pkg/channel/cli/runner.go` | CLI Channel 实现 |
| Create | `pkg/channel/cli/runner_test.go` | CLI Channel 测试 |
| Create | `pkg/orchestrator/session.go` | Session 类型 + 内存管理 |
| Create | `pkg/orchestrator/orchestrator.go` | Orchestrator 骨架 |
| Create | `pkg/orchestrator/orchestrator_test.go` | Orchestrator 集成测试 |
| Modify | `cmd/agent/main.go` | 替换为 CLIChannel + Orchestrator 启动 |

---

## Task 1: Provider 接口与 OpenAI 实现

**Files:**
- Create: `pkg/provider/interface.go`
- Create: `pkg/provider/openai.go`
- Create: `pkg/provider/openai_test.go`

- [ ] **Step 1.1: 写失败测试**

```go
// pkg/provider/openai_test.go
package provider_test

import (
	"context"
	"testing"
	"mini-code/pkg/provider"
	"github.com/sashabaranov/go-openai"
)

// 验证 OpenAIProvider 实现了 Provider 接口（编译期检查）
var _ provider.Provider = (*provider.OpenAIProvider)(nil)

func TestNewOpenAIProvider_ReturnsProvider(t *testing.T) {
	cfg := openai.DefaultConfig("test-key")
	cfg.BaseURL = "http://localhost:9999/v1"
	p := provider.NewOpenAIProvider(cfg, "test-model")
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
}
```

- [ ] **Step 1.2: 运行测试确认失败**

```bash
cd C:/Users/Eric/dev/mini-code && go test ./pkg/provider/...
```
预期：`cannot find package` 或 `undefined: provider.Provider`

- [ ] **Step 1.3: 创建 Provider 接口**

```go
// pkg/provider/interface.go
package provider

import (
	"context"
	"github.com/sashabaranov/go-openai"
)

// Provider 抽象 LLM 调用，解耦具体 SDK
type Provider interface {
	Chat(ctx context.Context, req openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error)
	ChatStream(ctx context.Context, req openai.ChatCompletionRequest) (*openai.ChatCompletionStream, error)
}
```

- [ ] **Step 1.4: 创建 OpenAI 实现**

```go
// pkg/provider/openai.go
package provider

import (
	"context"
	"github.com/sashabaranov/go-openai"
)

type OpenAIProvider struct {
	client *openai.Client
	model  string
}

func NewOpenAIProvider(cfg openai.ClientConfig, model string) *OpenAIProvider {
	return &OpenAIProvider{
		client: openai.NewClientWithConfig(cfg),
		model:  model,
	}
}

func (p *OpenAIProvider) Model() string { return p.model }

func (p *OpenAIProvider) Chat(ctx context.Context, req openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error) {
	return p.client.CreateChatCompletion(ctx, req)
}

func (p *OpenAIProvider) ChatStream(ctx context.Context, req openai.ChatCompletionRequest) (*openai.ChatCompletionStream, error) {
	return p.client.CreateChatCompletionStream(ctx, req)
}
```

- [ ] **Step 1.5: 运行测试确认通过**

```bash
go test ./pkg/provider/... -v
```
预期：`PASS`

- [ ] **Step 1.6: Commit**

```bash
git add pkg/provider/
git commit -m "feat(provider): 添加 Provider 接口和 OpenAI 实现"
```

---

## Task 2: Agent 接口 + BaseAgent 重构

**Files:**
- Create: `pkg/agent/agent.go`
- Modify: `pkg/agent/engine.go` → `pkg/agent/base.go`（重命名并修改）
- Modify: `pkg/agent/engine_test.go`

> **重要：** BaseAgent.Run() 接受外部传入的完整消息历史，不再内部维护。这是与现有 ClawEngine 最大的区别。

- [ ] **Step 2.1: 定义 Agent 接口**

```go
// pkg/agent/agent.go
package agent

import (
	"context"
	"github.com/sashabaranov/go-openai"
)

// StreamChunkHandler 流式响应回调（保持与原有签名一致）
type StreamChunkHandler func(content string, done bool) error

// Agent 接口：接受完整消息历史，返回最终响应内容
// 消息历史由调用方（Orchestrator）管理，Agent 不持有状态
type Agent interface {
	// Run 执行一轮完整对话（含工具调用循环），直到 finish_reason=stop
	// messages: 完整消息历史（含 system prompt 和注入的记忆）
	// 返回最终 assistant 消息内容，以及执行过程中新增的所有消息（供调用方追加到历史）
	Run(ctx context.Context, messages []openai.ChatCompletionMessage, handler StreamChunkHandler) (reply string, newMessages []openai.ChatCompletionMessage, err error)
	Name() string
	AllowedTools() []string
}
```

- [ ] **Step 2.2: 写 BaseAgent 测试（迁移现有测试）**

```go
// pkg/agent/base_test.go
package agent

import (
	"context"
	"fmt"
	"testing"
	"mini-code/pkg/tools"
	"github.com/sashabaranov/go-openai"
)

// fakeChatCompletionClient 保持与现有 engine_test.go 一致
type fakeChatCompletionClient struct {
	responses []openai.ChatCompletionResponse
	requests  []openai.ChatCompletionRequest
	err       error
}

func (f *fakeChatCompletionClient) Chat(_ context.Context, req openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error) {
	f.requests = append(f.requests, req)
	if f.err != nil {
		return openai.ChatCompletionResponse{}, f.err
	}
	if len(f.responses) == 0 {
		return openai.ChatCompletionResponse{}, fmt.Errorf("unexpected call")
	}
	resp := f.responses[0]
	f.responses = f.responses[1:]
	return resp, nil
}

func (f *fakeChatCompletionClient) ChatStream(_ context.Context, _ openai.ChatCompletionRequest) (*openai.ChatCompletionStream, error) {
	// 测试时传 handler=nil，走非流式路径，不调用此方法
	return nil, fmt.Errorf("streaming not supported in fake")
}

func TestBaseAgent_RunExecutesToolAndContinues(t *testing.T) {
	const toolName = "test_echo_base"
	tools.Executors[toolName] = func(args interface{}) (interface{}, error) {
		return "tool-output", nil
	}
	defer delete(tools.Executors, toolName)

	fakeClient := &fakeChatCompletionClient{
		responses: []openai.ChatCompletionResponse{
			{Choices: []openai.ChatCompletionChoice{{
				Message: openai.ChatCompletionMessage{
					Role: openai.ChatMessageRoleAssistant,
					ToolCalls: []openai.ToolCall{{
						ID: "call-1", Type: openai.ToolTypeFunction,
						Function: openai.FunctionCall{Name: toolName, Arguments: `{}`},
					}},
				},
			}}},
			{Choices: []openai.ChatCompletionChoice{{
				Message: openai.ChatCompletionMessage{Role: openai.ChatMessageRoleAssistant, Content: "完成"},
				FinishReason: openai.FinishReasonStop,
			}}},
		},
	}

	agent := NewBaseAgent(fakeClient, "test-model", nil)
	messages := []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: "你是助手"},
		{Role: openai.ChatMessageRoleUser, Content: "执行工具"},
	}

	// 传 handler=nil，走非流式路径（provider.Chat），便于测试
	reply, newMsgs, err := agent.Run(context.Background(), messages, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reply != "完成" {
		t.Errorf("expected reply '完成', got %q", reply)
	}
	// newMsgs 应包含：assistant(toolcall) + tool(result) + assistant(完成)
	if len(newMsgs) != 3 {
		t.Errorf("expected 3 new messages, got %d", len(newMsgs))
	}
}
```

- [ ] **Step 2.3: 运行测试确认失败**

```bash
go test ./pkg/agent/... -run TestBaseAgent -v
```
预期：`undefined: NewBaseAgent`

- [ ] **Step 2.4: 提取 engine.go 中的辅助方法**

在创建 `base.go` 之前，先确认 `engine.go` 中的辅助函数名称：

```bash
grep -n "^func " pkg/agent/engine.go
```

将以下私有方法从 engine.go **剪切**到新文件 `pkg/agent/helpers.go`（保持 `package agent`，逻辑不变）：
- `consumeStream` 或等价的流式解析函数（可能内联在 `RunTurnStream` 中，需要提取）
- `executeToolCallsConcurrently`（含 `executeToolCall`）
- `displayToolCallsStart`、`displayToolCallsResults`
- `truncateForLog`、`newDebugLogger`
- `toolCallResult` 结构体

> **重要：** 如果 engine.go 中没有独立的 `consumeStream` 函数（流式解析逻辑内联在 `RunTurnStream` 中），则在 `helpers.go` 中新建此函数，将 for 循环内容提取出来，签名为：
> ```go
> func (a *BaseAgent) consumeStream(ctx context.Context, stream *openai.ChatCompletionStream, handler StreamChunkHandler) (reply string, toolCalls []openai.ToolCall, finishReason openai.FinishReason, err error)
> ```

- [ ] **Step 2.5: 创建 base.go（从 engine.go 重构）**

新建 `pkg/agent/base.go`，核心变化：
1. 将 `ClawEngine` 改名为 `BaseAgent`
2. 移除 `messages []openai.ChatCompletionMessage` 字段（消息历史由外部管理）
3. `Run()` 签名改为 `Run(ctx, messages, handler) (reply, newMessages, error)`
4. **当 `handler == nil` 时使用非流式路径**（`provider.Chat()`，便于测试）；否则使用 `provider.ChatStream()`
5. 内部 ReAct 循环从传入的 messages 开始，将新增消息收集到 `newMessages` 切片返回
6. 保留 `maxTurns`、`maxConcurrency`、`debugLogger`、`verbose` 字段
7. 保留 `executeToolCallsConcurrently` 逻辑不变

```go
// pkg/agent/base.go
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"mini-code/pkg/provider"
	"mini-code/pkg/tools"
	"mini-code/pkg/ui"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/sashabaranov/go-openai"
)

const DefaultMaxTurns = 50

type BaseAgent struct {
	provider       provider.Provider
	model          string
	allowedTools   []string  // nil 表示允许全部工具
	debugLogger    *log.Logger
	maxConcurrency int
	verbose        bool
	maxTurns       int
}

func NewBaseAgent(p provider.Provider, model string, allowedTools []string) *BaseAgent {
	verbose := os.Getenv("LM_VERBOSE") != ""
	maxTurns := DefaultMaxTurns
	if v := os.Getenv("LM_MAX_TURNS"); v != "" {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil && n >= 0 {
			maxTurns = n
		}
	}
	return &BaseAgent{
		provider:       p,
		model:          model,
		allowedTools:   allowedTools,
		debugLogger:    newDebugLogger(),
		maxConcurrency: 5,
		verbose:        verbose,
		maxTurns:       maxTurns,
	}
}

func (a *BaseAgent) Name() string { return "base" }

func (a *BaseAgent) AllowedTools() []string { return a.allowedTools }

// filteredDefinitions 返回过滤后的工具列表
func (a *BaseAgent) filteredDefinitions() []openai.Tool {
	if len(a.allowedTools) == 0 {
		return tools.Definitions
	}
	allowed := make(map[string]bool, len(a.allowedTools))
	for _, t := range a.allowedTools {
		allowed[t] = true
	}
	var filtered []openai.Tool
	for _, def := range tools.Definitions {
		if allowed[def.Function.Name] {
			filtered = append(filtered, def)
		}
	}
	return filtered
}

// Run 执行完整的 ReAct 循环
// messages: 由 Orchestrator 传入的完整历史（含 system prompt + 注入记忆 + 对话历史）
// handler == nil 时使用非流式路径（provider.Chat），便于单元测试
// 返回最终回复内容，以及本次执行新增的消息列表（供 Orchestrator 追加到 Session）
func (a *BaseAgent) Run(ctx context.Context, messages []openai.ChatCompletionMessage, handler StreamChunkHandler) (string, []openai.ChatCompletionMessage, error) {
	var newMessages []openai.ChatCompletionMessage

	for i := 0; ; i++ {
		if a.maxTurns > 0 && i >= a.maxTurns {
			return "", newMessages, fmt.Errorf("达到最大工具调用轮数 (%d)", a.maxTurns)
		}

		allMessages := append(messages, newMessages...)

		req := openai.ChatCompletionRequest{
			Model:    a.model,
			Messages: allMessages,
			Tools:    a.filteredDefinitions(),
		}

		var reply string
		var toolCalls []openai.ToolCall
		var finishReason openai.FinishReason
		var err error

		if handler == nil {
			// 非流式路径（测试用）
			var resp openai.ChatCompletionResponse
			resp, err = a.provider.Chat(ctx, req)
			if err != nil {
				return "", newMessages, fmt.Errorf("chat error: %w", err)
			}
			if len(resp.Choices) > 0 {
				reply = resp.Choices[0].Message.Content
				toolCalls = resp.Choices[0].Message.ToolCalls
				finishReason = resp.Choices[0].FinishReason
			}
		} else {
			// 流式路径（生产用）
			var stream *openai.ChatCompletionStream
			stream, err = a.provider.ChatStream(ctx, req)
			if err != nil {
				return "", newMessages, fmt.Errorf("stream error: %w", err)
			}
			reply, toolCalls, finishReason, err = a.consumeStream(ctx, stream, handler)
			if err != nil {
				return "", newMessages, err
			}
		}

		assistantMsg := openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleAssistant,
			Content: reply,
		}
		if len(toolCalls) > 0 {
			assistantMsg.ToolCalls = toolCalls
		}
		newMessages = append(newMessages, assistantMsg)

		if len(toolCalls) > 0 {
			fmt.Println()
			a.displayToolCallsStart(toolCalls)
			results := a.executeToolCallsConcurrently(ctx, toolCalls)
			a.displayToolCallsResults(results)
			ui.PrintDim("  等待响应...")

			for _, result := range results {
				newMessages = append(newMessages, openai.ChatCompletionMessage{
					Role:       openai.ChatMessageRoleTool,
					Content:    result.resultStr,
					ToolCallID: result.toolCallID,
				})
			}
			continue
		}

		switch finishReason {
		case openai.FinishReasonStop, "":
			return reply, newMessages, nil
		case openai.FinishReasonLength:
			return reply, newMessages, fmt.Errorf("响应因达到 token 上限而被截断")
		default:
			return reply, newMessages, nil
		}
	}
}
```

> 注：`consumeStream`、`executeToolCallsConcurrently`、`displayToolCallsStart`、`displayToolCallsResults`、`truncateForLog`、`newDebugLogger` 从 engine.go 迁移过来，逻辑不变。

- [ ] **Step 2.5: 保留 engine.go 兼容层（临时，Phase 1 结束删除）**

在 `pkg/agent/engine.go` 中替换全部内容为：
```go
package agent

import (
	"context"
	"mini-code/pkg/provider"
	"github.com/sashabaranov/go-openai"
)

// legacyClientAdapter 将旧的 chatCompletionClient 接口包装为 provider.Provider
// 仅用于保证现有 engine_test.go 可编译，Phase 1 结束后删除
type legacyClientAdapter struct {
	client interface {
		CreateChatCompletion(context.Context, openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error)
		CreateChatCompletionStream(context.Context, openai.ChatCompletionRequest) (*openai.ChatCompletionStream, error)
	}
}

func (a *legacyClientAdapter) Chat(ctx context.Context, req openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error) {
	return a.client.CreateChatCompletion(ctx, req)
}

func (a *legacyClientAdapter) ChatStream(ctx context.Context, req openai.ChatCompletionRequest) (*openai.ChatCompletionStream, error) {
	return a.client.CreateChatCompletionStream(ctx, req)
}

// ClawEngine 兼容层，将在 Phase 1 完成后删除
type ClawEngine = BaseAgent

func NewClawEngine(client interface {
	CreateChatCompletion(context.Context, openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error)
	CreateChatCompletionStream(context.Context, openai.ChatCompletionRequest) (*openai.ChatCompletionStream, error)
}, model string) *ClawEngine {
	p := &legacyClientAdapter{client: client}
	return NewBaseAgent(p, model, nil)
}

// 确保 legacyClientAdapter 实现 provider.Provider 接口（编译期检查）
var _ provider.Provider = (*legacyClientAdapter)(nil)
```

- [ ] **Step 2.6: 运行全部测试**

```bash
go test ./... -v 2>&1 | tail -30
```
预期：全部 PASS（包括旧的 engine_test.go）

- [ ] **Step 2.7: Commit**

```bash
git add pkg/agent/
git commit -m "feat(agent): 重构 ClawEngine 为 BaseAgent，消息历史上移至调用方"
```

---

## Task 3: Channel 接口 + CLI Channel

**Files:**
- Create: `pkg/channel/types.go`
- Create: `pkg/channel/cli/runner.go`
- Create: `pkg/channel/cli/runner_test.go`

- [ ] **Step 3.1: 写 Channel 接口测试（编译期检查）**

```go
// pkg/channel/cli/runner_test.go
package cli_test

import (
	"mini-code/pkg/channel"
	"mini-code/pkg/channel/cli"
	"testing"
)

// 编译期确认 CLIChannel 实现了 Channel 接口
var _ channel.Channel = (*cli.CLIChannel)(nil)

func TestCLIChannel_ChannelID(t *testing.T) {
	ch := cli.New(nil) // nil readline config，仅测试 ID
	if ch.ChannelID() != "cli" {
		t.Errorf("expected ChannelID 'cli', got %q", ch.ChannelID())
	}
}
```

- [ ] **Step 3.2: 运行测试确认失败**

```bash
go test ./pkg/channel/... 2>&1
```
预期：`cannot find package`

- [ ] **Step 3.3: 创建 Channel 接口**

```go
// pkg/channel/types.go
package channel

import "context"

// Channel 统一输入输出接口，CLI 和 Telegram 均实现此接口
type Channel interface {
	// Start 启动消息监听循环（阻塞）
	Start(ctx context.Context) error
	// Messages 返回接收消息的只读通道
	Messages() <-chan IncomingMessage
	// Send 发送文本响应
	Send(msg OutgoingMessage) error
	// SendFile 发送文件附件
	SendFile(chatID string, path string) error
	// EditMessage 更新已发送的消息（CLI 为 no-op，Telegram 用于流式刷新）
	EditMessage(msgID string, text string) error
	// NotifyDone 任务完成通知（Telegram 发新消息触发推送）
	NotifyDone(chatID string, text string) error
	// ChannelID 返回 Channel 标识符
	ChannelID() string
}

type IncomingMessage struct {
	ChannelID string   // "cli" 或 Telegram chat_id
	UserID    string
	Text      string
	Files     []string // 本地临时文件路径（附件已预下载）
	ReplyTo   string
}

type OutgoingMessage struct {
	ChatID    string
	Text      string
	ReplyToID string
	MessageID string // 非空时为更新已有消息
}
```

- [ ] **Step 3.4: 创建 CLI Channel**

```go
// pkg/channel/cli/runner.go
package cli

import (
	"context"
	"fmt"
	"mini-code/pkg/channel"
	"mini-code/pkg/ui"

	"github.com/ergochat/readline"
)

type CLIChannel struct {
	rl       *readline.Instance
	messages chan channel.IncomingMessage
}

func New(rl *readline.Instance) *CLIChannel {
	return &CLIChannel{
		rl:       rl,
		messages: make(chan channel.IncomingMessage, 1),
	}
}

func (c *CLIChannel) ChannelID() string { return "cli" }

func (c *CLIChannel) Messages() <-chan channel.IncomingMessage { return c.messages }

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

func (c *CLIChannel) Send(msg channel.OutgoingMessage) error {
	// 流式输出由 Orchestrator 的 handler 直接调用 ui，此处处理非流式响应
	fmt.Print(msg.Text)
	return nil
}

func (c *CLIChannel) SendFile(_ string, path string) error {
	ui.PrintInfo("文件: %s", path)
	return nil
}

func (c *CLIChannel) EditMessage(_ string, _ string) error { return nil } // CLI no-op

func (c *CLIChannel) NotifyDone(_ string, text string) error {
	ui.PrintSuccess(text)
	return nil
}
```

- [ ] **Step 3.5: 运行测试确认通过**

```bash
go test ./pkg/channel/... -v
```
预期：`PASS`

- [ ] **Step 3.6: Commit**

```bash
git add pkg/channel/
git commit -m "feat(channel): 添加 Channel 接口和 CLI Channel 实现"
```

---

## Task 4: Orchestrator + Session

**Files:**
- Create: `pkg/orchestrator/session.go`
- Create: `pkg/orchestrator/orchestrator.go`
- Create: `pkg/orchestrator/orchestrator_test.go`

- [ ] **Step 4.1: 写 Orchestrator 测试**

```go
// pkg/orchestrator/orchestrator_test.go
package orchestrator_test

import (
	"context"
	"strings"
	"testing"
	"mini-code/pkg/channel"
	"mini-code/pkg/orchestrator"
)

type fakeChannel struct {
	msgs     chan channel.IncomingMessage
	sent     []string
}

func (f *fakeChannel) ChannelID() string { return "test" }
func (f *fakeChannel) Messages() <-chan channel.IncomingMessage { return f.msgs }
func (f *fakeChannel) Send(msg channel.OutgoingMessage) error {
	f.sent = append(f.sent, msg.Text)
	return nil
}
func (f *fakeChannel) SendFile(_, _ string) error        { return nil }
func (f *fakeChannel) EditMessage(_, _ string) error     { return nil }
func (f *fakeChannel) NotifyDone(_, text string) error   { f.sent = append(f.sent, text); return nil }

func TestOrchestrator_CreatesSessionPerUser(t *testing.T) {
	orch := orchestrator.New(nil) // nil memory store (Phase 1 无记忆)
	s1 := orch.GetOrCreateSession("cli", "user1")
	s2 := orch.GetOrCreateSession("cli", "user1")
	s3 := orch.GetOrCreateSession("cli", "user2")

	if s1 != s2 {
		t.Error("same user should get same session")
	}
	if s1 == s3 {
		t.Error("different users should get different sessions")
	}
}

func TestSession_ResetClearsMessages(t *testing.T) {
	orch := orchestrator.New(nil)
	s := orch.GetOrCreateSession("cli", "user1")
	s.AppendUserMessage("hello")
	s.AppendUserMessage("world")

	if len(s.Messages()) < 2 {
		t.Fatalf("expected messages, got %d", len(s.Messages()))
	}

	s.Reset()
	// Reset 后应只保留 system 消息
	for _, msg := range s.Messages() {
		if msg.Role != "system" {
			t.Errorf("after reset, expected only system messages, found role=%q", msg.Role)
		}
	}
}
```

- [ ] **Step 4.2: 运行测试确认失败**

```bash
go test ./pkg/orchestrator/... 2>&1
```
预期：`cannot find package`

- [ ] **Step 4.3: 创建 Session**

```go
// pkg/orchestrator/session.go
package orchestrator

import (
	"context"
	"sync"
	"time"

	"github.com/sashabaranov/go-openai"
)

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
	currentTurn    int
	lastToolName   string
}

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

func (s *Session) Messages() []openai.ChatCompletionMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]openai.ChatCompletionMessage, len(s.messages))
	copy(cp, s.messages)
	return cp
}

func (s *Session) AppendUserMessage(text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleUser,
		Content: text,
	})
	s.lastSeen = time.Now()
}

func (s *Session) AppendMessages(msgs []openai.ChatCompletionMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, msgs...)
}

func (s *Session) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	// 保留 system 消息
	system := s.messages[0]
	s.messages = []openai.ChatCompletionMessage{system}
}

func (s *Session) SetCancel(cancel context.CancelFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancel = cancel
}

func (s *Session) Cancel() {
	s.mu.Lock()
	f := s.cancel
	s.mu.Unlock()
	if f != nil {
		f()
	}
}

func (s *Session) StatusInfo() (agentType string, turn int, lastTool string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.agentType, s.currentTurn, s.lastToolName
}

func (s *Session) MessageCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.messages)
}
```

- [ ] **Step 4.4: 创建 Orchestrator**

```go
// pkg/orchestrator/orchestrator.go
package orchestrator

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"time"
	"mini-code/pkg/agent"
	"mini-code/pkg/channel"
	"mini-code/pkg/ui"
)

// Orchestrator 管理 Session，路由消息到 Agent
type Orchestrator struct {
	sessions   map[string]*Session
	mu         sync.Mutex
	memStore   interface{} // Phase 2 填充为 *memory.Store
}

func New(memStore interface{}) *Orchestrator {
	o := &Orchestrator{
		sessions: make(map[string]*Session),
		memStore: memStore,
	}
	go o.evictLoop() // 后台清理不活跃 Session
	return o
}

func (o *Orchestrator) GetOrCreateSession(channelID, userID string) *Session {
	key := channelID + ":" + userID
	o.mu.Lock()
	defer o.mu.Unlock()
	if s, ok := o.sessions[key]; ok {
		return s
	}
	s := newSession(channelID, userID, buildSystemPrompt())
	o.sessions[key] = s
	return s
}

// Handle 处理一条到来的消息
func (o *Orchestrator) Handle(ctx context.Context, msg channel.IncomingMessage, agnt agent.Agent, ch channel.Channel) error {
	session := o.GetOrCreateSession(msg.ChannelID, msg.UserID)

	// 追加用户消息到 Session
	session.AppendUserMessage(msg.Text)

	// 创建可取消 context，存入 Session 供 /cancel 使用
	runCtx, cancel := context.WithCancel(ctx)
	session.SetCancel(cancel)
	defer cancel()

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
	reply, newMsgs, err := agnt.Run(runCtx, session.Messages(), handler)
	streaming.Complete()

	// 将新消息追加到 Session（无论是否出错，保留已完成的工具调用）
	if len(newMsgs) > 0 {
		session.AppendMessages(newMsgs)
	}

	if err != nil {
		if runCtx.Err() != nil {
			ch.NotifyDone(msg.ChannelID, "任务已取消")
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

func buildSystemPrompt() string {
	// 从原 pkg/agent/engine.go 的 buildSystemPrompt() 完整迁移（内容不变）
	osName := runtime.GOOS
	var shellHint string
	switch osName {
	case "windows":
		shellHint = "如果必须执行 shell 命令，请使用 Windows CMD 语法。"
	default:
		shellHint = "如果必须执行 shell 命令，请使用 Unix shell 语法。"
	}

	return fmt.Sprintf(`你是一个专业的中文 AI 编程助手 Mini-Code。你的核心职责是帮助用户完成各类软件开发任务。

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
}

// buildBaseSystemPrompt 供 Phase 2 的 buildSystemPromptWithMemory 调用
func buildBaseSystemPrompt() string {
	return buildSystemPrompt()
}
```

- [ ] **Step 4.5: 运行测试**

```bash
go test ./pkg/orchestrator/... -v
```
预期：`PASS`

- [ ] **Step 4.6: Commit**

```bash
git add pkg/orchestrator/
git commit -m "feat(orchestrator): 添加 Session 管理和 Orchestrator 骨架"
```

---

## Task 5: 更新 main.go + 全面验证

**Files:**
- Modify: `cmd/agent/main.go`

- [ ] **Step 5.1: 更新 main.go**

将 `main.go` 改为通过 CLIChannel + Orchestrator + CoderAgent 启动：

```go
// cmd/agent/main.go
package main

import (
	"context"
	"fmt"
	"mini-code/pkg/agent"
	"mini-code/pkg/channel/cli"
	"mini-code/pkg/orchestrator"
	"mini-code/pkg/provider"
	"mini-code/pkg/ui"
	_ "mini-code/pkg/tools"
	"os"
	"os/signal"
	"syscall"

	"github.com/ergochat/readline"
	"github.com/sashabaranov/go-openai"
)

func main() {
	loadDotEnv(".env")

	apiKey := os.Getenv("API_KEY")
	baseURL := os.Getenv("BASE_URL")
	modelID := os.Getenv("MODEL")
	if apiKey == "" || baseURL == "" || modelID == "" {
		ui.PrintError("缺少必要环境变量 API_KEY / BASE_URL / MODEL")
		os.Exit(1)
	}

	cfg := openai.DefaultConfig(apiKey)
	cfg.BaseURL = baseURL
	p := provider.NewOpenAIProvider(cfg, modelID)

	rl, err := readline.NewEx(&readline.Config{
		Prompt:       ui.SprintColor(ui.User, "➜ "),
		HistoryFile:  ".mini-code-history",
		HistoryLimit: 1000,
		AutoComplete: getCompleter(),
	})
	if err != nil {
		ui.PrintError("初始化输入失败: %v", err)
		os.Exit(1)
	}
	defer rl.Close()

	cliCh := cli.New(rl)
	orch := orchestrator.New(nil) // Phase 2 传入 memory store
	baseAgent := agent.NewBaseAgent(p, modelID, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGINT)
	go func() {
		<-sigChan
		fmt.Println()
		ui.PrintInfo("收到中断信号，正在退出...")
		cancel()
		os.Exit(0)
	}()

	printWelcome()

	// 在 goroutine 中启动 CLI 监听
	go cliCh.Start(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-cliCh.Messages():
			if !ok {
				ui.PrintInfo("再见！感谢使用 Mini-Code。")
				return
			}

			// 处理内置命令
			if handleBuiltinCommand(msg.Text, orch, cliCh.ChannelID(), "local") {
				continue
			}

			if err := orch.Handle(ctx, msg, baseAgent, cliCh); err != nil {
				ui.PrintError("运行失败: %v", err)
			}
			fmt.Println()
		}
	}
}
```

> 注：`handleBuiltinCommand` 签名改为接受 `orch *orchestrator.Orchestrator, channelID, userID string`，通过 `orch.GetOrCreateSession(channelID, userID).Reset()` 实现 reset。此函数在 `cmd/agent/main.go` 中定义，处理 `reset`/`history`/`help`/`status` 等内置命令，返回 `bool`（是否已处理）。
> **Phase 3 备注：** `Handle()` 在 Phase 3 中会将 `agnt agent.Agent` 参数替换为 `factory AgentFactory`，届时修改此处调用即可。

- [ ] **Step 5.2: 运行全部测试**

```bash
go test ./... -count=1
```
预期：`ok` 所有包，无 FAIL

- [ ] **Step 5.3: 编译验证**

```bash
go build -o mini-code-test.exe ./cmd/agent && echo "BUILD OK"
```
预期：`BUILD OK`

- [ ] **Step 5.4: 删除临时兼容层**

如果 `engine_test.go` 中已无对 `NewClawEngine` 的引用，删除 `engine.go` 中的兼容层。

```bash
go test ./pkg/agent/... -v
```
预期：PASS

- [ ] **Step 5.5: 清理临时构建产物**

```bash
rm -f mini-code-test.exe
```

- [ ] **Step 5.6: 最终 Commit**

```bash
git add cmd/agent/main.go pkg/agent/engine.go
git commit -m "feat(phase1): 完成基础重构，CLI 通过 Orchestrator 运行"
```

---

## Phase 1 验收标准

运行以下命令，全部通过即为完成：

```bash
# 1. 全部测试通过
go test ./... -count=1
# 预期：全部 ok，无 FAIL

# 2. 编译成功
go build ./cmd/agent
# 预期：无报错

# 3. 代码检查
go vet ./...
# 预期：无警告
```
