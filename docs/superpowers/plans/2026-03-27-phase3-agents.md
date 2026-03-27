# Phase 3: 多 Agent 专化 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 ReviewerAgent 和 ResearcherAgent（受限工具集），在 Orchestrator 中添加意图路由，并注册 dispatch_agent 工具支持 CoderAgent 调起子 Agent。

**Architecture:** 三类 Agent 均基于 BaseAgent，通过 `allowedTools` 限制工具访问范围；Router 使用关键词匹配选择 Agent 类型；dispatch_agent 工具在当前 goroutine 内直接实例化子 Agent 执行，结果字符串返回给调用方。

**Tech Stack:** Go 标准库，复用 Phase 1/2 的 BaseAgent 和 Memory Store。

**前置条件：** Phase 1 和 Phase 2 已完成，全部测试通过。

---

## 文件变动清单

| 操作 | 文件 | 说明 |
|------|------|------|
| Create | `pkg/agent/coder.go` | CoderAgent（全部工具） |
| Create | `pkg/agent/reviewer.go` | ReviewerAgent（只读工具） |
| Create | `pkg/agent/researcher.go` | ResearcherAgent（搜索工具） |
| Create | `pkg/agent/agents_test.go` | 三类 Agent 工具集测试 |
| Create | `pkg/orchestrator/router.go` | 意图路由（关键词匹配） |
| Create | `pkg/orchestrator/router_test.go` | 路由规则测试 |
| Create | `pkg/tools/dispatch.go` | dispatch_agent 工具实现 |
| Create | `pkg/tools/dispatch_test.go` | dispatch 工具测试 |
| Modify | `pkg/tools/registry.go` | 注册 dispatch_agent 工具 |
| Modify | `pkg/orchestrator/orchestrator.go` | 集成 Router，按意图选 Agent |
| Modify | `cmd/agent/main.go` | 传入 Agent 工厂给 Orchestrator |

---

## Task 1: 三类专化 Agent

**Files:**
- Create: `pkg/agent/coder.go`
- Create: `pkg/agent/reviewer.go`
- Create: `pkg/agent/researcher.go`
- Create: `pkg/agent/agents_test.go`

- [ ] **Step 1.1: 写工具集测试**

```go
// pkg/agent/agents_test.go
package agent_test

import (
	"testing"
	"mini-code/pkg/agent"
	"mini-code/pkg/provider"
)

func newTestProvider() provider.Provider {
	// 返回一个不实际调用的空 Provider（仅用于工具集测试）
	return nil
}

func TestCoderAgent_HasAllTools(t *testing.T) {
	a := agent.NewCoderAgent(newTestProvider(), "model")
	tools := a.AllowedTools()
	// CoderAgent 传 nil 表示允许全部工具
	if tools != nil {
		t.Error("CoderAgent should allow all tools (nil allowedTools)")
	}
}

func TestReviewerAgent_OnlyHasReadOnlyTools(t *testing.T) {
	a := agent.NewReviewerAgent(newTestProvider(), "model")
	allowed := make(map[string]bool)
	for _, t := range a.AllowedTools() {
		allowed[t] = true
	}

	// 必须有的只读工具
	for _, required := range []string{"read_file", "git_diff", "git_log", "recall"} {
		if !allowed[required] {
			t.Errorf("ReviewerAgent missing required tool: %s", required)
		}
	}
	// 禁止写操作
	for _, forbidden := range []string{"write_file", "run_shell", "delete_file", "remember"} {
		if allowed[forbidden] {
			t.Errorf("ReviewerAgent should NOT have tool: %s", forbidden)
		}
	}
}

func TestResearcherAgent_HasSearchTools(t *testing.T) {
	a := agent.NewResearcherAgent(newTestProvider(), "model")
	allowed := make(map[string]bool)
	for _, t := range a.AllowedTools() {
		allowed[t] = true
	}

	for _, required := range []string{"search_in_files", "download_file", "recall", "remember"} {
		if !allowed[required] {
			t.Errorf("ResearcherAgent missing tool: %s", required)
		}
	}
	// ResearcherAgent 不能写文件
	if allowed["write_file"] {
		t.Error("ResearcherAgent should NOT have write_file")
	}
}
```

