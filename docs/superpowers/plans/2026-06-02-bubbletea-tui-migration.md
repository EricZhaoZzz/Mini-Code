# CLI 终端 UI 迁移到 Bubble Tea 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 CLI（`cmd/agent`）的输入/中断/输出从「readline + raw-mode Esc 轮询 + 直接 fmt.Print」三套并存模型，迁移到单一 Bubble Tea 事件循环（内联模式），并让 agent 层改为发结构化事件而非直接打印。

**Architecture:** 在 `pkg/agent` 新增 `EventSink` 接口取代 `Agent.Run` 的 `StreamChunkHandler` 入参；agent 运行期间只产出事件。CLI 侧由一个 Bubble Tea `Model` 拥有整个会话循环，agent 在 `tea.Cmd` 派生的 goroutine 中运行，事件经 `program.Send` 回到 `Update` 渲染并用 `tea.Println` 提交进滚动历史。Telegram 不变。

**Tech Stack:** Go 1.26；`github.com/charmbracelet/bubbletea`（v1 API 线）、`bubbles/textarea`、`bubbles/spinner`、`lipgloss`、`glamour`（内置 `dark` 风格）。测试用 `charmbracelet/x/exp/teatest` + fake sink。

**Spec:** `docs/superpowers/specs/2026-06-02-bubbletea-tui-migration-design.md`

---

## 锁定的设计决策（实现前必读）

1. **只改 `Agent` 接口与 `BaseAgent.Run`**：把 handler 入参从 `StreamChunkHandler`（func）换成 `EventSink`（接口）。
2. **保留** `StreamChunkHandler` 类型、`consumeStream`、`displayToolCallsStart/Results`、整个 `engine.go`（`ClawEngine = BaseAgent` 别名）原样不动——它们只服务 legacy 测试，不在 live 路径。`BaseAgent.Run` 内部用一个文本适配器把 `EventSink.OnText` 喂给 `consumeStream`。
3. **删除** `orchestrator.StreamHandler` 死接口（无人实现/调用）。
4. **CLI sink**（`uiSink`）放在 `pkg/channel/cli`（依赖 bubbletea+agent）；`CLIChannel` 暴露 `NewEventSink() agent.EventSink`。**Telegram sink**（`telegramSink`）放在 `pkg/orchestrator`（只依赖 `channel` + `textutil`，无 bubbletea），保留现有 1.5s 节流逻辑。
5. **Bubble Tea v1 API 线**：使用 `tea.Println`、`tea.Cmd`、`tea.Msg`、`tea.Batch`。`tea.NewProgram(model)`，内联模式（不调用 `tea.WithAltScreen()`）。
6. **program 指针传递**用 holder 指针（`*programHolder`）规避 Model 值拷贝问题。
7. **glamour**：流式期间活动区显示纯文本；`OnComplete` 时整段 glamour 渲染 → `tea.Println` 进滚动历史。

## 文件结构

**新建：**
- `pkg/agent/events.go` — `EventSink` 接口、`NopSink`、`ToolCallInfo`、`ToolResultInfo`、`ensureSink`、转换器。
- `pkg/agent/events_test.go` — fake sink 断言 `BaseAgent.Run` 正确发事件。
- `pkg/ui/styles.go` — lipgloss 样式（颜色/图标），取代运行期颜色打印的来源。
- `pkg/ui/guard.go` — 运行期 `programActive` 守卫 + 测试。
- `pkg/ui/guard_test.go`
- `pkg/channel/cli/model.go` — Bubble Tea `Model`（textarea、spinner、历史、双击 Esc、内置命令、glamour 渲染）。
- `pkg/channel/cli/model_test.go` — `teatest` + 纯 Update 逻辑测试。
- `pkg/channel/cli/sink.go` — `uiSink`（实现 `agent.EventSink`，`program.Send` 各类 tea.Msg）。
- `pkg/channel/cli/messages.go` — tea.Msg 类型定义（`chunkMsg` 等）。
- `pkg/orchestrator/sink_telegram.go` — `telegramSink`（实现 `agent.EventSink`，保留节流）。

**修改：**
- `pkg/agent/agent.go` — `Agent.Run` 签名改用 `EventSink`。
- `pkg/agent/base.go` — `Run` 改签名 + 发事件，移除 `ui` 依赖。
- `pkg/orchestrator/orchestrator.go` — 删除 `StreamHandler` 接口；`handleCLI`/`handleTelegram` 改用 sink。
- `pkg/orchestrator/orchestrator_test.go` — `fakeAgent.Run` 改签名。
- `pkg/channel/cli/runner.go` — `CLIChannel` 持有 `*programHolder`，`Send/EditMessage/...` 改走 `program.Send`，新增 `NewEventSink`、`Run`。
- `cmd/agent/main.go` — 用 Bubble Tea program 取代 select 循环；调整重启交接时序。
- `pkg/ui/colors.go` — `Print*` 加运行期守卫降级。

**删除：**
- `pkg/ui/esc_monitor.go`、`esc_monitor_unix.go`、`esc_monitor_windows.go`、`esc_monitor_test.go`
- `pkg/ui/markdown.go`、`markdown_test.go`、`highlight.go`、`highlight_test.go`、`theme.go`、`theme_test.go`
- `pkg/ui/input.go` 中 readline 相关（保留 `ReadLineSimple`/`Confirm`/`Select` 若仍被引用，否则整文件删）
- `pkg/ui/progress.go` 中 `Spinner` 部分（若 `ProgressBar`/`MultiProgress` 无调用方则整文件删）

---

## Phase 1：Agent EventSink 基础

### Task 1：定义 EventSink 与值类型

**Files:**
- Create: `pkg/agent/events.go`

- [ ] **Step 1：写事件类型与接口**

```go
package agent

import "github.com/sashabaranov/go-openai"

// ToolCallInfo 描述一次工具调用的展示信息（不含 ui 依赖）。
type ToolCallInfo struct {
	Name        string
	ArgsSummary string
}

// ToolResultInfo 描述一次工具调用的结果。
type ToolResultInfo struct {
	Name    string
	OK      bool
	Summary string
	Err     error
}

// EventSink 接收 Agent 运行期间的结构化事件。
// 调用方通过 ensureSink 包装，保证 nil 安全。
type EventSink interface {
	OnText(chunk string)            // 流式文本增量
	OnWaiting()                     // 工具执行完毕、等待模型继续
	OnToolStart(calls []ToolCallInfo)
	OnToolDone(results []ToolResultInfo)
	OnComplete(fullText string)     // 本轮最终回复（用于整段渲染）
}

// NopSink 是 EventSink 的空实现。
type NopSink struct{}

func (NopSink) OnText(string)                {}
func (NopSink) OnWaiting()                   {}
func (NopSink) OnToolStart([]ToolCallInfo)   {}
func (NopSink) OnToolDone([]ToolResultInfo)  {}
func (NopSink) OnComplete(string)            {}

func ensureSink(s EventSink) EventSink {
	if s == nil {
		return NopSink{}
	}
	return s
}

// buildToolCallInfos 从 openai tool calls 提取展示信息。
func buildToolCallInfos(toolCalls []openai.ToolCall) []ToolCallInfo {
	infos := make([]ToolCallInfo, 0, len(toolCalls))
	for _, tc := range toolCalls {
		infos = append(infos, ToolCallInfo{
			Name:        tc.Function.Name,
			ArgsSummary: summarizeToolArgs(tc.Function.Arguments),
		})
	}
	return infos
}

// buildToolResultInfos 从内部 toolCallResult 提取结果信息。
func buildToolResultInfos(results []toolCallResult) []ToolResultInfo {
	infos := make([]ToolResultInfo, 0, len(results))
	for _, r := range results {
		info := ToolResultInfo{Name: r.toolName, OK: r.err == nil, Err: r.err}
		if r.err != nil {
			info.Summary = r.err.Error()
		} else {
			info.Summary = r.resultStr
		}
		infos = append(infos, info)
	}
	return infos
}
```

