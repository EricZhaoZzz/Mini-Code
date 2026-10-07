# Mini-Code 日志系统重构设计

**日期：** 2026-04-19
**状态：** 待实施
**目标：** 清理日志相关重复代码与死代码、引入基于 `log/slog` 的统一 logger 接口、将终端 UI 展示与诊断日志在职责上彻底分离。

---

## 1. 背景与现状问题

仓库调研（`pkg/agent/helpers.go`、`pkg/ui/tools.go`、`pkg/agent/base.go`、`pkg/agent/engine.go`、`pkg/channel/telegram/telegram.go`、`pkg/supervisor/`、`.env.example` 等）显示以下问题：

1. **两套重复且互不同步的 `LogLevel`**
   - `pkg/agent/helpers.go:30-51` 定义了 `LogLevel/LogLevelMinimal/Normal/Verbose` + 全局 `currentLogLevel`，并在 `init()` 中读取 `LM_LOG_LEVEL` 环境变量。
   - `pkg/ui/tools.go:160-175` **又定义了一模一样的一套**，且导出 `SetLogLevel`。但整个仓库没有任何地方调用 `ui.SetLogLevel(...)`，意味着 `pkg/ui/tools.go:170` 的 `currentLogLevel` 恒为 `Normal`，它在 `tools.go:57` 守护的 `LogLevelVerbose` 分支是**死分支**。

2. **新 BaseAgent 路径丢失了 debug 日志**
   `LM_DEBUG=1` → `lm_debug.log` 由 `pkg/agent/helpers.go:53-65` 的 `newDebugLogger()` 写入，调用点 `debugLog("LM INPUT", req)` / `"LM OUTPUT"` 仅存在于 legacy 兼容层 `pkg/agent/engine.go:124/130/180/192`。新的 `pkg/agent/base.go:22` 虽持有 `debugLogger *log.Logger` 字段并在 `base.go:45` 初始化，但**无任何调用**——新执行路径的 debug 日志在重构后已悄悄消失。

3. **`LM_LOG_LEVEL` 变量语义模糊**
   名称暗示"日志级别"，实际只被 `pkg/ui/tools.go:57` 的 UI 详细度分支检查；不是真正意义上贯穿所有模块的日志级别。值域 `minimal/normal/verbose` 与后续引入的"日志"概念不对齐。

4. **诊断类打印散落且不受统一控制**
   - `pkg/channel/telegram/telegram.go:139-142` 用 `os.Getenv("TELEGRAM_DEBUG")` 判断并 `fmt.Printf` 直接落到 stdout。
   - `pkg/agent/helpers.go:60` 在无法打开 `lm_debug.log` 时 ad-hoc `fmt.Fprintf(os.Stderr, "[debug] ...")`。
   - `pkg/supervisor/` 整个包（热重启控制协议 `SwitchCoordinator`、worker runtime）**零日志调用**，`ready/shutdown/activate/快照交接` 这些关键事件无任何观测点。

5. **UI 层的展示打印和"日志"没被区分**
   `pkg/ui/colors.go / tools.go / progress.go` 大量 `fmt.Printf` 直接写 stdout。对 CLI 前端是正确的（给用户看）；但在 Telegram 场景下 bot 没有交互终端，这些打印漏到控制台，且与运维排查需要的"诊断日志"混在一起。

## 2. 设计目标

重构优先解决三件事（与需求澄清结果一致）：

1. **清理重复与死代码**：消除两套 `LogLevel`、死的 `SetLogLevel`、`pkg/ui/tools.go` 的 verbose 死分支、BaseAgent 上孤立的 `debugLogger` 字段。
2. **统一 logger 接口**：全仓诊断类输出通过一个入口，而不是散落的 `fmt.Printf` / `log.Logger` / ad-hoc 开关。
3. **分离 UI 展示与日志**：`pkg/ui` 只负责 CLI 终端展示（stdout），新 `pkg/log` 只负责诊断日志（stderr），两者职责正交。

