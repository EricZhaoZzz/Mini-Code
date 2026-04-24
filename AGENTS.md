# AGENTS.md

## 项目简介
`mini-code` 是一个本地运行的中文 AI 编程助手（CLI），Go 模块路径为 `mini-code`。支持两种运行形态：
- CLI：`go run ./cmd/agent`
- Telegram Bot：`go run ./cmd/telegram`

## 开发命令
```bash
# 构建
go build -o mini-code.exe ./cmd/agent
go build -o mini-code-telegram.exe ./cmd/telegram

# 测试
go test ./...
go test -v ./pkg/tools -run TestWriteFile
go test -cover ./...

# 代码检查
gofmt -w ./cmd ./pkg
go vet ./...
```

## 环境配置
复制 `.env.example` 为 `.env`，必需变量：`API_KEY`, `BASE_URL`, `MODEL`。

可选变量：`LM_LOG_LEVEL`（minimal/normal/verbose）、`LM_DEBUG`（写入 `lm_debug.log`）、`LM_MAX_TURNS`（默认 50）。

Telegram Bot 需要额外配置：`TELEGRAM_BOT_TOKEN`, `TELEGRAM_ALLOWED_USERS`（可选）。

## 目录结构
```
cmd/agent/main.go         # CLI 入口（supervisor/worker 双角色）
cmd/telegram/main.go     # Telegram Bot 入口
pkg/
├── agent/               # Agent 实现（base.go, coder/reviewer/researcher.go）
├── channel/             # IO 抽象（cli/, telegram/）
├── memory/              # 记忆系统（两层 SQLite + FTS5）
├── orchestrator/        # 会话编排（orchestrator, router, session）
├── provider/            # LLM 客户端（OpenAI 兼容）
├── supervisor/          # 热重启控制协议
├── tools/               # 工具注册与执行
└── ui/                  # 终端 UI 组件
```

## 关键约束
- **Supervisor/Worker 热重启**：`cmd/agent` 启动时 parent 作为 supervisor，spawn 子进程 worker。升级时通过 TCP 控制通道协调。
- **Agent 专化**：router.go 用关键词将消息路由到 Coder/Reviewer/Researcher。
- **工作区安全**：所有文件工具通过 `pkg/tools/workspace.go` 校验路径。
- **原子写入**：`atomic_write_windows.go` / `atomic_write_unix.go` 实现跨平台 tmp+rename。
- **并发工具**：`BaseAgent` 同一轮多个 tool_call 并发执行（maxConcurrency=5）。
- **跨平台 Shell**：`run_shell` 在 Windows 用 `cmd /C`，Unix 用 `/bin/sh -c`。

## CLI 特别注意
- Windows 编译输出为 `mini-code.exe`
- Shell 命令在 Windows 使用 CMD 语法
- 路径分隔符自动适配为反斜杠 `\`
- 内置命令：`help`/`h`/`?`、`exit`/`quit`/`q`、`clear`/`cls`、`reset`/`r`/`new`/`n`、`history`/`hist`、`version`/`v`

## 调试
- `LM_DEBUG=1` 写入 `lm_debug.log`
- `LM_LOG_LEVEL=verbose` 打印完整请求/响应
- `TELEGRAM_DEBUG=1` 打印 Telegram 发送者 user_id

## 提交规范
Conventional Commits：`feat:` / `fix:` / `docs:` / `refactor:` / `test:`，可加 scope。