- [ ] **Step 1.2: 运行测试确认失败**

```bash
go test ./pkg/agent/... -run TestCoderAgent -v
```
预期：`undefined: agent.NewCoderAgent`

- [ ] **Step 1.3: 先修改 base.go，添加 systemPromptOverride 和 agentName 字段**

> **必须先于 Steps 1.3-1.5 执行**：coder/reviewer/researcher 需要直接赋值这两个字段。

在 `pkg/agent/base.go` 的 `BaseAgent` 结构体中添加两个字段：

```go
type BaseAgent struct {
	provider             provider.Provider
	model                string
	allowedTools         []string
	debugLogger          *log.Logger
	maxConcurrency       int
	verbose              bool
	maxTurns             int
	systemPromptOverride string // 专化 Agent 覆盖默认 system prompt
	agentName            string // 专化 Agent 名称
}
```

更新 `Name()` 方法：
```go
func (a *BaseAgent) Name() string {
	if a.agentName != "" {
		return a.agentName
	}
	return "base"
}
```

在 `Run()` 方法最开头（`var newMessages` 之前）添加 system prompt override 逻辑：
```go
// 应用 system prompt override（专化 Agent 覆盖 Orchestrator 传入的 system message）
if a.systemPromptOverride != "" && len(messages) > 0 && messages[0].Role == "system" {
	overrideMsgs := make([]openai.ChatCompletionMessage, len(messages))
	copy(overrideMsgs, messages)
	overrideMsgs[0].Content = a.systemPromptOverride
	messages = overrideMsgs
}
```

- [ ] **Step 1.4: 创建 coder.go**

```go
// pkg/agent/coder.go
package agent

import "mini-code/pkg/provider"

const coderSystemPrompt = `你是专业的 AI 编程助手。

核心准则：
1. 修改代码前先阅读相关文件，理解现有实现
2. 优先使用 replace_in_file 进行最小化修改
3. 修改后运行测试验证正确性
4. 使用 remember 工具记录重要的项目决策和技术选型
5. 保持代码风格与项目现有风格一致`

// NewCoderAgent 创建编码 Agent，拥有全部工具权限
func NewCoderAgent(p provider.Provider, model string) *BaseAgent {
	a := NewBaseAgent(p, model, nil) // nil = 全部工具
	a.systemPromptOverride = coderSystemPrompt
	a.agentName = "coder"
	return a
}
```

- [ ] **Step 1.5: 创建 reviewer.go**

```go
// pkg/agent/reviewer.go
package agent

import "mini-code/pkg/provider"

var reviewerTools = []string{
	"read_file", "list_files", "search_in_files",
	"git_diff", "git_log", "git_status",
	"get_file_info", "recall",
}

const reviewerSystemPrompt = `你是专业的代码审查员。

核心准则：
1. 只读取和分析代码，不直接修改文件
2. 关注代码质量、安全性、可维护性
3. 给出具体、可操作的改进建议
4. 指出潜在的 bug 和性能问题`

// NewReviewerAgent 创建代码审查 Agent，只有只读工具权限
func NewReviewerAgent(p provider.Provider, model string) *BaseAgent {
	a := NewBaseAgent(p, model, reviewerTools)
	a.systemPromptOverride = reviewerSystemPrompt
	a.agentName = "reviewer"
	return a
}
```

- [ ] **Step 1.6: 创建 researcher.go**

```go
// pkg/agent/researcher.go
package agent

import "mini-code/pkg/provider"

var researcherTools = []string{
	"read_file", "list_files", "search_in_files",
	"download_file", "run_shell",
	"recall", "remember",
}

const researcherSystemPrompt = `你是技术调研专家。

核心准则：
1. 广泛搜索相关信息后再给出结论
2. 区分已确认的事实和推测
3. 提供有依据的技术建议
4. 将重要的调研结论用 remember 工具保存`

// NewResearcherAgent 创建技术调研 Agent
func NewResearcherAgent(p provider.Provider, model string) *BaseAgent {
	a := NewBaseAgent(p, model, researcherTools)
	a.systemPromptOverride = researcherSystemPrompt
	a.agentName = "researcher"
	return a
}
```