- [ ] **Step 2：加 `summarizeToolArgs`（复用 helpers.go 的参数摘要逻辑）**

在 `pkg/agent/events.go` 追加：

```go
import "encoding/json"

// summarizeToolArgs 从 JSON 参数里取一个最有信息量的字段做摘要。
func summarizeToolArgs(argsJSON string) string {
	var args map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return ""
	}
	if path, ok := args["path"].(string); ok {
		return path
	}
	if query, ok := args["query"].(string); ok {
		return truncateForLog(query, 30)
	}
	if cmd, ok := args["command"].(string); ok {
		return truncateForLog(cmd, 30)
	}
	return ""
}
```

> 注：`truncateForLog` 已存在于 `pkg/agent/helpers.go`，可直接复用；把上面两个 `import` 合并进 Step 1 的 import 块（`encoding/json` 与 `go-openai`）。

- [ ] **Step 3：编译通过**

Run: `go build ./pkg/agent/`
Expected: 编译成功（暂未被调用，`ensureSink`/`build*` 会触发 "declared and not used"？不会——它们是包级函数，Go 不报未用函数。通过。）

- [ ] **Step 4：Commit**

```bash
git add pkg/agent/events.go
git commit -m "feat(agent): add EventSink interface and event value types"
```

### Task 2：改 `Agent` 接口签名为 EventSink

**Files:**
- Modify: `pkg/agent/agent.go`

- [ ] **Step 1：改接口**

把 `pkg/agent/agent.go` 的 `Agent` 接口改为：

```go
// Agent 接口：接受完整消息历史，返回最终响应内容。
// 运行期事件通过 EventSink 上报（nil 表示静默）。
type Agent interface {
	Run(ctx context.Context, messages []openai.ChatCompletionMessage, sink EventSink) (reply string, newMessages []openai.ChatCompletionMessage, err error)
	Name() string
	AllowedTools() []string
}
```

**保留** 文件顶部的 `StreamChunkHandler` 类型定义不动（engine.go 仍用）。

- [ ] **Step 2：编译（预期失败，提示 base.go/orchestrator/test 不匹配）**

Run: `go build ./... 2>&1 | head -20`
Expected: FAIL，多个 "cannot use ... as EventSink" / 签名不匹配。这是预期，下个 Task 修。

- [ ] **Step 3：Commit（接口先行）**

```bash
git add pkg/agent/agent.go
git commit -m "refactor(agent): change Agent.Run handler param to EventSink"
```

### Task 3：`BaseAgent.Run` 改为发事件

**Files:**
- Modify: `pkg/agent/base.go:75-164`
- Test: `pkg/agent/events_test.go`

- [ ] **Step 1：先写失败测试（fake sink 断言事件序列）**

Create `pkg/agent/events_test.go`：

```go
package agent

import (
	"context"
	"testing"

	"github.com/sashabaranov/go-openai"
)

// recordingSink 记录收到的事件，用于断言。
type recordingSink struct {
	texts     []string
	toolStart [][]ToolCallInfo
	toolDone  [][]ToolResultInfo
	waiting   int
	completes []string
}

func (s *recordingSink) OnText(c string)                { s.texts = append(s.texts, c) }
func (s *recordingSink) OnWaiting()                     { s.waiting++ }
func (s *recordingSink) OnToolStart(c []ToolCallInfo)   { s.toolStart = append(s.toolStart, c) }
func (s *recordingSink) OnToolDone(r []ToolResultInfo)  { s.toolDone = append(s.toolDone, r) }
func (s *recordingSink) OnComplete(full string)         { s.completes = append(s.completes, full) }

// stubProvider 返回一次不带工具调用的非流式响应。
type stubProvider struct{ content string }

func (p *stubProvider) Chat(_ context.Context, _ openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error) {
	return openai.ChatCompletionResponse{
		Choices: []openai.ChatCompletionChoice{{
			Message:      openai.ChatCompletionMessage{Role: "assistant", Content: p.content},
			FinishReason: openai.FinishReasonStop,
		}},
	}, nil
}
func (p *stubProvider) ChatStream(_ context.Context, _ openai.ChatCompletionRequest) (*openai.ChatCompletionStream, error) {
	return nil, nil
}

func TestRunEmitsCompleteOnFinalReply(t *testing.T) {
	a := NewBaseAgent(&stubProvider{content: "hello world"}, "test-model", nil)
	sink := &recordingSink{}
	msgs := []openai.ChatCompletionMessage{{Role: "user", Content: "hi"}}

	reply, _, err := a.Run(context.Background(), msgs, sink)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if reply != "hello world" {
		t.Fatalf("reply = %q, want %q", reply, "hello world")
	}
	if len(sink.completes) != 1 || sink.completes[0] != "hello world" {
		t.Fatalf("OnComplete = %v, want [hello world]", sink.completes)
	}
}

func TestRunNilSinkIsSafe(t *testing.T) {
	a := NewBaseAgent(&stubProvider{content: "ok"}, "test-model", nil)
	if _, _, err := a.Run(context.Background(), []openai.ChatCompletionMessage{{Role: "user", Content: "x"}}, nil); err != nil {
		t.Fatalf("nil sink should be safe, got %v", err)
	}
}
```

> 注：`provider.Provider` 接口需被 `stubProvider` 满足。先确认其方法集——若 `Chat`/`ChatStream` 签名与上面不一致，按 `pkg/provider` 实际签名调整 stub（不要改 provider）。

- [ ] **Step 2：运行测试，确认失败**

Run: `go test ./pkg/agent/ -run TestRun -v`
Expected: FAIL（编译失败：`base.go` 仍是旧签名/`sink.OnComplete` 未被调用）。

- [ ] **Step 3：改 `BaseAgent.Run`**

把 `pkg/agent/base.go` 的 `Run` 改为下面内容（签名换 `sink EventSink`，内部用文本适配器复用 `consumeStream`，工具/等待/完成改发事件，移除 `fmt.Println()` 与 `ui.PrintDim`）：

```go
func (a *BaseAgent) Run(ctx context.Context, messages []openai.ChatCompletionMessage, sink EventSink) (string, []openai.ChatCompletionMessage, error) {
	sink = ensureSink(sink)

	if a.systemPromptOverride != "" && len(messages) > 0 && messages[0].Role == "system" {
		overrideMsgs := make([]openai.ChatCompletionMessage, len(messages))
		copy(overrideMsgs, messages)
		overrideMsgs[0].Content = combineSystemPrompt(messages[0].Content, a.systemPromptOverride)
		messages = overrideMsgs
	}

	// 文本适配器：把流式增量转成 EventSink.OnText，复用既有 consumeStream。
	textHandler := StreamChunkHandler(func(content string, done bool) error {
		if !done && content != "" {
			sink.OnText(content)
		}
		return nil
	})

	var newMessages []openai.ChatCompletionMessage

	for i := 0; ; i++ {
		if a.maxTurns > 0 && i >= a.maxTurns {
			return "", newMessages, fmt.Errorf("达到最大工具调用轮数 (%d)", a.maxTurns)
		}

		allMessages := append(messages, newMessages...)
		req := openai.ChatCompletionRequest{Model: a.model, Messages: allMessages, Tools: a.filteredDefinitions()}

		var reply string
		var toolCalls []openai.ToolCall
		var finishReason openai.FinishReason
		var err error

		if a.provider == nil { // 防御
			return "", newMessages, fmt.Errorf("provider 未配置")
		}

		// 非流式：当 provider 的 ChatStream 不可用或测试时。流式：生产路径。
		stream, streamErr := a.provider.ChatStream(ctx, req)
		if streamErr != nil || stream == nil {
			var resp openai.ChatCompletionResponse
			resp, err = a.provider.Chat(ctx, req)
			if err != nil {
				return "", newMessages, fmt.Errorf("chat error: %w", err)
			}
			if len(resp.Choices) > 0 {
				reply = resp.Choices[0].Message.Content
				toolCalls = resp.Choices[0].Message.ToolCalls
				finishReason = resp.Choices[0].FinishReason
				if reply != "" {
					sink.OnText(reply)
				}
			}
		} else {
			reply, toolCalls, finishReason, err = a.consumeStream(ctx, stream, textHandler)
			if err != nil {
				return "", newMessages, err
			}
		}

		assistantMsg := openai.ChatCompletionMessage{Role: openai.ChatMessageRoleAssistant, Content: reply}
		if len(toolCalls) > 0 {
			assistantMsg.ToolCalls = toolCalls
		}
		newMessages = append(newMessages, assistantMsg)

		if len(toolCalls) > 0 {
			sink.OnToolStart(buildToolCallInfos(toolCalls))
			results := a.executeToolCallsConcurrently(ctx, toolCalls)
			sink.OnToolDone(buildToolResultInfos(results))
			sink.OnWaiting()

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
			sink.OnComplete(reply)
			return reply, newMessages, nil
		case openai.FinishReasonLength:
			return reply, newMessages, fmt.Errorf("响应因达到 token 上限而被截断")
		default:
			sink.OnComplete(reply)
			return reply, newMessages, nil
		}
	}
}
```

