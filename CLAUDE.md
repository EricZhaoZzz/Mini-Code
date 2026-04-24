# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 项目简介

Mini-Code 是一个基于 Go 的本地 AI 编程助手，通过 OpenAI 兼容接口与 LLM 交互。提供两种运行形态：CLI（`cmd/agent`，含监督器/Worker 分进程热重启机制）与 Telegram Bot（`cmd/telegram`）。内置 19 个工具（文件 / Shell / Git / 记忆 / 子 Agent 调度）。

Go 模块路径为 `mini-code`，包导入形如 `mini-code/pkg/agent`（非 `github.com/...`）。

## 常用命令

```bash
# 构建
go build -o mini-code.exe ./cmd/agent        # CLI（Windows）
go build -o mini-code-telegram.exe ./cmd/telegram

# 运行（开发模式）
go run ./cmd/agent
go run ./cmd/telegram

# 测试
go test ./...                                      # 全部
go test -v ./pkg/agent                             # 某个包
go test -v ./pkg/tools -run TestWriteFile          # 单个测试函数
go test -cover ./...                               # 覆盖率

# 代码检查
gofmt -w ./cmd ./pkg
go vet ./...
```

## 环境配置

复制 `.env.example` 为 `.env`：

```env
API_KEY=<your-api-key>
BASE_URL=https://api.openai.com/v1
MODEL=gpt-4

# 可选
LM_LOG_LEVEL=normal    # minimal|normal|verbose（控制 helpers.go 中的 currentLogLevel）
LM_VERBOSE=1           # 在 BaseAgent 上启用 verbose 模式（独立于 LM_LOG_LEVEL）
LM_DEBUG=true          # 调试日志写入 lm_debug.log
LM_MAX_TURNS=50        # 单次任务最大工具轮次（0=不限）

# Telegram（仅 cmd/telegram 使用）
TELEGRAM_BOT_TOKEN=...
TELEGRAM_ALLOWED_USERS=123,456   # 为空则允许所有用户
```

## 架构概览（分层）

```
cmd/agent/main.go        CLI 入口，同时承担 supervisor 与 worker 双角色
cmd/telegram/main.go     Telegram Bot 入口

pkg/channel/             统一 IO 抽象（types.go 定义 Channel 接口）
  cli/                     CLI 频道（readline REPL）
  telegram/                Telegram Bot 频道（流式 EditMessage）

pkg/orchestrator/        会话编排
  orchestrator.go          Session 管理，消息路由入口
  router.go                基于中英文关键词路由到 Coder/Reviewer/Researcher
  session.go               Session 状态 + SessionSnapshot（用于热重启）

pkg/agent/               Agent 实现
  agent.go                 Agent 接口定义（Run + Name + AllowedTools）
  base.go                  BaseAgent：流式调用、并发工具调度、工具过滤
  coder.go / reviewer.go / researcher.go   专化 Agent（覆盖 system prompt 与允许工具集）
  prompt.go                系统提示构建（含记忆注入）
  engine.go                legacy 兼容层（ClawEngine = BaseAgent 别名）

pkg/memory/              记忆系统（两个独立 SQLite 连接）
  store.go                 globalDB + projectDB
  longterm.go              长期记忆（FTS5 全文检索，scope: project/user/global）
  session.go               短期会话记忆（带过期时间）
  prompt.go                将相关记忆注入 system prompt

pkg/supervisor/          热重启控制协议（仅 cmd/agent 使用）
  control.go               SwitchCoordinator：TCP 控制通道，协调 active/standby worker
  worker_runtime.go        Worker 侧客户端

pkg/provider/            LLM 客户端封装（OpenAI 兼容）
pkg/tools/               工具注册与执行（含 atomic_write、workspace 校验、dispatch_agent）
pkg/textutil/            UTF-8 安全截断等文本工具
pkg/ui/                  终端颜色、格式化、进度展示
```

**请求流：**
```
Channel → Orchestrator.HandleMessage → Router 选择 Agent 类型
   → Session.Messages（带记忆的 system prompt）
   → Agent.Run（流式 LLM → 并发执行工具 → 追加到消息 → 继续循环）
   → StreamHandler 回调推送到 Channel
```

## 关键设计决策（非显然部分）

