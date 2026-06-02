# 终端 UI 迁移到 Bubble Tea 设计稿

- 日期：2026-06-02
- 范围：CLI（`cmd/agent`）终端 UI 与输入/中断处理整体迁移到 Charm 生态
- 状态：设计已确认，待落实施计划

## 1. 背景与动机

当前 CLI 的终端交互由三套并存的模型拼成，彼此独立、难以维护：

1. **输入**：`ergochat/readline` 阻塞式 REPL（`pkg/channel/cli/runner.go`、`pkg/ui/input.go`），带历史文件与前缀补全。
2. **中断**：任务执行期间另起 goroutine，用 `golang.org/x/term` 把终端切到 raw 模式、非阻塞轮询 stdin 检测双击 Esc（`pkg/ui/esc_monitor*.go`）。
3. **输出**：全部直接 `fmt.Print` 写 stdout——`pkg/ui/markdown.go`（自研流式 markdown + chroma 高亮）、`progress.go`（spinner/进度条）、`tools.go`（工具调用展示）、`colors.go`/`format.go`；**且 agent 层 `pkg/agent/helpers.go`/`base.go` 也直接打印工具调用**，不经过任何 handler。

本次迁移的动机（四项全选）：Windows 终端健壮性、更丰富的 UI、更干净的架构、统一到 Charm 技术栈。

## 2. 关键决策

| 决策点 | 结论 | 理由 |
|---|---|---|
| 技术选型 | **只用 Bubble Tea**，不引入 tcell | bubbletea 与 tcell 是互斥的两套方案；现代 bubbletea 对 Windows 控制台支持已成熟，引入 tcell 只是多余复杂度与维护风险。配套用 `bubbles` + `lipgloss` + `glamour`。 |
| 渲染模式 | **内联 / Inline**（非 AltScreen） | 保留终端原生滚动/复制/历史，最贴近现有 REPL 手感；热重启的终端交接也比全屏简单得多。 |
| 接管程度 | **Bubble Tea 接管整个会话循环** | 真正兑现"单事件循环 + 干净架构"，删除 readline / esc_monitor / select 循环三套并存模型。 |
| Markdown | **改用 glamour**，内置 `dark` 风格 | 优先 Charm 栈一致性。接受流式期间无实时高亮的代价（见 §4.3）。 |
| 输入 | **多行**，用 `bubbles/textarea` | Enter 提交、Shift+Enter 换行。 |
| 运行期误用 `ui.Print*` 的守卫 | **降级丢弃**（非 panic） | program 激活期间若有遗漏的直接打印，静默丢弃而非崩溃。 |

**非目标（本次不动）：**
- **Telegram 频道**：不引入 bubbletea，继续 `EditMessage` 流式。本次是 CLI-only 迁移。
- **`pkg/agent/engine.go`（legacy `ClawEngine` 兼容垫片）**：只服务旧测试、不在 live 路径，保持原样，避免连锁改动。

## 3. 目标架构

### 3.1 顶层结构翻转

**现状（三套模型并存）：**
```
main.go select 循环 ←── cliCh.Messages() ←── readline 阻塞读
   └→ orch.Handle → handleCLI → renderer.fmt.Print（流式）
   └→ 任务期间另起 goroutine raw-mode 轮询 Esc
   └→ agent 层 helpers.go 直接 fmt.Print 工具调用
```

**目标（单一 Bubble Tea 事件循环）：**
```
tea.Program（拥有终端 · 内联模式）
  Model.Update:
    KeyMsg(Enter)        → 启动 tea.Cmd(goroutine): orch.Handle(...)
    KeyMsg(Shift+Enter)  → textarea 换行
    KeyMsg(Esc,Esc 1.5s) → 取消当前任务 ctx
    KeyMsg(Ctrl+D / exit)→ tea.Quit
    chunkMsg/toolMsg/doneMsg/noticeMsg → 更新状态 + tea.Println 提交滚动历史
  Model.View: 活动区 = spinner/状态行 + textarea 输入框
```

agent 在 `tea.Cmd` 派生的 goroutine 中运行，**所有输出经 `program.Send(msg)` 回到 Update**，绝不直接 `fmt.Print`。`program.Send` 是 goroutine 安全的。

### 3.2 关键重构：agent 层停止打印、改发事件

复用并补齐已有的 `StreamHandler` 接口（它本就有 `OnToolCall`/`OnToolResult`，仅缺被充分使用）：

- `pkg/agent/base.go` / `helpers.go`：`displayToolCallsStart` / `displayToolCallsResults` **删除所有 `ui.Print*`**，改为调用 `handler.OnToolCall(name, args)` / `handler.OnToolResult(name, result, err)`。`base.go` 中"等待响应..."等提示同样改发事件（或交由 spinner 状态承载）。
- **CLI 侧 handler 实现**：把 text chunk 与 tool 事件统一 `program.Send(chunkMsg / toolMsg)`。
- **Telegram 侧 handler**：忽略 tool 事件（维持现状只显示最终文本），text 走 `EditMessage`。
- **`dispatch_agent` 子 agent**：传 `nil` handler → 事件 no-op，子 agent 内部步骤保持静默（同现状）。

结果：`pkg/agent` 不再依赖 `pkg/ui` 做输出，只产出结构化事件——既消除"打印与渲染帧打架"，又真正解耦。

## 4. 组件映射