> 说明：原逻辑「`handler == nil` 走非流式」改为「`ChatStream` 失败/返回 nil 走非流式」，使 fake provider（`ChatStream` 返回 `nil,nil`）走非流式路径，便于测试且不改变生产行为（生产 provider 的 `ChatStream` 正常返回流）。

- [ ] **Step 4：移除 base.go 不再使用的 `ui` import**

`base.go` 现在不再调用 `ui.*`。删除 import 块里的 `"mini-code/pkg/ui"`。

Run: `go build ./pkg/agent/`
Expected: 成功。若报 `ui` imported and not used 已在本步解决；若报 `fmt` 未用则保留（仍在用）。

- [ ] **Step 5：运行测试，确认通过**

Run: `go test ./pkg/agent/ -run TestRun -v`
Expected: PASS（`TestRunEmitsCompleteOnFinalReply`、`TestRunNilSinkIsSafe`）。

- [ ] **Step 6：跑整个 agent 包测试，确认 legacy 未回归**

Run: `go test ./pkg/agent/`
Expected: PASS（`engine_test.go` 等 legacy 测试仍走 `displayToolCalls*` 打印路径，不受影响）。

- [ ] **Step 7：Commit**

```bash
git add pkg/agent/base.go pkg/agent/events.go pkg/agent/events_test.go
git commit -m "refactor(agent): BaseAgent.Run emits EventSink events instead of printing"
```

---

## Phase 2：Orchestrator 接入 sink

### Task 4：删除死接口 `StreamHandler`，新增 telegramSink

**Files:**
- Modify: `pkg/orchestrator/orchestrator.go:26-38`（删 `StreamHandler`）
- Create: `pkg/orchestrator/sink_telegram.go`

- [ ] **Step 1：删除 `orchestrator.StreamHandler` 接口块**

删除 `pkg/orchestrator/orchestrator.go` 第 26–38 行整段 `StreamHandler` 接口定义（无人实现/调用）。同时删除上方 `StreamMode` 相关常量若未被使用（`StreamModeCLI`/`StreamModeTelegram` 第 18–24 行——先 grep 确认无引用再删）。

Run: `grep -rn "StreamMode\|StreamHandler" pkg cmd | grep -v _test`
Expected: 删除后无输出（确认安全）。

- [ ] **Step 2：新增 telegramSink**

Create `pkg/orchestrator/sink_telegram.go`：

```go
package orchestrator

import (
	"fmt"
	"strings"
	"time"

	"mini-code/pkg/agent"
	"mini-code/pkg/channel"
	"mini-code/pkg/textutil"
)

// telegramSink 实现 agent.EventSink，保留 1.5s 节流 EditMessage 逻辑。
// 忽略工具事件（维持现状：Telegram 仅展示最终文本）。
type telegramSink struct {
	ch         channel.Channel
	chatID     string
	msgID      string
	buf        strings.Builder
	lastUpdate time.Time
}

func newTelegramSink(ch channel.Channel, chatID, msgID string) *telegramSink {
	return &telegramSink{ch: ch, chatID: chatID, msgID: msgID}
}

func (s *telegramSink) OnText(chunk string) {
	s.buf.WriteString(chunk)
	if time.Since(s.lastUpdate) > 1500*time.Millisecond && s.buf.Len() > 0 {
		text := textutil.TruncateWithEllipsis(s.buf.String(), 4000)
		if s.msgID != "" {
			s.ch.EditMessage(s.msgID, text)
		}
		s.lastUpdate = time.Now()
	}
}

func (s *telegramSink) OnWaiting()                          {}
func (s *telegramSink) OnToolStart([]agent.ToolCallInfo)    {}
func (s *telegramSink) OnToolDone([]agent.ToolResultInfo)   {}

func (s *telegramSink) OnComplete(full string) {
	content := s.buf.String()
	if content == "" {
		content = full
	}
	if content == "" {
		content = "✅ 任务完成"
	}
	if s.msgID != "" {
		s.ch.EditMessage(s.msgID, content)
	}
	_ = fmt.Sprint // 占位，保持 import（若不需要可删 fmt）
}
```

> 编译时如 `fmt` 未用就删掉 `fmt` import 与最后一行占位。

- [ ] **Step 3：编译**

Run: `go build ./pkg/orchestrator/ 2>&1 | head`
Expected: 可能报 `handleCLI`/`handleTelegram` 仍用旧 `StreamChunkHandler` —— 留给 Task 5。先确认 `sink_telegram.go` 自身无语法错。

- [ ] **Step 4：Commit**

```bash
git add pkg/orchestrator/orchestrator.go pkg/orchestrator/sink_telegram.go
git commit -m "refactor(orchestrator): drop dead StreamHandler, add telegramSink"
```

### Task 5：`handleCLI`/`handleTelegram` 改用 EventSink

**Files:**
- Modify: `pkg/orchestrator/orchestrator.go:134-244`

- [ ] **Step 1：改 `handleCLI`**

把 `handleCLI` 整段替换为（不再创建 markdown renderer / 不再 fmt.Print；从 channel 取 sink）：

```go
// handleCLI CLI 模式：通过 channel 提供的 EventSink 上报事件给 Bubble Tea。
func (o *Orchestrator) handleCLI(ctx context.Context, session *Session, agnt agent.Agent, ch channel.Channel) error {
	sink := sinkFromChannel(ch)

	reply, newMsgs, err := agnt.Run(ctx, session.Messages(), sink)

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
	_ = reply
	return nil
}

// sinkFromChannel 若 channel 能提供 EventSink 则用之，否则用 NopSink。
func sinkFromChannel(ch channel.Channel) agent.EventSink {
	if p, ok := ch.(interface{ NewEventSink() agent.EventSink }); ok {
		return p.NewEventSink()
	}
	return agent.NopSink{}
}
```

- [ ] **Step 2：改 `handleTelegram`**

把 `handleTelegram` 中构造 `agent.StreamChunkHandler` 的部分换成 `telegramSink`：

```go
func (o *Orchestrator) handleTelegram(ctx context.Context, session *Session, agnt agent.Agent, msg channel.IncomingMessage, ch channel.Channel) error {
	chatID := msg.ChannelID

	msgID, err := ch.Send(channel.OutgoingMessage{ChatID: chatID, Text: "⏳ 正在处理..."})
	if err != nil {
		return fmt.Errorf("send initial message: %w", err)
	}

	sink := newTelegramSink(ch, chatID, msgID)

	reply, newMsgs, err := agnt.Run(ctx, session.Messages(), sink)

	if len(newMsgs) > 0 {
		session.AppendMessages(newMsgs)
	}
	if err != nil {
		if ctx.Err() != nil {
			ch.EditMessage(msgID, "🛑 任务已取消")
			ch.NotifyDone(chatID, "任务已取消")
			return nil
		}
		ch.EditMessage(msgID, fmt.Sprintf("❌ 执行失败: %v", err))
		return err
	}
	if reply != "" {
		ch.NotifyDone(chatID, "✅ 任务已完成")
	}
	return nil
}
```