附带纳入的范围（成本低、价值大）：

4. 把 LLM 请求/响应（原本依赖 `LM_DEBUG`）接回到 Debug 级日志，按字段长度而不是原文记录。
5. 把 `TELEGRAM_DEBUG` 的 printf 迁到 `log.For("telegram").Debug(...)`，废弃该环境变量。
6. 给 `pkg/supervisor` 的 ready/shutdown/activate/快照交接加 Info 级观测点。

**明确不做**（Phase 2 候选）：sink 注入（bot 把 error 推给管理员 chat）、按 session 的 context 字段注入、工具执行性能直方图、JSON 输出模式、文件轮转。

## 3. 方案选型

**采用方案 A — 薄封装 `pkg/log`**：`pkg/log` 基于 `log/slog`（Go 1.21+，go.mod 已 `go 1.26.1`）提供最小集合 `Setup / Default / For / SetOutput`，其他包直接使用 `*slog.Logger` 原生 API（`Debug/Info/Warn/Error/With/DebugContext/...`）。

对比方案 B（自定义 `Logger` 接口 + slog 适配）：违反 YAGNI，当前没有切换底层的诉求，`*slog.Logger` 本身就是合适的抽象层。
对比方案 C（在 A 基础上加 `log.FromContext(ctx)`）：能力有价值但属于 Phase 2——此次重构不含"按 session 追踪"诉求，不引入。

## 4. 架构与模块边界

### 4.1 新增

- `pkg/log/` 薄封装 slog：
  - `log.go`：`Setup / Default / For / SetOutput` 公开函数与全局默认 logger
  - `level.go`：级别解析与旧值 / `LM_DEBUG` / `TELEGRAM_DEBUG` 废弃 warning
  - `handler.go`：`prettyHandler`（tty 彩色 / 管道纯文本两种分支）
  - `log_test.go`：单元测试

### 4.2 删除

| 位置 | 内容 |
|------|------|
| `pkg/agent/helpers.go:30-51` | `LogLevel/LogLevelMinimal/Normal/Verbose/currentLogLevel/init()` |
| `pkg/agent/helpers.go:53-65` | `newDebugLogger()` + `lm_debug.log` 写文件逻辑 |
| `pkg/agent/helpers.go:217-224` | `(*BaseAgent).debugLog(tag, v)` 方法 |
| `pkg/agent/base.go:22, :45` | `debugLogger *log.Logger` 字段及其初始化 |
| `pkg/ui/tools.go:160-175` | 重复且未被注入的 `LogLevel/currentLogLevel/SetLogLevel` |
| `pkg/ui/tools.go:57-64` | `if currentLogLevel >= LogLevelVerbose` 打印参数 JSON 的死分支 |

### 4.3 不动

- `pkg/ui/` 的 `PrintSuccess/Error/Warning/Info/Tool/ToolResult/Header/Stream/Spinner/Progress` 等全部 `fmt.Printf` 路径保持不变：它们是 **CLI 终端 UI 展示**，不是日志。
- Channel / Agent / 工具注册机制不涉及。
- 记忆系统、session 快照、热重启控制协议线上行为不变（只是增加 Info 级观测日志）。

### 4.4 UI 与日志的职责分离

| 通道 | 去向 | 目的 | 控制 |
|------|------|------|------|
| UI 展示（`pkg/ui.Print*` 等） | stdout | 给 CLI 前端用户看 | 颜色/icon 取决于 tty |
| 日志（`pkg/log.*`） | stderr | 给运维/开发者排查 | `LM_LOG_LEVEL` 级别过滤 |

Telegram 运行时 stdout 没人看也无影响；stderr 仍能捕获诊断信息。CLI 下 stderr 与 stdout 并排显示，用户如果不喜欢可以自行 `2>out.log` 重定向。

## 5. `pkg/log` API 形状