- [ ] **Step 1.7: 运行测试**

```bash
go test ./pkg/agent/... -v
```
预期：全部 PASS

- [ ] **Step 1.8: Commit**

```bash
git add pkg/agent/coder.go pkg/agent/reviewer.go pkg/agent/researcher.go pkg/agent/agents_test.go pkg/agent/base.go pkg/agent/agent.go
git commit -m "feat(agent): 实现 CoderAgent/ReviewerAgent/ResearcherAgent 专化角色"
```

---

## Task 2: 意图路由

**Files:**
- Create: `pkg/orchestrator/router.go`
- Create: `pkg/orchestrator/router_test.go`

- [ ] **Step 2.1: 写路由测试**

```go
// pkg/orchestrator/router_test.go
package orchestrator_test

import (
	"testing"
	"mini-code/pkg/orchestrator"
)

func TestRouter_DefaultsToCoderAgent(t *testing.T) {
	r := orchestrator.NewRouter()
	agentType := r.Route("帮我实现一个函数")
	if agentType != "coder" {
		t.Errorf("expected coder, got %q", agentType)
	}
}

func TestRouter_ReviewKeywordsRouteToReviewer(t *testing.T) {
	r := orchestrator.NewRouter()
	cases := []string{
		"帮我审查这段代码",
		"review 一下 main.go",
		"检查代码质量",
		"这段代码有什么问题",
	}
	for _, msg := range cases {
		agentType := r.Route(msg)
		if agentType != "reviewer" {
			t.Errorf("msg %q: expected reviewer, got %q", msg, agentType)
		}
	}
}

func TestRouter_ResearchKeywordsRouteToResearcher(t *testing.T) {
	r := orchestrator.NewRouter()
	cases := []string{
		"调研一下 gRPC 和 REST 的区别",
		"搜索 Go 中如何实现连接池",
		"了解一下 Redis Sentinel 的工作原理",
	}
	for _, msg := range cases {
		agentType := r.Route(msg)
		if agentType != "researcher" {
			t.Errorf("msg %q: expected researcher, got %q", msg, agentType)
		}
	}
}
```

- [ ] **Step 2.2: 运行测试确认失败**

```bash
go test ./pkg/orchestrator/... -run TestRouter -v
```
预期：`undefined: orchestrator.NewRouter`

- [ ] **Step 2.3: 创建 router.go**

```go
// pkg/orchestrator/router.go
package orchestrator

import "strings"

// Router 根据用户消息内容路由到合适的 Agent 类型
type Router struct{}

func NewRouter() *Router { return &Router{} }

// reviewerKeywords 触发 ReviewerAgent 的关键词
var reviewerKeywords = []string{
	"审查", "review", "检查代码", "代码质量", "code review",
	"有什么问题", "找问题", "看看代码", "分析代码",
}

// researcherKeywords 触发 ResearcherAgent 的关键词
var researcherKeywords = []string{
	"调研", "搜索", "了解", "查找", "research",
	"怎么实现", "如何实现", "什么是", "比较", "对比",
}

// Route 返回应处理该消息的 Agent 类型名称
// 返回值："coder" | "reviewer" | "researcher"
func (r *Router) Route(message string) string {
	lower := strings.ToLower(message)

	for _, kw := range reviewerKeywords {
		if strings.Contains(lower, kw) {
			return "reviewer"
		}
	}
	for _, kw := range researcherKeywords {
		if strings.Contains(lower, kw) {
			return "researcher"
		}
	}
	return "coder" // 默认
}
```

- [ ] **Step 2.4: 运行测试**

```bash
go test ./pkg/orchestrator/... -run TestRouter -v
```
预期：全部 PASS

- [ ] **Step 2.5: Commit**

```bash
git add pkg/orchestrator/router.go pkg/orchestrator/router_test.go
git commit -m "feat(orchestrator): 实现关键词意图路由（reviewer/researcher/coder）"
```

---

## Task 3: dispatch_agent 工具