- [ ] **Step 3：清理 orchestrator.go 不再使用的 import**

删除不再用到的 `ui`、`textutil`、`time`、`strings`（逐个按编译报错处理；`strings` 可能仍被其他函数用——以 `go build` 为准）。

Run: `go build ./pkg/orchestrator/`
Expected: 成功（除非 `orchestrator_test.go` 的 `fakeAgent` 未更新——那是下个 Task）。

- [ ] **Step 4：Commit**

```bash
git add pkg/orchestrator/orchestrator.go
git commit -m "refactor(orchestrator): route CLI/Telegram streaming through EventSink"
```

### Task 6：迁移 `orchestrator_test.go` 的 fakeAgent

**Files:**
- Modify: `pkg/orchestrator/orchestrator_test.go:35-48`

- [ ] **Step 1：改 `fakeAgent.Run` 签名为 EventSink**

```go
func (f *fakeAgent) Run(_ context.Context, _ []openai.ChatCompletionMessage, sink agent.EventSink) (string, []openai.ChatCompletionMessage, error) {
	if sink != nil && f.reply != "" {
		sink.OnText(f.reply)
		sink.OnComplete(f.reply)
	}
	return f.reply, f.newMsgs, nil
}
```

- [ ] **Step 2：运行 orchestrator 测试**

Run: `go test ./pkg/orchestrator/`
Expected: PASS。

- [ ] **Step 3：全量编译（cli/main 仍会失败，预期）**

Run: `go build ./... 2>&1 | head`
Expected: `pkg/channel/cli` 与 `cmd/agent` 报错（NewEventSink 未实现等）——后续 Phase 修。

- [ ] **Step 4：Commit**

```bash
git add pkg/orchestrator/orchestrator_test.go
git commit -m "test(orchestrator): migrate fakeAgent to EventSink signature"
```

---

## Phase 3：UI lipgloss 样式与运行期守卫

### Task 7：运行期守卫

**Files:**
- Create: `pkg/ui/guard.go`, `pkg/ui/guard_test.go`
- Modify: `pkg/ui/colors.go`（各 `Print*` 前置守卫）

- [ ] **Step 1：写失败测试**

Create `pkg/ui/guard_test.go`：

```go
package ui

import "testing"

func TestProgramActiveGuard(t *testing.T) {
	if IsProgramActive() {
		t.Fatal("default should be inactive")
	}
	SetProgramActive(true)
	if !IsProgramActive() {
		t.Fatal("should be active after SetProgramActive(true)")
	}
	SetProgramActive(false)
	if IsProgramActive() {
		t.Fatal("should be inactive after SetProgramActive(false)")
	}
}
```

- [ ] **Step 2：运行，确认失败**

Run: `go test ./pkg/ui/ -run TestProgramActiveGuard -v`
Expected: FAIL（`IsProgramActive` 未定义）。

- [ ] **Step 3：实现守卫**

Create `pkg/ui/guard.go`：

```go
package ui

import "sync/atomic"

// programActive 标记是否有 Bubble Tea program 正在独占终端。
// 为真时，所有 Print* 直接降级丢弃，避免破坏渲染帧。
var programActive atomic.Bool

// SetProgramActive 由 CLI 在 program.Run 前后调用。
func SetProgramActive(active bool) { programActive.Store(active) }

// IsProgramActive 返回当前是否处于 program 独占期。
func IsProgramActive() bool { return programActive.Load() }
```

- [ ] **Step 4：在 colors.go 的 Print* 前置守卫**

在 `pkg/ui/colors.go` 每个写 stdout 的导出函数（`PrintSuccess`/`PrintError`/`PrintWarning`/`PrintInfo`/`PrintTool`/`PrintToolResult`/`PrintSeparator`/`PrintHeader`/`PrintPrompt`/`PrintAssistantLabel`/`PrintDim`/`PrintBold`）函数体**第一行**加：

```go
	if IsProgramActive() {
		return
	}
```

（`SprintColor`/`SprintfColor` 是纯字符串返回，不加守卫。）

- [ ] **Step 5：测试通过 + 全 ui 包测试**

Run: `go test ./pkg/ui/ -run TestProgramActiveGuard -v`
Expected: PASS

- [ ] **Step 6：Commit**

```bash
git add pkg/ui/guard.go pkg/ui/guard_test.go pkg/ui/colors.go
git commit -m "feat(ui): add runtime program-active guard that drops Print* during TUI"
```

### Task 8：lipgloss 样式定义

**Files:**
- Create: `pkg/ui/styles.go`

- [ ] **Step 1：引入依赖**

Run:
```bash
go get github.com/charmbracelet/lipgloss@latest
go get github.com/charmbracelet/bubbletea@latest
go get github.com/charmbracelet/bubbles@latest
go get github.com/charmbracelet/glamour@latest
```
Expected: `go.mod` 新增四个依赖。

- [ ] **Step 2：定义样式**

Create `pkg/ui/styles.go`：

```go
package ui

import "github.com/charmbracelet/lipgloss"

// 复用现有配色意图（绿=assistant/success，蓝=secondary，黄=warning，红=error）。
var (
	StyleUserPrompt  = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true) // 蓝
	StyleAssistant   = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))            // 绿
	StyleDim         = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))             // 灰
	StyleError       = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))             // 红
	StyleWarning     = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))            // 黄
	StyleToolName    = lipgloss.NewStyle().Bold(true)
	StyleStatusBar   = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

// ToolDisplayName 复用工具中文名映射（来自 tools.go 的 ToolNames）。
func ToolDisplayName(name string) string {
	if dn, ok := ToolNames[name]; ok {
		return dn
	}
	return name
}

// ToolIcon 复用工具图标映射（来自 tools.go 的 ToolIcons）。
func ToolIcon(name string) string {
	if ic, ok := ToolIcons[name]; ok {
		return ic
	}
	return IconTool
}
```

> `ToolNames`/`ToolIcons`/`IconTool` 已存在于 `pkg/ui/tools.go`、`colors.go`，保留。

- [ ] **Step 3：编译**

Run: `go build ./pkg/ui/`
Expected: 成功。

- [ ] **Step 4：Commit**

```bash
git add go.mod go.sum pkg/ui/styles.go
git commit -m "feat(ui): add lipgloss styles and tool display helpers"
```

---

## Phase 4：Bubble Tea Model

### Task 9：tea.Msg 类型与 uiSink

**Files:**
- Create: `pkg/channel/cli/messages.go`, `pkg/channel/cli/sink.go`

- [ ] **Step 1：定义消息类型**

Create `pkg/channel/cli/messages.go`：

```go
package cli

import "mini-code/pkg/agent"

// 这些 tea.Msg 由 uiSink 通过 program.Send 投递，由 model.Update 消费。
type chunkMsg struct{ text string }
type toolStartMsg struct{ calls []agent.ToolCallInfo }
type toolDoneMsg struct{ results []agent.ToolResultInfo }
type waitingMsg struct{}
type completeMsg struct{ full string }
type noticeMsg struct {
	level string // "info" | "error" | "success" | "dim"
	text  string
}
type taskDoneMsg struct{ err error }
```

- [ ] **Step 2：定义 uiSink**

Create `pkg/channel/cli/sink.go`：

```go
package cli

import (
	"mini-code/pkg/agent"

	tea "github.com/charmbracelet/bubbletea"
)

// uiSink 实现 agent.EventSink，把事件投递给 Bubble Tea program。
type uiSink struct{ holder *programHolder }

func (s uiSink) send(msg tea.Msg) {
	if s.holder != nil && s.holder.p != nil {
		s.holder.p.Send(msg)
	}
}

func (s uiSink) OnText(chunk string)                     { s.send(chunkMsg{text: chunk}) }
func (s uiSink) OnWaiting()                              { s.send(waitingMsg{}) }
func (s uiSink) OnToolStart(c []agent.ToolCallInfo)      { s.send(toolStartMsg{calls: c}) }
func (s uiSink) OnToolDone(r []agent.ToolResultInfo)     { s.send(toolDoneMsg{results: r}) }
func (s uiSink) OnComplete(full string)                  { s.send(completeMsg{full: full}) }

var _ agent.EventSink = uiSink{}
```