```go
package log

import (
    "io"
    "log/slog"
)

// Setup 初始化全局默认 logger。level 支持：
//   debug | info | warn | error （推荐）
//   minimal | normal | verbose  （兼容，首次出现 deprecation warning）
//   0 | 1 | 2                    （兼容）
//   "" 未设置或未识别 → info
func Setup(level string)

// Default 返回全局默认 logger；未 Setup 过时惰性初始化为 info/stderr。
func Default() *slog.Logger

// For 返回附带 module 字段的子 logger：Default().With("module", name)。
func For(module string) *slog.Logger

// SetOutput 替换 handler 的 writer（默认 os.Stderr），仅供测试使用。
func SetOutput(w io.Writer)
```

**调用风格示例**

```go
log.For("agent").Debug("llm request", "model", cfg.Model, "messages", len(msgs))
log.For("telegram").Debug("incoming", "chat_id", chatID, "user_id", userID)
log.For("supervisor").Info("worker activated", "pid", pid, "role", "active")
log.For("memory").Warn("scope invalid, fallback to project", "scope", raw)
```

**`prettyHandler` 输出格式**

```
15:04:05.123 DEBUG [agent]      llm request  model=gpt-4 messages=12
15:04:06.456 INFO  [supervisor] worker activated  pid=4821 role=active
15:04:07.789 WARN  [memory]     scope invalid, fallback to project  scope=team
```

实现要点：

- 有 tty 时按级别着色：Debug=dim、Info=默认、Warn=黄、Error=红；`module` 字段统一色。
- 检测到 `NO_COLOR` 环境变量或目标非 tty 时降级为纯文本。
- 字段值含空格或特殊字符时用 `%q` 加引号。
- **不输出 JSON**——这是给人看的。Phase 2 真需要机器解析时直接替换为 `slog.NewJSONHandler` 即可。

**线程安全**

- 全局默认 logger 指针由 `atomic.Pointer[slog.Logger]` 守护，`Setup` 替换时无锁。
- `For(module)` 每次调用返回一个新子 logger，不做缓存；slog 内部 `With` 已足够轻量。
- handler 对 `io.Writer` 的写入依赖 OS `write(2)` 行级原子性，不额外加 mutex（每条日志一次 `Write` 调用）。

## 6. `LM_LOG_LEVEL` 新语义与兼容

### 6.1 推荐值（新标准）

| 值 | slog.Level | 典型内容 |
|----|-----------|---------|
| `debug` | Debug | LLM 请求/响应字段、telegram 收包、工具执行明细 |
| `info`（默认） | Info | supervisor 生命周期、session 路由切换、关键业务事件 |
| `warn` | Warn | 可恢复异常（记忆写失败 fallback、输入被截断） |
| `error` | Error | 未处理错误、工具崩溃、流式中断原因 |

### 6.2 旧值兼容映射

| 旧值 | 映射到 | 说明 |
|------|-------|------|
| `minimal` / `0` | `warn` | |
| `normal` / `1` | `info` | |
| `verbose` / `2` | `debug` | |
| `""` 或未设置 | `info` | 无 warning |
| 未识别字符串 | `info` | 一次性 warning：`[log] unrecognized LM_LOG_LEVEL=xxx, using info` |

`Setup` 首次解析到 `minimal/normal/verbose` 时向 stderr 打一行 deprecation warning，例如：

```
[log] LM_LOG_LEVEL=verbose is deprecated, use "debug" instead
```

使用 `sync.Once`（按具体值去重）保证同一旧值最多只提示一次，避免刷屏。

### 6.3 废弃环境变量

| 变量 | 处理 |
|------|------|
| `LM_DEBUG` | 废弃。`Setup` 启动时若检测到 `LM_DEBUG` 非空，发一次性 warning：`[log] LM_DEBUG is no longer supported, set LM_LOG_LEVEL=debug instead`；不再生成 `lm_debug.log`。 |
| `TELEGRAM_DEBUG` | 废弃。同样逻辑的 warning：`[log] TELEGRAM_DEBUG is no longer supported, set LM_LOG_LEVEL=debug instead`。 |

