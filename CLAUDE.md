# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 项目简介

Mini-Code 是一个基于 Go 的本地 AI 编程助手。目前仅提供 CLI 一种运行形态（`cmd/agent`，含监督器/Worker 分进程热重启机制）。内置 20 个工具（文件 / Shell / Git / 记忆 / 网页搜索 / 子 Agent 调度）。

Go 模块路径为 `mini-code`，包导入形如 `mini-code/pkg/agent`（非 `github.com/...`）。

> `AGENTS.md` 与本文件内容互补，修改架构或环境变量时两处需同步。

## 常用命令

```bash
go build -o mini-code.exe ./cmd/agent        # 构建（Windows）
go run ./cmd/agent                           # 开发运行

go test ./...                                # 全部
go test -v ./pkg/agent                       # 某个包
go test -v ./pkg/tools -run TestWriteFile    # 单个测试函数
go test -race ./...                          # 涉及 Session / 并发改动时必跑
go test -cover ./...

gofmt -w ./cmd ./pkg
go vet ./...
```

开发机是 Windows，Linux/macOS 的沙箱代码在本机不会编译。改了 `pkg/sandbox` 或 `pkg/tools` 中带构建标签的文件后，至少做一次交叉编译检查（PowerShell）：
`$env:GOOS="linux"; go vet ./pkg/sandbox ./pkg/tools; $env:GOOS="darwin"; go vet ./pkg/sandbox ./pkg/tools; Remove-Item Env:GOOS`

## 环境配置

复制 `.env.example` 为 `.env`。`.env` 由 supervisor 和 worker 各自加载；已存在的环境变量不会被覆盖。

LLM Provider 二选一（`cmd/agent/main.go`）：
- **Pie Gateway（优先）**：`PIE_APP_ID` + `PIE_APP_SECRET` + `PIE_GATEWAY_PATH`，模型取 `MODEL`，缺省 `deepseek-v3-2`
- **OpenAI 兼容（回退）**：`API_KEY` + `BASE_URL` + `MODEL`

`PIE_*` 只用于 LLM，与 `web_search` 无关；搜索走 Exa MCP。

| 可选变量 | 作用 |
|---|---|
| `LM_LOG_LEVEL` | minimal/normal/verbose（或 0/1/2），控制 `helpers.go` 的 `currentLogLevel` |
| `LM_VERBOSE` | 非空即在 BaseAgent 启用 verbose（独立于 `LM_LOG_LEVEL`） |
| `LM_DEBUG` | 非空即写 `lm_debug.log`（已 gitignore，可能很大） |
| `LM_MAX_TURNS` | 单次任务最大工具轮次，默认 50，0=不限 |
| `LM_CONTEXT_BUDGET` | 上下文 token 预算，默认 96000 |
| `LM_SHELL_TIMEOUT` | `run_shell` 超时秒数，默认 120 |
| `LM_SHELL_STRICT` | `=1` 时 `run_shell` 只允许白名单命令 |
| `LM_SANDBOX_STRICT` | `=1` 且 OS 沙箱不可用时拒绝执行 `run_shell`（⚠️ Windows 上等于禁用 shell） |
| `LM_SANDBOX` | `=0` 显式关闭 L2 OS 沙箱（逃生舱，启动时打印提示） |
| `LM_SANDBOX_WRITABLE` | 额外沙箱可写目录（逗号分隔），默认自动取 `GOCACHE`/`GOMODCACHE`/`TMPDIR` |
| `EXA_MCP_URL` | 覆盖免费 Exa 搜索端点（默认 `https://mcp.exa.ai/mcp`，无需 API key） |

## 架构概览

```
cmd/agent/main.go        CLI 入口，承担 supervisor / worker / sandbox runner（--sandbox-run）三种角色
pkg/sandbox/             L2 OS 沙箱（Linux Landlock / macOS Seatbelt / 其他平台 none）+ audit.go 审计日志
pkg/channel/             Channel IO 接口（types.go）；cli/ 为 readline REPL
pkg/orchestrator/        Session 管理、关键词路由、上下文压缩、SessionSnapshot（热重启交接）
pkg/agent/               BaseAgent + coder/reviewer/researcher 专化预设；prompt.go 构建系统提示（含记忆注入）；context.go token 估算
pkg/memory/              两个独立 SQLite 连接（global + project），长期记忆 + 短期会话记忆
pkg/provider/            Provider 接口；openai.go / pie_gateway.go
pkg/supervisor/          TCP 控制协议（control.go）与 worker 侧客户端（worker_runtime.go）
pkg/tools/               工具注册、执行、工作区校验、shell 防护
pkg/textutil/            UTF-8 安全截断
pkg/ui/                  终端颜色、格式化、进度、Esc 监听
```

请求流：
```
CLI → Orchestrator.HandleMessage → Router 选 agent 类型
   → 追加用户消息 → compactIfNeeded（超预算时摘要压缩）
   → Agent.Run（流式 LLM → 并发工具调用 → 追加结果 → 循环）
   → 新消息写回 Session → applyPendingRestart
```

## 关键设计决策（非显然部分）

- **Supervisor/Worker 热重启**
  parent 进程作为 supervisor，监听本地 TCP，spawn 子进程 worker（`--worker/--standby/--control-addr/--worker-id/--session-snapshot`）。worker 发 `ready`，supervisor 通过 `restart_request` 拉起 standby；standby ready 后向旧 worker 发 `shutdown`、再发 `activate`，session 快照经文件传递。消息类型见 `pkg/supervisor/control.go`。