- [ ] **Step 3：定义 programHolder（占位，runner.go 会用）**

在 `pkg/channel/cli/sink.go` 顶部追加（与 uiSink 同文件）：

```go
// programHolder 持有 *tea.Program 的指针，规避 Model 值拷贝导致拿不到 program。
type programHolder struct{ p *tea.Program }
```

- [ ] **Step 4：编译**

Run: `go build ./pkg/channel/cli/ 2>&1 | head`
Expected: 可能报 runner.go 旧代码冲突——下个 Task 一并改。先确认本文件语法对（`go vet` 单文件不便，靠后续编译）。

- [ ] **Step 5：Commit**

```bash
git add pkg/channel/cli/messages.go pkg/channel/cli/sink.go
git commit -m "feat(cli): add tea.Msg types and uiSink"
```

### Task 10：Model 核心（输入、提交、双击 Esc、历史）

**Files:**
- Create: `pkg/channel/cli/model.go`
- Test: `pkg/channel/cli/model_test.go`

- [ ] **Step 1：写 Model 纯逻辑的失败测试（双击 Esc 窗口判定）**

Create `pkg/channel/cli/model_test.go`：

```go
package cli

import (
	"testing"
	"time"
)

func TestDoubleEscDetectsWithinWindow(t *testing.T) {
	m := newModel(nil, nil)
	m.taskRunning = true

	now := time.Unix(1000, 0)
	// 第一次 Esc：不取消，记录时间
	if m.registerEsc(now) {
		t.Fatal("first Esc should not trigger cancel")
	}
	// 1.0s 后第二次：应触发
	if !m.registerEsc(now.Add(time.Second)) {
		t.Fatal("second Esc within window should trigger cancel")
	}
}

func TestDoubleEscOutsideWindow(t *testing.T) {
	m := newModel(nil, nil)
	m.taskRunning = true
	now := time.Unix(1000, 0)
	m.registerEsc(now)
	if m.registerEsc(now.Add(2 * time.Second)) {
		t.Fatal("second Esc outside 1.5s window should NOT trigger")
	}
}

func TestHistoryNavigation(t *testing.T) {
	m := newModel(nil, nil)
	m.history = []string{"first", "second"}
	if got := m.historyPrev(); got != "second" {
		t.Fatalf("prev = %q, want second", got)
	}
	if got := m.historyPrev(); got != "first" {
		t.Fatalf("prev = %q, want first", got)
	}
	if got := m.historyNext(); got != "second" {
		t.Fatalf("next = %q, want second", got)
	}
}
```

- [ ] **Step 2：运行，确认失败**

Run: `go test ./pkg/channel/cli/ -run "TestDoubleEsc|TestHistory" -v`
Expected: FAIL（`newModel`/方法未定义）。

- [ ] **Step 3：实现 Model**

Create `pkg/channel/cli/model.go`：

```go
package cli

import (
	"context"
	"strings"
	"time"

	"mini-code/pkg/agent"
	"mini-code/pkg/channel"
	"mini-code/pkg/orchestrator"
	"mini-code/pkg/ui"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
)

const doubleEscWindow = 1500 * time.Millisecond

// taskRunner 抽象 orchestrator.Handle，便于测试注入。
type taskRunner func(ctx context.Context, text string) error

type model struct {
	ta       textarea.Model
	sp       spinner.Model
	renderer *glamour.TermRenderer

	runTask taskRunner // nil 时输入被忽略（测试用）
	holder  *programHolder

	taskRunning bool
	streamBuf   strings.Builder // 流式纯文本累积，OnComplete 时 glamour 整段渲染
	statusText  string

	history    []string
	historyIdx int // 指向"下一个更旧"的位置，== len 表示在编辑区

	lastEsc time.Time
	cancel  context.CancelFunc
	baseCtx context.Context
}

func newModel(baseCtx context.Context, run taskRunner) model {
	ta := textarea.New()
	ta.Placeholder = "输入任务，Enter 提交，Shift+Enter 换行..."
	ta.Prompt = "➜ "
	ta.ShowLineNumbers = false
	ta.Focus()
	ta.SetHeight(1)

	sp := spinner.New()
	sp.Spinner = spinner.Dot

	r, _ := glamour.NewTermRenderer(glamour.WithStandardStyle("dark"), glamour.WithWordWrap(100))

	return model{
		ta:         ta,
		sp:         sp,
		renderer:   r,
		runTask:    run,
		baseCtx:    baseCtx,
		historyIdx: 0,
	}
}

func (m model) Init() tea.Cmd { return textarea.Blink }

// registerEsc 记录一次 Esc；返回 true 表示构成双击（应取消任务）。
func (m *model) registerEsc(now time.Time) bool {
	if !m.taskRunning {
		return false
	}
	if !m.lastEsc.IsZero() && now.Sub(m.lastEsc) < doubleEscWindow {
		m.lastEsc = time.Time{}
		return true
	}
	m.lastEsc = now
	return false
}

func (m *model) historyPrev() string {
	if len(m.history) == 0 {
		return m.ta.Value()
	}
	if m.historyIdx < len(m.history) {
		m.historyIdx++
	}
	return m.history[len(m.history)-m.historyIdx]
}

func (m *model) historyNext() string {
	if m.historyIdx > 1 {
		m.historyIdx--
		return m.history[len(m.history)-m.historyIdx]
	}
	m.historyIdx = 0
	return ""
}
```

> 时间来源：测试直接调用 `registerEsc(now)` 注入时间；`Update` 里用 `time.Now()`（见 Task 11）。`time.Now()` 在生产代码可用（仅 Workflow 脚本环境禁用，本项目正常运行不受限）。

- [ ] **Step 4：运行测试，确认通过**

Run: `go test ./pkg/channel/cli/ -run "TestDoubleEsc|TestHistory" -v`
Expected: PASS（三个测试）。

- [ ] **Step 5：Commit**

```bash
git add pkg/channel/cli/model.go pkg/channel/cli/model_test.go
git commit -m "feat(cli): bubbletea model core (input, double-Esc, history)"
```

### Task 11：Model.Update 与 View

**Files:**
- Modify: `pkg/channel/cli/model.go`

- [ ] **Step 1：实现 Update**

在 `pkg/channel/cli/model.go` 追加：