- **Supervisor/Worker 分进程热重启（`cmd/agent`）**
  CLI 启动时 parent 进程作为 supervisor，spawn 子进程作为 worker（通过 `--worker/--standby/--control-addr/--session-snapshot` 标志）。升级场景：新 standby worker 启动 → 通过 TCP 控制通道发 `ready` → supervisor 发 `shutdown` 给旧 worker、再发 `activate` 给新 worker、session 快照通过 `--session-snapshot` 文件传递。消息类型见 `pkg/supervisor/control.go`。

- **Agent 专化 + 关键词路由**
  `orchestrator/router.go` 用简单关键词匹配（中英混合，如「审查」「review」「调研」「research」）将消息路由到 `coder`（默认）/`reviewer`/`researcher`。专化 Agent 通过 `systemPromptOverride` 追加在共享 system prompt 之后（不替换），并可限制 `allowedTools`。工具白名单：Coder = 全部（nil）；Reviewer = 只读（`read_file, list_files, search_in_files, git_*,  get_file_info, recall`）；Researcher = `read_file, list_files, search_in_files, download_file, run_shell, recall, remember`。

- **Channel 抽象统一 CLI/Telegram**
  `channel.Channel` 是 IO 接口：`Send`/`EditMessage`/`NotifyDone`/`SendFile`。CLI 的 `EditMessage` 为 no-op，Telegram 每 1.5s 刷新消息避免轰炸。新增频道只需实现该接口。

- **记忆两层 SQLite**
  全局库（用户级/跨项目）+ 项目库（仅当前 workspace），均启用 FTS5。`Store.migrate()` 自动建表。`memory/prompt.go` 在构建 system prompt 时检索相关记忆注入。

- **Session 快照可序列化**
  `SessionSnapshot`（`orchestrator/session.go`）含 channel_id、user_id、agent_type、messages。用于热重启时把对话交接给新 worker，测试见 `session_snapshot_test.go`。

- **工作区安全**
  所有文件工具通过 `pkg/tools/workspace.go` 校验路径在工作目录内，返回中文错误（"路径超出工作区"）。

- **原子文件写入**
  `atomic_write_unix.go` / `atomic_write_windows.go` 分平台实现 tmp+rename，防写入中断损坏。

- **并发工具执行**
  `BaseAgent` 同一轮的多个 tool_call 并发跑（`maxConcurrency=5`），全部完成后再进入下轮。

- **跨平台 Shell**
  `run_shell` 在 Windows 用 `cmd /C`，Unix 用 `/bin/sh -c`。命令 `mini-code restart` 是特殊拦截值（`pkg/tools/restart.go`），不经 shell 执行，而是调用注册的 `RestartHandler` 触发热重启。

- **兼容垫片**
  `pkg/agent/engine.go` 中的 `ClawEngine`/`chatCompletionClient`/`legacyClientAdapter` 是 Phase 1 遗留兼容层，仅为旧测试编译通过，新代码应直接用 `BaseAgent` + `provider.Provider`。

## 工具系统扩展

新工具：在 `pkg/tools/` 创建文件 → 定义参数结构体 → 实现 `ToolExecutor` → 在 `registry.go` 的 `init()` 调用 `register()`。
- `jsonschema:"required"` 标签：控制 OpenAI function calling schema 中的 required 字段
- `validate:"required"` 标签（`go-playground/validator`）：运行时参数校验，校验失败返回中文错误
- Schema 由 `invopop/jsonschema` 从结构体自动生成（`system.go` 中的 `generateSchema`）
- SQLite 使用 `modernc.org/sqlite`（纯 Go，无 CGO 依赖，可无 CGO 交叉编译）

调度子 Agent 的 `dispatch_agent` 工具允许主 Coder 调用 Reviewer/Researcher 处理子任务（定义见 `pkg/tools/dispatch.go`）。

## 调试

- `LM_DEBUG=1` 写入 `lm_debug.log`（已在 `.gitignore`，但可能较大，定期清理）
- `LM_LOG_LEVEL=verbose` 打印完整请求/响应
- Telegram 侧设 `TELEGRAM_DEBUG=1` 会在控制台打印发送者 user_id

## 提交规范

Conventional Commits：`feat:` / `fix:` / `docs:` / `refactor:` / `test:`，可加 scope，例如 `feat(agent): ...`。