**Files:**
- Create: `pkg/tools/dispatch.go`
- Create: `pkg/tools/dispatch_test.go`
- Modify: `pkg/tools/registry.go`

- [ ] **Step 3.1: 写 dispatch 测试**

```go
// pkg/tools/dispatch_test.go
package tools_test

import (
	"encoding/json"
	"testing"
	"mini-code/pkg/tools"
)

func TestDispatchAgent_RequiresRoleAndTask(t *testing.T) {
	// 缺少必填参数时应返回错误
	args, _ := json.Marshal(map[string]interface{}{
		"role": "reviewer",
		// 缺少 task
	})
	_, err := tools.Executors["dispatch_agent"](string(args))
	if err == nil {
		t.Error("expected error for missing task")
	}
}

func TestDispatchAgent_InvalidRoleReturnsError(t *testing.T) {
	args, _ := json.Marshal(map[string]interface{}{
		"role": "unknown_role",
		"task": "do something",
	})
	_, err := tools.Executors["dispatch_agent"](string(args))
	if err == nil {
		t.Error("expected error for invalid role")
	}
}
```

- [ ] **Step 3.2: 运行测试确认失败**

```bash
go test ./pkg/tools/... -run TestDispatchAgent -v
```
预期：`undefined` 或 Executor 不存在

- [ ] **Step 3.3: 创建 dispatch.go**

```go
// pkg/tools/dispatch.go
package tools

import (
	"context"
	"fmt"
	"mini-code/pkg/agent"
	"mini-code/pkg/provider"

	"github.com/sashabaranov/go-openai"
)

// dispatchProvider 通过闭包注入，由 main.go 在启动时设置
var dispatchProvider provider.Provider
var dispatchModel string

// SetDispatchProvider 注入 Provider 和模型名（由 main.go 调用）
func SetDispatchProvider(p provider.Provider, model string) {
	dispatchProvider = p
	dispatchModel = model
}

// DispatchAgentArguments dispatch_agent 工具参数
type DispatchAgentArguments struct {
	Role string `json:"role" validate:"required" jsonschema:"required" jsonschema_description:"要调用的 Agent 角色：reviewer | researcher"`
	Task string `json:"task" validate:"required" jsonschema:"required" jsonschema_description:"交给子 Agent 的任务描述"`
}

func DispatchAgent(args interface{}) (interface{}, error) {
	var params DispatchAgentArguments
	if err := parseArgs(args, &params); err != nil {
		return nil, err
	}
	if dispatchProvider == nil {
		return nil, fmt.Errorf("dispatch provider 未初始化")
	}

	var subAgent *agent.BaseAgent
	switch params.Role {
	case "reviewer":
		subAgent = agent.NewReviewerAgent(dispatchProvider, dispatchModel)
	case "researcher":
		subAgent = agent.NewResearcherAgent(dispatchProvider, dispatchModel)
	default:
		return nil, fmt.Errorf("未知 Agent 角色 %q，支持：reviewer | researcher", params.Role)
	}

	// 构建子 Agent 的消息：必须包含 system message，才能触发 BaseAgent 的 systemPromptOverride 逻辑
	// system message 内容会被 subAgent 的 systemPromptOverride 覆盖
	messages := []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: "你是专业的 AI 助手。"},
		{Role: openai.ChatMessageRoleUser, Content: params.Task},
	}

	// 在当前 goroutine 中同步执行，不经过 Orchestrator
	reply, _, err := subAgent.Run(context.Background(), messages, nil)
	if err != nil {
		return nil, fmt.Errorf("[%s Agent] 执行失败: %w", params.Role, err)
	}
	return fmt.Sprintf("[%s Agent 结果]\n%s", params.Role, reply), nil
}
```

- [ ] **Step 3.4: 注册 dispatch_agent 工具**

在 `pkg/tools/registry.go` 的 `init()` 函数中添加：

```go
register("dispatch_agent", "调用专化子 Agent 执行子任务（reviewer 或 researcher）", DispatchAgentArguments{}, DispatchAgent)
```

- [ ] **Step 3.5: 更新 main.go 注入 dispatch provider**

在 `main.go` 创建 Provider 之后添加：