- **`mini-code restart` 是特殊拦截命令**
  `pkg/tools/restart.go` 拦截，不经 shell，调用注册的 `RestartHandler`：`go build` 到 `.mini-code/releases/<timestamp>/`，再请求 supervisor 切换。

- **工作区边界**
  worker 启动时先 `tools.SetWorkspaceRoot(".")` 固定根目录，再初始化其他组件，防止后续 chdir 导致安全边界漂移。所有文件工具经 `pkg/tools/workspace.go` 校验路径，返回中文错误（"路径超出工作区"）。

- **shell 防护不是沙箱**
  `shell_guard.go` 始终拦截破坏性命令黑名单；`LM_SHELL_STRICT=1` 改为首词白名单，但通过检查的命令仍可访问工作区外文件。真正的文件边界由下面的沙箱分层提供。

- **沙箱三层，刻意跨平台不对称**（设计见 `docs/superpowers/plans/2026-10-07-sandbox-hardening.md`）
  - L1 路径边界（`pkg/tools/workspace*.go`，全平台）：拒绝 ADS（`file.txt:stream`）、UNC、device、NUL，并做句柄级校验拦截 NTFS junction 越权。
  - L2 OS 强制（`pkg/sandbox`，仅 Linux/macOS）：沙箱起不来时**硬失败，绝不静默降级**；Windows 无可行方案（Job Objects 不限文件、AppContainer 需穷举能力），只有 L1+L3。
  - L3 审计（全平台）：每次 `run_shell` 追加 JSONL 到 `<工作区>/.mini-code/audit.log`。
  - 沙箱配置由 `main.go` 经 `tools.SetSandboxConfig` 注入；未注入时（如单测）为关闭状态，保持旧行为。
  - `sandbox_darwin.go` 未在 macOS 实测。

- **junction 的坑（改 `workspace*.go` 前必读）**
  junction 是 reparse point 而非符号链接：`os.Lstat` 的 Mode 不含 `ModeSymlink`，`EvalSymlinks` 也解析不了其内部文件，`filepath.Rel` 会把它当普通子目录放行。必须查 `FILE_ATTRIBUTE_REPARSE_POINT` 或用 `GetFinalPathNameByHandle` 取真实路径（见 `workspace_windows.go`）。

- **Landlock 必须在 runner 子进程里收紧**
  Landlock 作用于调用线程且不可逆，在主进程收紧会锁死 mini-code 自身（`~/.mini-code/memory.db`、审计日志、`.mini-code/releases/`）。因此 `sandbox.Wrap` 只把命令改写为 `mini-code --sandbox-run ...`，由该子进程收紧后再 exec；`main.go` 开头用 `sandbox.IsRunnerInvocation` 优先分流。

- **Agent 路由刻意非对称**
  `router.go` 关键词匹配（中英混合）：写入意图优先级最高，直接归 coder（唯一拥有全部工具，误判代价最低）；其次 reviewer，再 researcher，默认 coder。专化 prompt **追加**在共享 prompt 之后，不替换；reviewer（只读）/researcher 使用工具白名单（见各自 `*Tools` 变量）。

- **上下文压缩放在 Orchestrator 而非 Agent**
  Agent 无跨轮状态，压缩放进 `Run` 会每轮重复触发。默认预算 96k token，保留 system + 最近 12 条，摘要写回 Session 供下次合并；调用 LLM 失败时静默放弃压缩，不阻塞主流程。

- **BaseAgent 并发工具执行**
  同一轮多个 tool_call 并发（`maxConcurrency=5`），全部完成后进入下一轮；非流式路径（handler=nil）仅供测试。

- **记忆两层 SQLite**
  global 库 `~/.mini-code/memory.db`（user/global 长期记忆 + session_memory），project 库 `./.mini-code/project.db`（仅 project scope）；均启用 FTS5，单写连接 + WAL，`Store.migrate()` 自动建表。

- **原子文件写入**
  `atomic_write_unix.go` / `atomic_write_windows.go` 分平台实现 tmp+rename。

- **跨平台 Shell**
  `run_shell` 在 Windows 用 `cmd /C`，Unix 用 `/bin/sh -c`。

- **Session 快照可序列化**
  `SessionSnapshot`（`orchestrator/session.go`）含 channel_id、user_id、agent_type、messages，测试见 `session_snapshot_test.go`。

## 工具系统扩展

新工具：在 `pkg/tools/` 创建文件 → 定义参数结构体 → 实现 `ToolExecutor` → 在 `registry.go` 的 `init()` 调用 `register()`。
- `jsonschema:"required"` 标签：控制 function calling schema 的 required 字段
- `validate:"required"` 标签（`go-playground/validator`）：运行时校验，失败返回中文错误
- Schema 由 `invopop/jsonschema` 自动生成（`system.go` 的 `generateSchema`）
- `dispatch_agent` 的工厂由 `main.go` 注入，只接受 `reviewer`/`researcher`（`pkg/tools/dispatch.go`）
- SQLite 使用 `modernc.org/sqlite`（纯 Go，无 CGO）

## CLI 备注

- 内置命令：`help/h/?`、`exit/quit/q`、`clear/cls`、`reset/r/new/n`、`history/hist`、`version/v`
- 历史文件 `.mini-code-history`，运行时数据在 `.mini-code/`
- `LM_LOG_LEVEL=verbose` 打印完整请求/响应

## 提交规范

Conventional Commits：`feat:` / `fix:` / `docs:` / `refactor:` / `test:`，可加 scope，例如 `feat(agent): ...`。