```go
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlD:
			return m, tea.Quit
		case tea.KeyEsc:
			if m.registerEsc(time.Now()) {
				if m.cancel != nil {
					m.cancel()
				}
				return m, tea.Println(ui.StyleWarning.Render("已中止任务"))
			}
			if m.taskRunning {
				return m, tea.Println(ui.StyleDim.Render("  [再按一次 Esc 可中止任务]"))
			}
			return m, nil
		case tea.KeyEnter:
			if m.taskRunning {
				return m, nil // 任务进行中忽略提交
			}
			text := strings.TrimSpace(m.ta.Value())
			if text == "" {
				return m, nil
			}
			// Shift+Enter 由 textarea 处理为换行；这里只处理纯 Enter 提交。
			return m.submit(text)
		case tea.KeyUp:
			if !m.taskRunning {
				m.ta.SetValue(m.historyPrev())
			}
			return m, nil
		case tea.KeyDown:
			if !m.taskRunning {
				m.ta.SetValue(m.historyNext())
			}
			return m, nil
		}

	case chunkMsg:
		m.streamBuf.WriteString(msg.text)
		// 流式期间不逐块提交，仅累积；活动区 View 显示尾部预览（见 View）。
		return m, nil

	case toolStartMsg:
		var cmds2 []tea.Cmd
		for _, c := range msg.calls {
			line := "  " + ui.ToolIcon(c.Name) + " " + ui.StyleToolName.Render(ui.ToolDisplayName(c.Name))
			if c.ArgsSummary != "" {
				line += " " + ui.StyleDim.Render(c.ArgsSummary)
			}
			cmds2 = append(cmds2, tea.Println(line))
		}
		m.statusText = "执行工具..."
		return m, tea.Batch(cmds2...)

	case toolDoneMsg:
		var cmds2 []tea.Cmd
		for _, r := range msg.results {
			icon := ui.IconSuccess
			style := ui.StyleDim
			if !r.OK {
				icon = ui.IconError
				style = ui.StyleError
			}
			summary := strings.ReplaceAll(r.Summary, "\n", " ")
			if len(summary) > 80 {
				summary = summary[:80]
			}
			cmds2 = append(cmds2, tea.Println("    "+icon+" "+style.Render(summary)))
		}
		return m, tea.Batch(cmds2...)

	case waitingMsg:
		m.statusText = "等待响应..."
		return m, nil

	case completeMsg:
		// 整段 glamour 渲染流式累积的文本，提交进滚动历史。
		out := m.streamBuf.String()
		m.streamBuf.Reset()
		rendered := out
		if m.renderer != nil && strings.TrimSpace(out) != "" {
			if s, err := m.renderer.Render(out); err == nil {
				rendered = strings.TrimRight(s, "\n")
			}
		}
		return m, tea.Println(rendered)

	case noticeMsg:
		return m, tea.Println(m.renderNotice(msg))

	case taskDoneMsg:
		m.taskRunning = false
		m.statusText = ""
		m.lastEsc = time.Time{}
		m.ta.Reset()
		m.ta.Focus()
		var c tea.Cmd
		if msg.err != nil {
			c = tea.Println(ui.StyleError.Render("运行失败: " + msg.err.Error()))
		}
		return m, c

	case spinner.TickMsg:
		var c tea.Cmd
		m.sp, c = m.sp.Update(msg)
		return m, c
	}

	var c tea.Cmd
	m.ta, c = m.ta.Update(msg)
	cmds = append(cmds, c)
	return m, tea.Batch(cmds...)
}

func (m model) submit(text string) (tea.Model, tea.Cmd) {
	m.history = append(m.history, text)
	m.historyIdx = 0
	m.ta.Reset()
	m.taskRunning = true
	m.statusText = "正在思考..."

	// 回显用户输入到滚动历史
	echo := tea.Println(ui.StyleUserPrompt.Render("➜ ") + text)

	taskCtx, cancel := context.WithCancel(m.baseCtx)
	m.cancel = cancel

	run := m.runTask
	task := func() tea.Msg {
		if run == nil {
			return taskDoneMsg{}
		}
		err := run(taskCtx, text)
		return taskDoneMsg{err: err}
	}
	return m, tea.Batch(echo, m.sp.Tick, task)
}

func (m model) renderNotice(n noticeMsg) string {
	switch n.level {
	case "error":
		return ui.StyleError.Render(n.text)
	case "warning":
		return ui.StyleWarning.Render(n.text)
	case "dim":
		return ui.StyleDim.Render(n.text)
	default:
		return n.text
	}
}
```

- [ ] **Step 2：实现 View**

追加：

```go
func (m model) View() string {
	var b strings.Builder
	if m.taskRunning {
		b.WriteString(m.sp.View())
		b.WriteString(" ")
		b.WriteString(ui.StyleStatusBar.Render(m.statusText))
		// 流式尾部预览（纯文本，最后两行）
		if buf := m.streamBuf.String(); buf != "" {
			lines := strings.Split(strings.TrimRight(buf, "\n"), "\n")
			tail := lines
			if len(lines) > 2 {
				tail = lines[len(lines)-2:]
			}
			b.WriteString("\n")
			b.WriteString(ui.StyleDim.Render(strings.Join(tail, "\n")))
		}
		b.WriteString("\n")
		return b.String()
	}
	b.WriteString(m.ta.View())
	return b.String()
}
```

- [ ] **Step 3：teatest 冒烟测试**

在 `pkg/channel/cli/model_test.go` 追加：

```go
import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
)

func TestModelEchoesSubmittedInput(t *testing.T) {
	done := make(chan struct{})
	run := func(_ context.Context, text string) error { close(done); return nil }
	m := newModel(context.Background(), run)

	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(80, 24))
	tm.Type("hello")
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})

	<-done
	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlD})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))

	out := tm.FinalOutput(t)
	if !strings.Contains(readAll(t, out), "hello") {
		t.Fatal("expected echoed input 'hello' in output")
	}
}
```

并加辅助：

```go
import "io"

func readAll(t *testing.T, r io.Reader) string {
	t.Helper()
	b, _ := io.ReadAll(r)
	return string(b)
}
```

Run:
```bash
go get github.com/charmbracelet/x/exp/teatest@latest
go test ./pkg/channel/cli/ -run TestModel -v
```
Expected: PASS（若 teatest API 名有出入，按其当前版本调整 `WaitFinished`/`FinalOutput` 调用；核心断言"输出含 hello"不变）。

- [ ] **Step 4：全 cli 包测试**

Run: `go test ./pkg/channel/cli/`
Expected: PASS

- [ ] **Step 5：Commit**

```bash
git add pkg/channel/cli/model.go pkg/channel/cli/model_test.go go.mod go.sum
git commit -m "feat(cli): implement model Update/View with glamour and teatest smoke"
```

---

## Phase 5：CLIChannel 重写与 main.go 接线

### Task 12：重写 CLIChannel

**Files:**
- Modify: `pkg/channel/cli/runner.go`
- Modify: `pkg/channel/cli/runner_test.go`（按新结构调整或精简）

- [ ] **Step 1：重写 runner.go**

把 `pkg/channel/cli/runner.go` 整体替换为：

```go
package cli

import (
	"context"
	"mini-code/pkg/agent"
	"mini-code/pkg/channel"
	"mini-code/pkg/ui"

	tea "github.com/charmbracelet/bubbletea"
)

// CLIChannel 由 Bubble Tea program 驱动的 CLI 频道。
type CLIChannel struct {
	holder   *programHolder
	messages chan channel.IncomingMessage
}

// New 创建 CLIChannel。program 在 BuildProgram 时绑定。
func New() *CLIChannel {
	return &CLIChannel{
		holder:   &programHolder{},
		messages: make(chan channel.IncomingMessage, 1),
	}
}

// BuildProgram 用给定 taskRunner 构造 tea.Program 并回填 holder。
func (c *CLIChannel) BuildProgram(baseCtx context.Context, run taskRunner) *tea.Program {
	m := newModel(baseCtx, run)
	m.holder = c.holder
	p := tea.NewProgram(m)
	c.holder.p = p
	return p
}

// NewEventSink 提供 orchestrator 用的事件 sink。
func (c *CLIChannel) NewEventSink() agent.EventSink { return uiSink{holder: c.holder} }

func (c *CLIChannel) ChannelID() string                              { return "cli" }
func (c *CLIChannel) Messages() <-chan channel.IncomingMessage       { return c.messages }
func (c *CLIChannel) Start(_ context.Context) error                  { return nil } // 由 program.Run 驱动
func (c *CLIChannel) Send(msg channel.OutgoingMessage) (string, error) {
	if c.holder.p != nil {
		c.holder.p.Send(noticeMsg{level: "info", text: msg.Text})
	}
	return "", nil
}
func (c *CLIChannel) SendFile(_ string, path string) error {
	if c.holder.p != nil {
		c.holder.p.Send(noticeMsg{level: "info", text: "文件: " + path})
	}
	return nil
}
func (c *CLIChannel) EditMessage(_ string, _ string) error { return nil }
func (c *CLIChannel) NotifyDone(_ string, text string) error {
	if c.holder.p != nil {
		c.holder.p.Send(noticeMsg{level: "success", text: text})
	}
	return nil
}

var _ channel.Channel = (*CLIChannel)(nil)
var _ = ui.IsProgramActive // 保持 ui 依赖（供守卫；如未直接用可删此行）
```