### 6.4 文档同步

下列文件在本次重构中必须与代码同步修改：

- `.env.example` 第 10-16 行移除 `LM_DEBUG` 段落；第 32-34 行移除 `TELEGRAM_DEBUG` 段落；重写 `LM_LOG_LEVEL` 注释为新四档。
- `CLAUDE.md` 第 43-44 行与第 138-140 行（调试章节）。
- `README.md` 与 `README_EN.md` 的环境变量表与调试说明。
- `AGENTS.md` 第 66-67 行与第 172 行。
- `pkg/ui/format.go:349-350` 的 help panel 环境变量说明。

## 7. 各模块接入要点

### 7.1 入口初始化

- `cmd/agent/main.go`：在 `godotenv.Load` 之后、任何 agent / supervisor 逻辑之前调用 `log.Setup(os.Getenv("LM_LOG_LEVEL"))`。supervisor 角色与 worker 角色两条启动路径**各调一次**（worker 子进程通过环境继承，调一次即生效）。
- `cmd/telegram/main.go`：同上。

### 7.2 `pkg/agent`

- `base.go:22` 删除 `debugLogger *log.Logger` 字段；`base.go:45` 的初始化同步删除。
- `helpers.go` 的 `LogLevel` 类型、常量、`currentLogLevel`、`init()`、`newDebugLogger`、`debugLog` 整块删除。
- 在流式调用入口与结尾追加 Debug 日志：

  ```go
  lg := log.For("agent").With("name", a.Name())
  lg.Debug("llm request",
      "model", cfg.Model,
      "messages", len(msgs),
      "tools", len(toolDefs))
  // ... consumeStream ...
  lg.Debug("llm response",
      "reply_len", len(reply),
      "tool_calls", len(toolCalls),
      "finish_reason", finishReason)
  ```

  **不打印 messages / prompt / reply 原文**，避免把用户数据或潜在敏感信息泄到 stderr；需要查内容时走 Phase 2 的 trace 机制。

### 7.3 `pkg/agent/engine.go`（legacy 垫片）

- `engine.go:124, 130, 180, 192` 原有 `e.debugLog("LM INPUT", req)` / `"LM OUTPUT", resp)` 改为 `log.For("agent.legacy").Debug("llm input", ...)` / `"llm output"`，保持 `engine_test.go` 通过。
- `engine_test.go:94, 162, 239` 的 `engine.debugLogger = nil` 行删除（字段已不存在）。

### 7.4 `pkg/channel/telegram`

`telegram.go:139-142` 的 `TELEGRAM_DEBUG` 分支替换为：

```go
log.For("telegram").Debug("incoming message",
    "chat_id", chatID,
    "user_id", userID,
    "username", msg.From.UserName)
```

级别过滤由 slog 统一完成，无需再检查 `os.Getenv`。

### 7.5 `pkg/supervisor`（新增 Info 观测点）

- `control.go`（`SwitchCoordinator`）：worker connect、收到 `ready`、下发 `shutdown`、下发 `activate`、session 快照写入/读取、新旧 worker 交接完成。
- `worker_runtime.go`：worker 向 supervisor 发 `ready`、收到 `shutdown` / `activate`、从快照恢复 session。
- 字段约定：`role=active|standby|supervisor`、`pid`、`control_addr`、`snapshot_path`。
- 不打印 session 消息正文（含用户数据）。

### 7.6 `pkg/memory`

现有几处 fallback（scope 无效、DB 写入失败）原本静默返回 error，对应位置补 `log.For("memory").Warn(...)` 一行用于事后排查；**不改变** error 返回值与控制流。

### 7.7 `cmd/agent/main.go` 的 stderr 转发

`cmd/agent/main.go:492` 附近 `cmd.Stderr = os.Stderr` 这条线（supervisor spawn worker 时把 worker stderr 接到自己 stderr）保持不变——正好让 worker 的 `pkg/log` 输出自然汇入同一股 stderr。

### 7.8 通用原则