| 现有 | 替换为 | 说明 |
|---|---|---|
| `ergochat/readline` | `bubbles/textarea` | 多行输入；Enter 提交、Shift+Enter 换行。历史沿用 `.mini-code-history`，在 Model 里自维护 `↑/↓` 导航。内置命令（help/exit/clear…）做简单前缀提示，PrefixCompleter 不再需要。 |
| `pkg/ui/esc_monitor*.go` + `x/term` | Model.Update 的 `KeyMsg` | **整个包删除**。双击 Esc 改为 Model 里 `lastEsc time` 字段 + 1.5s 窗口判定 → 取消任务 ctx。`nonBlockingRead`、`esc_monitor_unix/windows.go` 一并移除。 |
| `pkg/ui/progress.go` 自研 Spinner | `bubbles/spinner` | 任务期间活动区显示 spinner + 当前状态。`ProgressBar`/`MultiProgress` 若确认无调用方则删除。 |
| `pkg/ui/markdown.go` + `highlight.go` + `theme.go` | `glamour`（内置 `dark`） | 见 §4.3 流式策略。chroma 由 glamour 间接依赖，仍可保留高亮能力。 |
| `pkg/ui/colors.go`（`fatih/color`）+ `format.go` + `tools.go` 打印 | `lipgloss` 样式 + 事件 | 颜色/图标/标签用 lipgloss 定义；打印函数运行期改走事件路由（§4.1）。 |
| `pkg/channel/cli/runner.go`（阻塞 readline） | bubbletea 版 `CLIChannel` | 持有 `*tea.Program`；`Send/EditMessage/NotifyDone/SendFile` 改为 `program.Send(对应 msg)`；`Start()` 改为 `program.Run()`。 |

### 4.1 残留 `ui.Print*` 的路由

- **program 启动前 / 退出后**（欢迎横幅、致命错误、信号退出）：保持普通 `fmt.Print`，无冲突。
- **program 运行期间**（任务中提示/错误、内置命令输出）：**不直接打印**，统一改走 `program.Send(noticeMsg{level,text})` → Update 里 `tea.Println` / `tea.Printf`。
- **运行期守卫**：`ui` 包加一个"program 是否激活"标志；激活期间若仍有人调用 `ui.Print*`，**静默降级丢弃**（不 panic），避免破坏渲染帧。用于兜底漏网的直接打印调用点。

### 4.2 热重启终端交接

内联模式下显著简化，但仍需保证时序：worker 收到 restart 请求时，**先让 bubbletea 优雅退出并恢复终端状态**（`p.Quit()` + 等 `p.Wait()`，bubbletea 自动 restore），**之后**再把 stdin/stdout 交接给新 worker（exec）。需调整 `RestartHandler` / `applyPendingRestart` 的触发时机：在 program 退出后再执行交接。`SessionSnapshot` 机制不变。

### 4.3 glamour 流式策略（已知取舍）

glamour 是整文档渲染，无法逐 token 增量高亮。采用通行做法：
1. 流式期间，活动区显示**纯文本**增量（即时反馈，无高亮）。
2. `doneMsg` 到达后，用 glamour 渲染整段回复 → `tea.Println` 提交进滚动历史。

代价：流式过程中无实时语法高亮；最终呈现是干净的 glamour 效果。

## 5. 测试策略

- **Model 交互测试**：用 `teatest`（charm 官方）喂 `KeyMsg`、断言 View 输出与提交内容。
- **agent 层**：改成发事件后更易测——用 fake `StreamHandler` 断言 `OnToolCall/OnToolResult` 被正确调用，替换掉当前难测的"打印到 stdout"。
- **回归保护**：保留现有 `pkg/agent`、`pkg/orchestrator` 测试；`engine.go` 兼容垫片测试不动。

## 6. 实施阶段（粗粒度，细节交由实施计划）

1. 引入依赖：`bubbletea`、`bubbles`、`lipgloss`、`glamour`；建立 `pkg/ui` 的 lipgloss 样式与事件消息类型。
2. `StreamHandler` 接口补齐 text 事件；`base.go`/`helpers.go` 改为发事件、移除 `ui.Print*`。
3. 新建 bubbletea Model（textarea 输入、spinner、双击 Esc、历史导航、内置命令）。
4. 重写 `CLIChannel` 持有 `*tea.Program`；`orchestrator.handleCLI` 改为经事件流。
5. 删除 `esc_monitor*.go`、`input.go`(readline 部分)、自研 `markdown.go`/`highlight.go`/`progress.go` 中被取代的部分；接入 glamour。
6. 处理热重启交接时序。
7. `ui` 包加运行期降级守卫；扫净残留运行期 `ui.Print*`。
8. 补 `teatest` 与 fake-handler 测试。

## 7. 主要风险

- **运行期直接打印的漏网点**：分布广（main/orchestrator/agent），靠运行期守卫 + 逐个排查兜底。
- **热重启时序**：必须在 program 退出后再交接终端，否则终端状态错乱；需重点验证 Windows。
- **textarea 多行 + Enter 提交的键位**：Enter 提交、Shift+Enter 换行的判定需在 Update 里正确区分（部分终端对 Shift+Enter 支持差，需回退方案，如以单独修饰键或 `Alt+Enter` 兜底）。
- **glamour 在窄终端/中文宽字符下的换行**：需验证 CJK 宽度与 lipgloss 宽度计算一致。