> `model` 的 `holder` 字段需可被外部包内赋值——同包，直接 `m.holder = c.holder` 可行。

- [ ] **Step 2：调整/精简 runner_test.go**

`runner_test.go` 旧测试依赖 readline 注入。改为针对新 API：

```go
package cli

import (
	"context"
	"testing"
)

func TestNewEventSinkImplementsInterface(t *testing.T) {
	c := New()
	c.BuildProgram(context.Background(), nil)
	sink := c.NewEventSink()
	// nil-safe 调用不应 panic
	sink.OnText("x")
	sink.OnComplete("x")
}
```

（删除旧的、依赖 `readline.Instance` 的测试用例。）

- [ ] **Step 3：编译 + 测试**

Run: `go build ./pkg/channel/cli/ && go test ./pkg/channel/cli/`
Expected: PASS

- [ ] **Step 4：Commit**

```bash
git add pkg/channel/cli/runner.go pkg/channel/cli/runner_test.go
git commit -m "refactor(cli): CLIChannel driven by bubbletea program"
```

### Task 13：main.go 接线

**Files:**
- Modify: `cmd/agent/main.go:355-487`（runWorker 中输入/循环部分）

- [ ] **Step 1：替换 readline 初始化与主循环**

在 `runWorker` 中：

1. 删除 `readline.NewEx(...)` 那段（357–371 行）及 `rl` 变量、`defer rl.Close()`。
2. 删除 `cliCh := cli.New(rl)` 改为 `cliCh := cli.New()`。
3. 删除文件顶部 `"github.com/ergochat/readline"` import 与 `getCompleter()`（570–587 行，readline 专用）。
4. 把 EscMonitor 相关全部删除：`ui.GlobalEscMonitor.SetCancelFunc(cancel)`（441 行）、Start/Stop（472/476 行）。
5. 用下面的 program 驱动取代 `printWelcome(); go cliCh.Start(ctx)` 与其后的 `for { select ... }` 块（453–486 行）：

```go
	printWelcome()

	run := func(taskCtx context.Context, text string) error {
		// 内置命令拦截
		if handleBuiltinCommand(text, orch, cliCh.ChannelID(), "local") {
			return nil
		}
		msg := channel.IncomingMessage{ChannelID: cliCh.ChannelID(), UserID: "local", Text: text}
		return orch.Handle(taskCtx, msg, factory, cliCh)
	}

	program := cliCh.BuildProgram(ctx, run)

	ui.SetProgramActive(true)
	_, runErr := program.Run()
	ui.SetProgramActive(false)

	if runErr != nil {
		return runErr
	}
	return nil
```

> `channel` 包需在 main.go import（已有 `mini-code/pkg/channel/cli`，补 `mini-code/pkg/channel`）。

- [ ] **Step 2：内置命令输出改走 program**

`handleBuiltinCommand` 内多处 `ui.Print*`/`fmt.Println` 在 program 激活期会被守卫丢弃。改为通过 program 发 `noticeMsg`。最简做法：让 `handleBuiltinCommand` 返回要显示的文本，由 `run` 闭包 `program.Send`。但为控制改动量，改为直接在 `handleBuiltinCommand` 里用包级 program 引用发送。

实现：给 main 包加一个文件级变量与辅助（在 `cmd/agent/main.go` 顶部 `var activeProgram *tea.Program`），在 `program := cliCh.BuildProgram(...)` 后赋值 `activeProgram = program`；`handleBuiltinCommand` 内把 `ui.PrintInfo(...)`/`fmt.Println(...)` 替换为：

```go
	if activeProgram != nil {
		activeProgram.Println(/* 文本 */)
	}
```

> `tea.Program.Println` 在 v1 存在（直接打印到滚动历史，线程安全）。`clear`/`cls` 用 `activeProgram.Println` 不能清屏——内联模式下"清屏"语义弱化，改为打印分隔提示即可（接受行为微调）。`exit/quit` 仍 `os.Exit(0)` 之前先 `activeProgram.Quit()` 让终端恢复。

逐条改：`help` → `activeProgram.Println(ui.HelpPanel())`；`reset/new` → 业务逻辑不变，提示文本走 `Println`；`history` → 走 `Println`；`version` → 走 `Println`；`exit/quit/q` → `if activeProgram != nil { activeProgram.Quit() }; ` 之后保留 `os.Exit(0)`。

- [ ] **Step 3：信号处理与退出**

`sigChan` 的 `os.Exit(0)` 前加 `if activeProgram != nil { activeProgram.Quit() }`，确保终端状态恢复。

- [ ] **Step 4：编译**

Run: `go build ./cmd/agent/`
Expected: 成功。若报 `readline`/`getCompleter`/`GlobalEscMonitor` 残留引用，逐个清除。

- [ ] **Step 5：手动冒烟**

Run: `go run ./cmd/agent`（需 `.env` 配好 provider）
手动验证：输入一句话→看到回显 `➜ ...`→spinner→工具行/最终 glamour 块→可继续输入；任务中双击 Esc 中止；`exit` 正常退出且终端不乱。

- [ ] **Step 6：Commit**

```bash
git add cmd/agent/main.go
git commit -m "feat(cmd/agent): drive CLI with bubbletea program, remove readline/esc loop"
```

---

## Phase 6：删除被取代的旧代码

### Task 14：删除 esc_monitor / readline input / 自研 markdown / spinner

**Files:** 见各步

- [ ] **Step 1：删除 esc_monitor 全套**

```bash
git rm pkg/ui/esc_monitor.go pkg/ui/esc_monitor_unix.go pkg/ui/esc_monitor_windows.go pkg/ui/esc_monitor_test.go
```

Run: `grep -rn "EscMonitor\|GlobalEscMonitor\|nonBlockingRead" pkg cmd`
Expected: 无输出。若有残留引用，先清理再删。

- [ ] **Step 2：删除自研 markdown / highlight / theme（已被 glamour 取代）**

```bash
git rm pkg/ui/markdown.go pkg/ui/markdown_test.go pkg/ui/highlight.go pkg/ui/highlight_test.go pkg/ui/theme.go pkg/ui/theme_test.go
```

Run: `grep -rn "NewMarkdownRenderer\|MarkdownRenderer\|ui.Highlight\b" pkg cmd`
Expected: 无输出（orchestrator 已不再用）。

- [ ] **Step 3：处理 input.go**

```bash
grep -rn "ui.ReadLine\|ui.ReadMultiLine\|NewReadlineInput\|NewInputHandler\|ui.Confirm\|ui.Select\b" pkg cmd
```
- 若 `Confirm`/`Select`/`ReadLineSimple` 无任何引用：`git rm pkg/ui/input.go`。
- 若仍有引用：仅删除 `input.go` 内 readline 相关（`NewReadlineInput`/`getCompleter`/`InputHandler`/`ReadLine`/`ReadMultiLine`），保留被引用的纯函数，并删除 `github.com/ergochat/readline` import。

- [ ] **Step 4：处理 progress.go 的 Spinner**

```bash
grep -rn "ui.NewSpinner\|ui.Spinner\|NewProgressBar\|NewMultiProgress\|ThinkingDisplay\|NewStreamingText\|StartToolCall" pkg cmd | grep -v _test
```
- 全无引用：`git rm pkg/ui/progress.go` 与 `pkg/ui/tools.go` 中无用的 `Spinner`/`ToolCallDisplay`/`StreamingText`/`ThinkingDisplay`（保留仍被 `styles.go` 用到的 `ToolNames`/`ToolIcons`、被 `helpers.go` 用到的 `LogLevel`/`SetLogLevel`/`currentLogLevel`）。
- 有引用：仅删未用部分。

> 谨慎：`ToolNames`/`ToolIcons`（tools.go）、`currentLogLevel`/`SetLogLevel`/`LogLevel`（tools.go）仍被 `styles.go`/`helpers.go`/main 使用，**不要删**。

- [ ] **Step 5：清理 go.mod 未用依赖**