- 只在"**分支判断点**"和"**外部交互前后**"记录，不在热路径每次调用都打。
- 永远不打 API key、用户 prompt 原文、文件内容、工具 arguments 原文。
- `module` 字段限定白名单：`agent / agent.legacy / telegram / supervisor / memory / orchestrator / log`。新增模块需更新设计文档。

## 8. 错误处理与敏感信息

### 8.1 日志自身不能反噬业务

- `pkg/log` 所有函数**绝不 panic**：`Setup` 解析失败降级到 info；`SetOutput(nil)` 保持原 writer。
- handler 写 stderr 若返回 error（管道被关、磁盘满）吞掉不向上传递；`slog.Handler.Handle` 始终返回 nil。
- 禁止在任何业务路径用 `log.Fatal*` 风格退出；退出由 `main` 显式决定。

### 8.2 字段白名单（允许列表，不是黑名单）

只允许在日志字段中出现以下类别的值：

- **计数 / 长度**：`messages`, `reply_len`, `tool_calls`, `files`, `elapsed_ms`
- **枚举 / 类型**：`model`, `role`, `agent`, `scope`, `finish_reason`
- **标识符**：`chat_id`, `user_id`, `session_id`, `pid`, `snapshot_path`, `control_addr`
- **错误**：`err`（即 `error.Error()` 字符串；调用方负责不把 token / 密钥拼进 error message）

**严禁直接传入**：`messages` / `prompt` / `content` / `body` / `args` / `env` / `headers` / `api_key` 等原始载荷。Code review 把这条作为红线检查。

Telegram 的 `msg.From.UserName` 算用户可见标识而非秘密，保留在 Debug；如后续隐私要求提高可再收紧为仅 `user_id`。

### 8.3 热重启 stderr 合流

Supervisor 与两个 worker 同进程组写同一个 stderr，`prettyHandler` 输出按行交错（每条日志一次 `Write`，不会行内混乱）。为追溯来源，所有 supervisor / worker 日志统一携带：

- `role=active | standby | supervisor`
- `pid=<int>`

不引入文件分流——与第 4.4 节的"stderr-only"决定一致。

### 8.4 panic 处理

- `pkg/log` 自身不 recover 其他代码的 panic。
- 现有已 recover 的 goroutine（工具并发执行、telegram 长轮询、supervisor control accept loop 等）原有 recover 分支内改写为：
  ```go
  log.For(module).Error("panic", "err", r, "stack", string(debug.Stack()))
  ```
- 没有 recover 的代码路径**保持原样**，不借本次重构新增 panic 兜底。

### 8.5 并发安全

- 全局 `*slog.Logger` 指针由 `atomic.Pointer[slog.Logger]` 守护；`Setup` 替换时无锁无竞争。
- `For(module)` 不做缓存；`slog.Logger.With` 内部已足够轻量，有实测性能问题再引 `sync.Map`。
- handler 对 `io.Writer` 的写入不加 mutex（依赖 OS 行级原子性）。

## 9. 测试策略

### 9.1 `pkg/log` 单元测试（`pkg/log/log_test.go`）

| 测试 | 验证点 |
|------|--------|
| `TestSetup_ParseLevel` | `debug/info/warn/error` 及大小写不敏感解析 |
| `TestSetup_LegacyValues` | `minimal→warn, normal→info, verbose→debug`；`0/1/2` 同理 |
| `TestSetup_DeprecationOnce` | 连续两次 `verbose`，deprecation warning 只出现一次 |
| `TestSetup_UnknownValue` | `"trace"` → info + 一次性 warning |
| `TestSetup_Empty` | 空字符串 → info，无 warning |
| `TestSetup_LMDebugWarning` | 预设 `LM_DEBUG=1` 再 Setup → 发 "no longer supported" warning |
| `TestSetup_TelegramDebugWarning` | 预设 `TELEGRAM_DEBUG=1` 再 Setup → 同上 warning |
| `TestFor_ModuleField` | `For("agent").Debug("x")` 输出含 `[agent]` |
| `TestPrettyHandler_NoColor` | 设 `NO_COLOR=1` → 输出无 ANSI 转义 |
| `TestPrettyHandler_Format` | 格式为 `时间 级别 [module] 消息 kv...`；含空格值用 `%q` |
| `TestDefault_LazyInit` | 不调 `Setup` 直接 `Default().Info(...)` 不 panic，级别为 info |
| `TestConcurrentSetup` | 并发 `Setup` + 日志写入，`-race` 无报告 |

