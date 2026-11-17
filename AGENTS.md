# AGENTS.md

## 项目简介
`mini-code` 是本地运行的中文 AI 编程助手（CLI），Go 1.26.1，模块路径 `mini-code`
（导入形如 `mini-code/pkg/agent`）。仅 CLI 一种形态，入口 `cmd/agent`，内置 20 个工具。

## 开发命令
```bash
go build -o mini-code.exe ./cmd/agent   # 构建（Windows）
go run ./cmd/agent                      # 开发运行
go test ./...                           # 全部测试
go test -v ./pkg/tools -run TestWriteFile
go test -race ./...                     # 涉及 Session/并发改动时必跑
gofmt -w ./cmd ./pkg
go vet ./...
```

## 环境配置
`.env` 由 supervisor 和 worker 各自加载；已存在的环境变量不会被覆盖。

LLM Provider 二选一：
- **Pie Gateway（优先）**：`PIE_APP_ID` + `PIE_APP_SECRET` + `PIE_GATEWAY_PATH`，模型名取 `MODEL`，缺省 `deepseek-v3-2`
- **OpenAI 兼容（回退）**：`API_KEY` + `BASE_URL` + `MODEL`

可选变量：
| 变量 | 作用 |
|---|---|
| `LM_LOG_LEVEL` | minimal/normal/verbose（或 0/1/2） |
| `LM_VERBOSE` | 非空即启用 BaseAgent verbose |
| `LM_DEBUG` | 非空即写 `lm_debug.log` |
| `LM_MAX_TURNS` | 最大工具轮次，默认 50，0=不限 |
| `LM_CONTEXT_BUDGET` | 上下文 token 预算，默认 96000 |
| `LM_SHELL_TIMEOUT` | run_shell 超时秒数，默认 120 |
| `LM_SHELL_STRICT` | =1 时 run_shell 只允许白名单命令 |
| `LM_SANDBOX_STRICT` | =1 且 OS 沙箱不可用时拒绝执行（⚠️ Windows 上会拒绝所有 run_shell） |
| `LM_SANDBOX` | =0 显式关闭 OS 级沙箱（逃生舱，启动会打印提示） |
| `LM_SANDBOX_WRITABLE` | 额外允许沙箱读写的目录（逗号分隔） |
| `EXA_MCP_URL` | 覆盖免费 Exa 搜索端点，无需 API key |

注意：`PIE_*` 只用于 LLM，与 `web_search` 无关；搜索走 Exa MCP。

## 架构
```
cmd/agent/main.go          # supervisor + worker 双角色入口
pkg/channel/               # Channel IO 接口；cli/ 为 readline REPL
pkg/orchestrator/          # Session 管理、路由、上下文压缩、快照恢复
pkg/agent/                 # BaseAgent + coder/reviewer/researcher 预设
pkg/memory/                # 两层 SQLite（global/project）+ FTS5
pkg/provider/              # Provider 接口；openai.go / pie_gateway.go
pkg/sandbox/               # L2 OS 沙箱（landlock/seatbelt/none）+ 审计日志
pkg/supervisor/            # TCP 控制协议与热重启运行时
pkg/tools/                 # 工具注册、执行、工作区校验、shell 防护
pkg/textutil/              # UTF-8 安全截断
pkg/ui/                    # 终端颜色、格式、进度
docs/superpowers/          # plans/ 与 specs/ 设计文档（已被 Git 跟踪）
```

请求流：
```
CLI → Orchestrator.Handle → Router 选 agent 类型
   → 追加用户消息 → compactIfNeeded（超预算时摘要压缩）
   → Agent.Run（流式 → 并发工具调用 → 追加结果 → 循环）
   → 新消息写回 Session → applyPendingRestart
```

## 关键设计（非显然部分）
- **Supervisor/Worker 热重启**：parent 监听本地 TCP，spawn 子进程 worker，参数为
  `--worker/--standby/--control-addr/--worker-id/--session-snapshot`。worker 发 `ready`，
  supervisor 通过 `restart_request` 拉起 standby；standby ready 后向旧 worker 发 `shutdown`、
  再发 `activate`，旧进程退出后完成晋升。消息类型见 `pkg/supervisor/control.go`。
- **`mini-code restart`**：`pkg/tools/restart.go` 特殊拦截，不经 shell，触发
  `RestartHandler`：`go build` 到 `.mini-code/releases/<timestamp>/`，再请求 supervisor 切换。
- **工作区边界**：worker 启动时先 `tools.SetWorkspaceRoot(".")` 固定根目录，再初始化其他组件，
  防止后续 chdir 导致安全边界漂移。所有文件工具经 `workspace.go` 校验。