Run: `go mod tidy`
Expected: 移除 `github.com/ergochat/readline`（若已无引用）；保留 chroma（glamour 间接依赖会自动留为 indirect）。

- [ ] **Step 6：全量编译 + 测试**

Run: `go build ./... && go test ./...`
Expected: 全 PASS。

- [ ] **Step 7：Commit**

```bash
git add -A
git commit -m "chore(ui): remove esc_monitor, readline input, custom markdown, legacy spinner"
```

---

## Phase 7：热重启终端交接时序

### Task 15：program 退出后再交接

**Files:**
- Modify: `cmd/agent/main.go`（restart 流程）、确认 `pkg/orchestrator/orchestrator.go:applyPendingRestart`

- [ ] **Step 1：理解现状**

`orch.Handle` 末尾调用 `applyPendingRestart`，它在 `RestartRuntime.ApplyPendingRestart` 里触发交接（exec 新 binary）。在 Bubble Tea 接管下，这发生在 `run` 闭包（tea.Cmd goroutine）内——此时 program 仍在跑、终端仍被 bubbletea 占用，直接 exec 会导致终端状态错乱。

- [ ] **Step 2：让重启先退出 program**

修改 `run` 闭包：捕获 `applyPendingRestart` 的"需要重启"信号，先让 program 退出，再在 `program.Run()` 返回后执行交接。

具体：给 orchestrator 增加查询方法（或复用 `RestartRuntime.HasPendingRestart`）。在 `run` 闭包末尾不直接 exec，而是：

```go
	run := func(taskCtx context.Context, text string) error {
		if handleBuiltinCommand(text, orch, cliCh.ChannelID(), "local") {
			return nil
		}
		msg := channel.IncomingMessage{ChannelID: cliCh.ChannelID(), UserID: "local", Text: text}
		err := orch.Handle(taskCtx, msg, factory, cliCh)
		if workerRuntimePendingRestart() { // 见下
			activeProgram.Quit() // 触发 program.Run() 返回
		}
		return err
	}
```

其中 `applyPendingRestart` 改为**不在 Handle 内 exec**，而是仅标记 pending；真正的交接放在 `program.Run()` 返回之后：

```go
	program := cliCh.BuildProgram(ctx, run)
	activeProgram = program
	ui.SetProgramActive(true)
	_, runErr := program.Run()
	ui.SetProgramActive(false)
	if runErr != nil {
		return runErr
	}
	// program 已退出、终端已恢复 → 此时安全交接
	if pending := orch.TakePendingRestartSnapshot(); pending != nil {
		return orch.ApplyRestartNow(*pending) // 新增：执行 exec 交接
	}
	return nil
```

- [ ] **Step 3：在 orchestrator 暴露 pending 快照的取出/执行**

在 `pkg/orchestrator/orchestrator.go` 把 `applyPendingRestart(session)` 拆成两步：
- `Handle` 末尾：若 `o.restart != nil && o.restart.HasPendingRestart()`，把 `session.ExportSnapshot()` 暂存到 `o.pendingSnapshot`（新增字段 `pendingSnapshot *SessionSnapshot`），**不**调用 `ApplyPendingRestart`。
- 新增 `func (o *Orchestrator) TakePendingRestartSnapshot() *SessionSnapshot`（返回并清空）。
- 新增 `func (o *Orchestrator) ApplyRestartNow(s SessionSnapshot) error { return o.restart.ApplyPendingRestart(s) }`。

`workerRuntimePendingRestart()` 在 main 包用 `orch.HasPendingRestartPublic()` 之类暴露，或直接让 `run` 闭包持有 `orch` 调 `orch.restart`（不可，未导出）——因此新增导出方法 `func (o *Orchestrator) HasPendingRestart() bool`。

- [ ] **Step 4：编译 + 现有重启测试**

Run: `go build ./... && go test ./pkg/orchestrator/ ./pkg/supervisor/`
Expected: PASS（`session_snapshot_test.go` 等不应回归）。

- [ ] **Step 5：手动验证热重启**

Run: 启动 `go run ./cmd/agent`，触发 `mini-code restart`（或让 agent 调 restart 工具），确认：旧 program 退出→终端恢复→新 worker 接管→会话延续、终端无错乱（**重点在 Windows 上验证**）。

- [ ] **Step 6：Commit**

```bash
git add cmd/agent/main.go pkg/orchestrator/orchestrator.go
git commit -m "fix(restart): hand off terminal only after bubbletea program exits"
```

---

## Phase 8：最终集成

### Task 16：全量验证与清扫

- [ ] **Step 1：gofmt + vet**

Run: `gofmt -w ./cmd ./pkg && go vet ./...`
Expected: 无输出。

- [ ] **Step 2：全量测试 + 覆盖率**

Run: `go test ./...`
Expected: 全 PASS。

- [ ] **Step 3：扫净运行期残留 `ui.Print*`/`fmt.Print`**

Run: `grep -rn "ui.Print\|fmt.Print" pkg/agent pkg/orchestrator | grep -v _test`
Expected: agent 包应无 `ui.Print`（已移除）；orchestrator 仅在 program 未激活路径（如不存在）。逐条确认运行期路径不直接打印（依赖守卫兜底）。

- [ ] **Step 4：构建双形态二进制**

Run:
```bash
go build -o mini-code.exe ./cmd/agent
go build -o mini-code-telegram.exe ./cmd/telegram
```
Expected: 两个二进制构建成功。

- [ ] **Step 5：Telegram 回归手测**

启动 telegram bot，发一条消息，确认流式 EditMessage 与最终文本与迁移前一致（telegramSink 行为不变）。

- [ ] **Step 6：更新文档**

在 `CLAUDE.md` 的「关键设计决策」与 `pkg/ui/` 描述处，更新为 Bubble Tea 内联模式 + EventSink 事件流；移除对 esc_monitor / 自研 markdown 的描述。

```bash
git add CLAUDE.md
git commit -m "docs: update architecture notes for bubbletea TUI migration"
```

---

## 自检（写完计划后回看 spec）

- **Spec §3.1 循环翻转** → Task 13（main.go program 驱动）✅
- **Spec §3.2 agent 改发事件** → Task 1–3、6 ✅
- **Spec §4 组件映射**：textarea→Task 10；esc→KeyMsg Task 10–11；spinner→Task 11；glamour→Task 11；lipgloss→Task 8；CLIChannel→Task 12 ✅
- **Spec §4.1 ui.Print 路由 + 降级守卫** → Task 7、13 Step 2 ✅
- **Spec §4.2 热重启交接** → Task 15 ✅
- **Spec §4.3 glamour 流式策略** → Task 11（流式纯文本预览 + OnComplete 整段渲染）✅
- **Spec §5 测试策略**：fake sink（Task 3）、teatest（Task 11）✅
- **Spec 非目标**：Telegram 不变（telegramSink 保留节流，Task 4/5）；engine.go 不动（决策 2）✅
- **类型一致性**：`EventSink`/`NopSink`/`ToolCallInfo`/`ToolResultInfo`/`uiSink`/`telegramSink`/`programHolder`/`taskRunner`/各 `*Msg` 全程命名一致 ✅
- **占位符扫描**：无 TBD/TODO；所有 step 含具体代码或命令 ✅

## 已知风险与缓解

- **bubbletea/bubbles/glamour API 版本差异**：计划按 v1 API 线编写；执行时若 `teatest.FinalOutput`/`textarea` 字段名有出入，按拉到的实际版本微调（核心结构不变）。
- **Shift+Enter 换行**：部分终端不区分；若 textarea 默认 Enter 即换行，需在 Update 里改判定（如 `Alt+Enter`/双 Enter 提交）作为兜底——执行 Task 11 时按实测终端行为确认键位。
- **CJK 宽字符换行**：glamour `WithWordWrap(100)` 在中文宽字符下可能不准；Task 16 手测时确认，必要时改用 `WithEmoji()`/调小 wrap。
- **热重启在 Windows 的终端恢复**：Task 15 Step 5 重点验证。