所有测试通过 `SetOutput(&bytes.Buffer{})` 捕获输出做断言；`t.Cleanup` 恢复 stderr 与环境变量。

### 9.2 既有测试的调整

- `pkg/agent/engine_test.go:94/162/239` 删除 `engine.debugLogger = nil` 行（字段不存在）。
- 全仓 `grep -r "LogLevel\|currentLogLevel\|SetLogLevel\|newDebugLogger\|debugLog\|debugLogger"` 确认无残留引用后 `go build ./...` + `go vet ./...` + `go test ./...` 必须全绿。
- 若发现测试依赖"`LM_DEBUG=1` 则写 `lm_debug.log`"的行为（预期：实际不存在），一律删除。

### 9.3 集成手工验证清单

1. `LM_LOG_LEVEL=debug go run ./cmd/agent` 问一个简单问题：stderr 看到 `[agent] llm request` / `llm response`，且**不含** messages 原文。
2. `LM_LOG_LEVEL=info go run ./cmd/agent` 同一问题：stderr 不含任何 Debug 行。
3. 未设 `LM_LOG_LEVEL` 且设 `LM_DEBUG=1`：启动 stderr 出现 `"LM_DEBUG is no longer supported"` warning；工作目录下**不**生成 `lm_debug.log`。
4. `LM_LOG_LEVEL=verbose`：启动有 deprecation warning，级别实际为 debug。
5. `LM_LOG_LEVEL=debug go run ./cmd/telegram` 发一条消息：stderr 有 `[telegram] incoming message chat_id=... user_id=...`。
6. 触发 supervisor 热重启（按 `cmd/agent/main.go` 既有升级路径）：stderr 有 `[supervisor] worker ready/activated/shutdown` 若干 Info 条目。
7. CLI 终端 UI 表现与重构前**完全一致**——工具调用图标、耗时、spinner、颜色视觉 diff 为零。
8. `go test ./... -race` 全绿；`gofmt -l .` 无输出；`go vet ./...` 无输出。

### 9.4 体积 / 依赖检查

- 引入 `log/slog` 不增加任何第三方依赖（标准库）。
- `pkg/log` 代码量预计 ≤200 行（含测试前）。

## 10. 迁移与破坏性变化

### 10.1 用户可感知的破坏

| 变化 | 影响 | 应对 |
|------|------|------|
| `LM_DEBUG` 失效 | `.env` 里 `LM_DEBUG=1` 不再写 `lm_debug.log` | 启动一次性 warning 指引改用 `LM_LOG_LEVEL=debug` |
| `TELEGRAM_DEBUG` 失效 | 控制台不再 print user_id | 同上 warning |
| 不再生成 `lm_debug.log` | 查该文件的习惯失效 | 新行为是实时 stderr；如需落盘由用户自行 `2>out.log` |

### 10.2 兼容保留

- `LM_LOG_LEVEL` 旧值 `minimal/normal/verbose` + `0/1/2` 照常工作，带一次性 deprecation warning。
- `pkg/ui` 所有 `PrintXxx` 的签名与输出零变化，CLI 视觉零变化。
- Channel / Agent / 工具注册 / 记忆 / session 快照 / 热重启协议行为不变。

### 10.3 代码层面破坏（项目内部）