- **Agent 路由是刻意非对称的**：写入意图关键词优先级最高，直接归 coder（唯一拥有全部工具、
  误判代价最低）；其次 reviewer，再 researcher，默认 coder。专化 prompt **追加**在共享 prompt
  之后，不替换；reviewer/researcher 只持有只读工具白名单（见各自 `*Tools` 变量）。
  英文关键词按整词匹配（允许 s/ed/ing 词形），中文按子串匹配。
- **BaseAgent**：同一轮多个 tool_call 并发执行（`maxConcurrency=5`），全部完成后进入下一轮；
  非流式路径（handler=nil）仅供测试。
- **上下文压缩在 Orchestrator 而非 Agent**：Agent 无跨轮状态，压缩若放进 Run 会每轮重复触发。
  默认预算 96k token，保留 system + 最近 12 条，摘要写回 Session 供下次合并；调用 LLM 失败时
  静默放弃压缩，不阻塞主流程。
- **记忆库**：global DB `~/.mini-code/memory.db`（user/global 长期记忆 + session_memory），
  project DB `./.mini-code/project.db`（仅 project scope）；均启用 FTS5，单写连接 + WAL。
- **工具系统**：`registry.go` 的 `init()` 注册；参数结构体同时使用
  `jsonschema:"required"`（生成 function schema）与 `validate:"required"`（运行时校验）；
  schema 由 `invopop/jsonschema` 自动生成。`dispatch_agent` 的工厂由 `main.go` 注入，
  只接受 `reviewer`/`researcher`，子 Agent 继承父任务 ctx 并以静默模式运行。
  耗时工具用 `registerCtx()` 注册以响应取消；危险操作（`delete_file`）经 `askUser()` 由用户本人确认。
- **shell 防护不是沙箱**：始终拦截破坏性命令黑名单；`LM_SHELL_STRICT=1` 时改为白名单模式，
  但通过检查的命令仍可访问工作区外文件。
- **沙箱分三层且刻意不对称**（`pkg/sandbox`，见
  `docs/superpowers/plans/2026-10-07-sandbox-hardening.md`）：
  - **L1 路径边界**（`pkg/tools/workspace*.go`，全平台）：拒绝 ADS（`file.txt:stream`，
    ListFiles 看不到，可绕过内容审计）、UNC、device、NUL；并做句柄级校验拦截
    **NTFS junction 越权**。
  - **L2 OS 强制**（Linux Landlock / macOS Seatbelt）：不可用时**硬失败而非静默降级**。
  - **L3 审批 + 审计**（全平台）：`run_shell` 全部执行落
    `<工作区>/.mini-code/audit.log`（JSONL）。
  - **Windows 无 OS 层**：Job Objects 不限制文件访问，AppContainer 要求事先穷举
    能力清单，与 coding agent 开放的行为空间不匹配。此处只有 L1+L3。
  - 相关环境变量：`LM_SANDBOX_STRICT`（=1 时沙箱不可用即拒绝执行）、
    `LM_SANDBOX`（=0 显式关闭 L2，逃生舱）、`LM_SANDBOX_WRITABLE`（额外可写目录）。
- **junction 的坑（改 workspace*.go 前必读）**：junction 是 reparse point 而非符号链接，
  `os.Lstat` 返回的 Mode **不含 `ModeSymlink`**，`EvalSymlinks` 也解析不了 junction 内部
  的文件 —— 于是「用 Lstat 检查符号链接」这类加固完全失效，字符串层面的 `Rel` 判断
  会把它当普通子目录放行。必须查 `FILE_ATTRIBUTE_REPARSE_POINT`(0x400) 或用
  `GetFinalPathNameByHandle` 取句柄真实路径。
- **Landlock 必须用 runner 子进程收紧**：它限制整个调用线程且不可逆，在主进程收紧会
  连带锁死 mini-code 自己（记忆库在 `~/.mini-code/`、审计日志与构建产物在工作区外）。
  实现上 `Wrap` 只改写命令指向 `mini-code --sandbox-run`，由该子进程收紧后再 exec。
- **Channel 抽象**：`Send`/`EditMessage`/`NotifyDone`/`SendFile`；CLI 的 `EditMessage` 为 no-op。

## CLI 特别注意
- Windows 输出 `mini-code.exe`；shell 走 `cmd /C`，Unix 走 `/bin/sh -c`
- 内置命令：`help/h/?`、`exit/quit/q`、`clear/cls`、`reset/r/new/n`、`history/hist`、`version/v`
- 历史文件 `.mini-code-history`，运行时数据在 `.mini-code/`

## 调试
- `LM_DEBUG=1` 写 `lm_debug.log`
- `LM_LOG_LEVEL=verbose` 打印完整请求/响应

## 提交规范
Conventional Commits：`feat:` / `fix:` / `docs:` / `refactor:` / `test:`，可加 scope。