```go
tools.SetDispatchProvider(p, modelID)
```

- [ ] **Step 3.6: 运行测试**

```bash
go test ./pkg/tools/... -run TestDispatchAgent -v
go test ./... -count=1
```
预期：全部 PASS

- [ ] **Step 3.7: Commit**

```bash
git add pkg/tools/dispatch.go pkg/tools/dispatch_test.go pkg/tools/registry.go
git commit -m "feat(tools): 实现 dispatch_agent 工具，支持调起 reviewer/researcher 子 Agent"
```

---

## Task 4: Orchestrator 集成 Router

**Files:**
- Modify: `pkg/orchestrator/orchestrator.go`
- Modify: `cmd/agent/main.go`

- [ ] **Step 4.1: 更新 Orchestrator 使用 Router**

在 `Orchestrator` 结构体中添加 `router *Router` 字段，在 `New()` 中初始化：

```go
type Orchestrator struct {
	sessions   map[string]*Session
	mu         sync.Mutex
	memStore   *memory.Store
	router     *Router
}

func New(memStore *memory.Store) *Orchestrator {
	o := &Orchestrator{
		sessions: make(map[string]*Session),
		memStore: memStore,
		router:   NewRouter(),
	}
	go o.evictLoop()
	return o
}
```

- [ ] **Step 4.2: 更新 Handle 使用 AgentFactory**

修改 `Handle()` 签名，接受 AgentFactory 而不是固定的 Agent：

```go
// AgentFactory 根据类型创建 Agent
type AgentFactory func(agentType string) agent.Agent

func (o *Orchestrator) Handle(ctx context.Context, msg channel.IncomingMessage, factory AgentFactory, ch channel.Channel) error {
	session := o.GetOrCreateSession(msg.ChannelID, msg.UserID)

	// 路由到合适的 Agent 类型
	agentType := o.router.Route(msg.Text)
	session.mu.Lock()
	session.agentType = agentType
	session.mu.Unlock()

	agnt := factory(agentType)
	// ... 其余逻辑不变
}
```

- [ ] **Step 4.3: 更新 main.go 传入 AgentFactory**

```go
// main.go 中创建 AgentFactory
factory := func(agentType string) agent.Agent {
	switch agentType {
	case "reviewer":
		return agent.NewReviewerAgent(p, modelID)
	case "researcher":
		return agent.NewResearcherAgent(p, modelID)
	default:
		return agent.NewCoderAgent(p, modelID)
	}
}

// 在消息处理循环中
if err := orch.Handle(ctx, msg, factory, cliCh); err != nil {
	ui.PrintError("运行失败: %v", err)
}
```

- [ ] **Step 4.3.5: 编译验证（Handle 签名变更后）**

```bash
go build ./... 2>&1
```
预期：无报错。如果有编译错误，确认所有调用 `Handle()` 的地方（包括测试文件）都已更新为传入 `factory AgentFactory` 参数。

- [ ] **Step 4.4: 运行全部测试**

```bash
go test ./... -count=1
```
预期：全部 PASS

- [ ] **Step 4.5: 手动验收测试**

```
> 帮我审查 pkg/agent/base.go 的代码质量
（终端应显示 "[reviewer Agent]" 标识，且 Agent 只使用只读工具）

> 调研一下 Go 中的 goroutine 泄漏检测方案
（应使用 ResearcherAgent）

> 在 main.go 中添加版本号常量
（应使用 CoderAgent，有写入权限）
```

- [ ] **Step 4.6: 最终 Commit**

```bash
git add pkg/orchestrator/ cmd/agent/main.go
git commit -m "feat(phase3): 完成多 Agent 专化和意图路由"
```

---

## Phase 3 验收标准

```bash
# 1. 全部测试通过
go test ./... -count=1

# 2. 编译成功
go build ./cmd/agent

# 3. 路由测试
go test ./pkg/orchestrator/... -run TestRouter -v
# 预期：所有路由规则通过

# 4. Agent 工具集隔离测试
go test ./pkg/agent/... -run TestReviewerAgent -v
# 预期：工具集受限验证通过
```