- `pkg/agent.LogLevel`、`LogLevelMinimal/Normal/Verbose` 导出类型**删除** —— 仓库内 `grep` 确认无外部引用（仅 helpers.go 自用）。
- `pkg/ui.LogLevel`、`pkg/ui.SetLogLevel` 删除 —— 确认无引用（"死代码"的直接证据）。
- `pkg/agent.BaseAgent.debugLogger` 未导出字段删除 —— 零外部影响。

### 10.4 明确不在本次范围（Phase 2 候选）

- 日志 sink 注入（Telegram bot 把 Error 推给管理员 chat）
- 按 session / request 的 context 字段注入（`log.FromContext(ctx)`）
- 工具执行性能直方图（`elapsed_ms` 分位数）
- JSON 输出模式（接采集系统）
- 文件轮转（stderr 同时 tee 到文件）

本次只做"清理 + 统一 + 分离"的最小闭环，上述能力留待真正有需求时叠加。

### 10.5 回滚策略

分拆为若干独立 commit，保证任一步回滚不影响其他步骤：

1. `feat(log): 新增 pkg/log 及单元测试`（纯新增，不触碰其他包）
2. `refactor(agent|telegram|supervisor|memory|cmd): 接入 pkg/log`（增加调用点 + 入口 Setup）
3. `refactor(agent|ui): 删除旧 LogLevel / debugLogger / SetLogLevel`（清理）
4. `docs: 同步 .env.example/README/CLAUDE.md/AGENTS.md/ui.format`（文档）

任何一步出现问题都可独立回滚；哪怕保留 `pkg/log` 但还原旧 `LogLevel`，CLI 也仍能跑。

## 11. 全景目录树速览

```
pkg/log/                              新增
  log.go                                Setup / Default / For / SetOutput
  level.go                              级别解析 + 旧值兼容 + LM_DEBUG/TELEGRAM_DEBUG 废弃 warning
  handler.go                            prettyHandler（tty 彩色 / 管道纯文本）
  log_test.go                           单元测试

pkg/agent/                            清理 + 接入
  helpers.go                            删 LogLevel 枚举 / newDebugLogger / debugLog
  base.go                               删 debugLogger 字段；流式调用前后加 Debug
  engine.go                             legacy debugLog → log.For("agent.legacy").Debug
  engine_test.go                        删 debugLogger = nil 行

pkg/ui/                               清理
  tools.go                              删重复 LogLevel / SetLogLevel / verbose 死分支

pkg/channel/telegram/telegram.go      TELEGRAM_DEBUG → log.For("telegram").Debug
pkg/supervisor/                       新增 Info 观测点（ready/activate/shutdown/snapshot）
pkg/memory/                           fallback 路径补 Warn 日志
cmd/agent/main.go                     启动时 log.Setup
cmd/telegram/main.go                  同上

.env.example                          移除 LM_DEBUG / TELEGRAM_DEBUG；更新 LM_LOG_LEVEL 注释
CLAUDE.md                             调试章节更新
README.md / README_EN.md              变量表 & 示例同步
AGENTS.md                             调试/变量段落同步
pkg/ui/format.go                      help panel 环境变量说明同步
```

---

## 12. 验收标准

- [ ] `grep -r "LogLevel\|currentLogLevel\|SetLogLevel\|newDebugLogger\|debugLog\|debugLogger" cmd pkg` 无输出（旧枚举、死代码、旧字段与方法全部清除）。
- [ ] `grep -r "LM_DEBUG\|TELEGRAM_DEBUG" cmd pkg` 仅在 `pkg/log/level.go` 中出现（作为废弃 warning 的匹配字符串）。
- [ ] `lm_debug.log` 不再被生成（对应工作目录下 `ls lm_debug.log` 应为不存在）。
- [ ] `go build ./...`、`go vet ./...`、`go test ./... -race`、`gofmt -l .` 均无输出/全绿。
- [ ] 第 9.3 节集成验证清单 1-8 人工逐项通过。
- [ ] `.env.example` / `README*.md` / `CLAUDE.md` / `AGENTS.md` / `pkg/ui/format.go` 均已同步更新。
