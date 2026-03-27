# Mini-Code → OpenClaw 演进设计规格

**日期：** 2026-03-27
**状态：** 已批准
**目标：** 在现有 Mini-Code CLI 基础上，实现 OpenClaw 核心能力子集：持久化三层记忆系统、角色专化多 Agent、Telegram 单一平台接入。

---

## 1. 目标与范围

### 1.1 构建目标

基于现有 Mini-Code（Go 实现的本地 AI 编程助手 CLI），以分层架构扩展为具备以下能力的系统：

1. **持久化三层记忆系统**（对齐 OpenClaw）：Working Memory + Session Memory（72h TTL）+ Long-term Fact Store（`remember`/`forget`/`recall` 工具）
2. **角色专化多 Agent**：CoderAgent（全工具）、ReviewerAgent（只读）、ResearcherAgent（搜索）
3. **Telegram Channel 接入**：远程遥控 + 文件收发 + 任务完成推送通知
4. **统一 Channel 接口**：CLI 和 Telegram 实现同一接口，未来扩展零成本

### 1.2 不在范围内

- 其他消息平台（WhatsApp、Slack 等）
- 向量数据库（用 SQLite FTS5 替代）
- 多 LLM Provider 路由（保持 OpenAI 兼容接口）
- Web UI

---

## 2. 整体架构

### 2.1 分层模型

```
┌─────────────────────────────────────────────────────┐
│               Channel 层（pkg/channel/）              │
│         CLI Channel        Telegram Channel          │
│         统一 Channel 接口（types.go）                  │
└──────────────────────┬──────────────────────────────┘
                       ↓
┌─────────────────────────────────────────────────────┐
│             Orchestrator 层（pkg/orchestrator/）      │
│    Session 管理 │ Agent 路由 │ 跨 Channel 状态管理     │
└──────────────────────┬──────────────────────────────┘
                       ↓
┌─────────────────────────────────────────────────────┐
│               Agent 层（pkg/agent/）                  │
│   BaseAgent  │  CoderAgent  │  ReviewerAgent         │
│                             │  ResearcherAgent       │
└──────────┬────────────────────────────────┬─────────┘
           ↓                                ↓
┌──────────────────┐              ┌──────────────────────┐
│  Memory 层        │              │  Tool + Provider 层   │
│  pkg/memory/     │              │  pkg/tools/（不变）    │
│  SQLite 三层记忆  │              │  pkg/provider/（补全） │
└──────────────────┘              └──────────────────────┘
```

### 2.2 包变动一览

| 包路径 | 状态 | 说明 |
|--------|------|------|
| `pkg/tools/`（13 个工具） | KEEP | 完全不变 |
| `pkg/ui/` | KEEP | 完全不变 |
| `pkg/agent/engine.go` | EXTEND | 重构为 `base.go`，实现 `Agent` 接口 |
| `pkg/provider/` | EXTEND | 补全 `Provider` 接口 + OpenAI 实现 |
| `pkg/tools/registry.go` | EXTEND | 新增注册 4 个工具 |
| `pkg/channel/` | NEW | CLI + Telegram Channel 实现 |
| `pkg/orchestrator/` | NEW | Session 管理 + Agent 路由 |
| `pkg/memory/` | NEW | SQLite 三层记忆 |
| `pkg/tools/memory_tools.go` | NEW | remember/forget/recall 实现 |
| `pkg/tools/dispatch.go` | NEW | dispatch_agent 实现 |
| `cmd/agent/main.go` | EXTEND | 替换为 Channel 启动方式 |

---

## 3. Channel 层

### 3.1 接口定义（`pkg/channel/types.go`）

```go
type Channel interface {
    Start(ctx context.Context) error
    Messages() <-chan IncomingMessage
    Send(msg OutgoingMessage) error
    SendFile(chatID string, path string) error
    EditMessage(id string, text string) error
    NotifyDone(chatID string, text string) error
}

type IncomingMessage struct {
    ChannelID string    // "cli" 或 Telegram chat_id
    UserID    string
    Text      string
    Files     []string  // 本地临时路径（附件已预先下载）
    ReplyTo   string
}

type OutgoingMessage struct {
    ChatID    string
    Text      string
    ReplyToID string
}
```

### 3.2 CLI Channel（`pkg/channel/cli/runner.go`）

- 将现有 `cmd/agent/main.go` 的 readline REPL 逻辑提取为 `CLIChannel`
- `ChannelID` 固定为 `"cli"`，`UserID` 固定为 `"local"`
- `Send` = 终端流式打印（现有行为不变）
- `EditMessage` = no-op（CLI 不需要编辑消息）
- `NotifyDone` = `ui.PrintSuccess()`

### 3.3 Telegram Channel（`pkg/channel/telegram/bot.go`）

**依赖：** `github.com/go-telegram-bot-api/telegram-bot-api/v5`

**配置环境变量：**
```env
TELEGRAM_BOT_TOKEN=<BotFather 颁发的 token>
TELEGRAM_ALLOWED_USERS=123456789,987654321   # 逗号分隔的允许用户 ID
```

**关键行为：**
- Long Polling 接收消息，无需公网 IP / HTTPS
- 收到附件（图片/文档）时，先调用 Telegram API 下载到 `os.TempDir()/mini-code/` 临时目录，再以文件路径传给 Agent
- 流式响应：Agent 执行期间，每 1.5 秒调用 `EditMessage` 刷新同一条消息
- 任务完成后调用 `NotifyDone` 发送**新消息**（触发手机推送通知）
- 响应超过 4096 字符时自动分段发送
- 同一 `chat_id` 串行处理（Lane Queue，防状态竞争）

**支持的 `/` 命令：**

| 命令 | 功能 |
|------|------|
| `/start` | 欢迎消息 + 当前工作区信息 |
| `/reset` | 清空当前会话历史（保留长期记忆），同时清理当前会话临时文件 |
| `/memory` | 查看已记住的项目 / 用户记忆 |
| `/status` | 当前任务执行进度（显示：当前 Agent 类型、已执行轮次、最近一次工具调用名称） |
| `/cancel` | 取消当前正在执行的任务 |

**取消机制：** Session 持有 `cancel context.CancelFunc`。Orchestrator 在调用 `Agent.Run()` 前创建 `context.WithCancel`，将 `cancel` 存入 Session。收到 `/cancel` 命令时调用该函数，`Agent.Run()` 通过 `ctx.Done()` 检测取消并提前返回。被取消的轮次中**已完成**的工具调用结果写入消息历史，**未开始**的不写入，保持历史一致性。

**临时文件清理：** Telegram Channel 每次会话结束（正常完成或 `/reset`）后，清理 `os.TempDir()/mini-code/<chatID>/` 目录下的所有临时附件文件。

---

## 4. Orchestrator 层

### 4.1 主调度器（`pkg/orchestrator/orchestrator.go`）

职责：
1. 监听所有 Channel 的 `Messages()` 通道
2. 根据 `ChannelID + UserID` 查找或创建 `Session`
3. 从 Memory 层加载三类记忆，注入 system prompt
4. 调用 `Router` 选择 Agent 类型
5. 将消息交给 Agent 执行，将响应回传给 Channel
6. 任务完成后异步触发 Session Memory 更新

### 4.2 Session 管理（`pkg/orchestrator/session.go`）

```go
type Session struct {
    ID        string
    ChannelID string
    UserID    string
    AgentType string
    Messages  []openai.ChatCompletionMessage  // Working Memory 由 Session 持有
    cancel    context.CancelFunc              // 用于 /cancel 命令
    CreatedAt time.Time
    mu        sync.Mutex
}
```

**Working Memory 归属：** Session 对象持有 `Messages` 切片（即 Working Memory）。每次 Orchestrator 处理消息时：
1. 将新用户消息追加到 `session.Messages`
2. 将记忆注入 system prompt 后，把完整 `session.Messages` 传给 `Agent.Run()`
3. `Agent.Run()` 返回后，将 assistant 消息和工具消息追加回 `session.Messages`

`BaseAgent` 内部**不再**维护消息历史，每次 `Run()` 调用接收完整历史并在调用结束后返回，历史管理职责上移至 Orchestrator。

- 内存中维护 `map[string]*Session`，key 为 `ChannelID:UserID`
- `/reset` 命令清空 `session.Messages`（保留 system 消息），但不删除 Long-term Memory
- **Session 过期驱逐：** 不活跃超过 24 小时的 Session 从 map 中驱逐（后台 goroutine 每小时扫描一次），防止内存无限增长

### 4.3 Agent 路由（`pkg/orchestrator/router.go`）

路由策略（优先级从高到低）：
1. **显式命令**：用户明确说"帮我 review"→ ReviewerAgent，"调研一下"→ ResearcherAgent
2. **关键词匹配**：审查/检查/review → Reviewer；调研/搜索/了解 → Researcher
3. **默认兜底**：CoderAgent

---

## 5. Agent 层

### 5.1 Agent 接口（`pkg/agent/agent.go`）

```go
type Agent interface {
    Run(ctx context.Context, messages []openai.ChatCompletionMessage, handler StreamChunkHandler) (string, error)
    Name() string
    AllowedTools() []string
}
```

### 5.2 BaseAgent（`pkg/agent/base.go`）

- 现有 `ClawEngine` 的直接重构，核心 ReAct 循环逻辑完全保留
- 接收外部传入的 `messages`（由 Orchestrator 注入记忆后传入）
- 接收 `allowedTools` 过滤器，限定工具调用范围

### 5.3 专化 Agent

| Agent | System Prompt 重点 | 工具集 |
|-------|-------------------|--------|
| **CoderAgent** | 代码质量、最小化修改、运行测试验证 | 全部 17 个工具（13 原有 + remember/forget/recall/dispatch_agent） |
| **ReviewerAgent** | 只读分析、发现问题、给出改进建议（不直接修改） | read_file、list_files、search_in_files、git_diff、git_log、git_status、get_file_info、recall |
| **ResearcherAgent** | 广泛搜索、整理信息、有依据的结论 | read_file、list_files、search_in_files、download_file、run_shell、recall、remember |

### 5.4 子 Agent 调度（`pkg/tools/dispatch.go`）

`dispatch_agent(role, task)` 工具：CoderAgent 执行复杂任务时可调起 ReviewerAgent 并行验证，结果汇总后返回。

**实现方式：直接实例化 Agent，不经过 Orchestrator。**

`dispatch_agent` 工具在注册时通过闭包注入 `*memory.Store`，调用时：
1. 根据 `role` 参数直接在当前 goroutine 内创建对应 Agent 实例（ReviewerAgent / ResearcherAgent）
2. 以 `task` 为 user 消息、以注入的 Memory Store 构建轻量 messages 切片
3. 同步调用子 Agent 的 `Run()`，返回结果字符串给调用方 CoderAgent
4. 子 Agent 的消息历史不写回父 Session，保持隔离

此方案无需 Orchestrator 暴露任何额外接口，`pkg/tools/dispatch.go` 只依赖 `pkg/agent` 和 `pkg/memory`。

---

## 6. Memory 层

### 6.1 三层记忆模型

| 层级 | 存储 | 时效 | 读取时机 | 写入时机 |
|------|------|------|---------|---------|
| Working Memory | 内存 `[]ChatMessage` | 会话内 | 每轮对话 | 每轮对话 |
| Session Memory | SQLite `session_memory` | 72h TTL | 新会话开始 | 任务完成后异步 |
| Long-term Memory | SQLite `long_term_memory` | 永久 | 新会话开始 | Agent 调用 `remember()` |

### 6.2 存储位置与 Scope 映射

```
~/.mini-code/memory.db             # 全局 DB：scope=user 和 scope=global 的 long_term_memory，以及所有 session_memory
<workspace>/.mini-code/project.db  # 项目 DB：scope=project 的 long_term_memory（仅此表）
```

**`pkg/memory/store.go` 需维护两个 DB 连接：**
- `globalDB`：启动时打开 `~/.mini-code/memory.db`，整个生命周期有效
- `projectDB`：每次 Orchestrator 处理消息时，根据当前工作区路径按需打开（已打开则复用）

`remember(scope=project, ...)` → 写 `projectDB.long_term_memory`
`remember(scope=user/global, ...)` → 写 `globalDB.long_term_memory`
`session_memory` 始终写 `globalDB`，通过 `workspace` 字段区分项目

### 6.3 SQLite Schema

```sql
-- 短期记忆（72h TTL）
CREATE TABLE IF NOT EXISTS session_memory (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    workspace   TEXT NOT NULL,
    content     TEXT NOT NULL,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at  DATETIME NOT NULL
);

-- 长期记忆 Fact Store
CREATE TABLE IF NOT EXISTS long_term_memory (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    scope       TEXT NOT NULL CHECK(scope IN ('project','user','global')),
    workspace   TEXT,           -- scope=project 时关联工作区路径
    content     TEXT NOT NULL,  -- 自然语言事实描述
    tags        TEXT,           -- JSON 数组，如 ["architecture","decision"]
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- FTS5 全文检索虚拟表
CREATE VIRTUAL TABLE IF NOT EXISTS memory_fts
USING fts5(content, tags, content=long_term_memory, content_rowid=id);
```

### 6.4 记忆工具（新增到 `pkg/tools/`）

**`remember(content, scope, tags?)`**
- 将事实写入 `long_term_memory` 表
- 自动更新 FTS5 索引
- 返回 `memory_id`（供 `forget` 使用）

**`forget(memory_id)`**
- 从 `long_term_memory` 删除指定记忆
- 更新 FTS5 索引

**`recall(query, scope?)`**
- 用 SQLite FTS5 全文检索相关记忆
- 返回最相关的 N 条（默认 10 条）

### 6.5 记忆注入 system prompt 格式

注入内容追加在 system prompt 末尾，总长度上限为 **2000 字符**。超出时截断优先级（从低到高保留）：
1. 首先截断 Session Memory（最早的摘要先丢弃）
2. 其次截断 Long-term Memory，按 `created_at` 倒序，`user` scope 优先于 `project` scope 保留（用户偏好比项目细节更通用）

```
## 项目记忆
- 技术栈：Go 1.22，无 CGO 依赖
- 编码风格：tab 缩进，错误用 fmt.Errorf 包装

## 用户偏好
- 回复语言：中文
- 详细程度：简洁

## 近期上下文（过去 72 小时）
- 2026-03-26: 完成了 Memory 层的 SQLite 集成，决定使用 modernc.org/sqlite
```

### 6.6 SQLite 驱动

使用 `modernc.org/sqlite`（纯 Go 实现，无 CGO，全平台零配置编译）。

---

## 7. Provider 层补全

### 7.1 Provider 接口（`pkg/provider/interface.go`）

```go
type Provider interface {
    Chat(ctx context.Context, req ChatRequest) (ChatResponse, error)
    ChatStream(ctx context.Context, req ChatRequest) (*ChatStream, error)
}
```

解耦 `pkg/agent` 对 `go-openai` SDK 的直接依赖，为未来接入其他 Provider 打基础（当前只实现 OpenAI 兼容版本）。

---

## 8. 安全设计

| 威胁 | 防护措施 |
|------|---------|
| 未授权 Telegram 访问 | `TELEGRAM_ALLOWED_USERS` 白名单，非白名单消息静默丢弃 |
| 目录穿越 | 现有 `workspace.go` 路径校验保留 |
| 文件操作越权 | 工具调用仍通过 `resolveWorkspacePath` 校验 |
| 无限循环 | 现有 `LM_MAX_TURNS` 限制保留 |
| Telegram 附件安全 | 下载到 `os.TempDir()/mini-code/` 隔离目录，会话结束后清理 |

---

## 9. 分阶段实现路线图

### Phase 1 — 基础重构（保持现有功能）
- 提取 `Provider` 接口，迁移 OpenAI 调用到 `pkg/provider/openai.go`
- 将 `ClawEngine` 重构为 `BaseAgent`，实现 `Agent` 接口
- 提取 CLI readline 为 `CLIChannel`，实现 `Channel` 接口
- 引入 `Orchestrator` 骨架，串联 CLIChannel → Orchestrator → CoderAgent
- **验收标准**：全部现有测试通过，用户体验无变化

### Phase 2 — Memory 层
- 实现 `pkg/memory/store.go`（SQLite 初始化，自动建表）
- 实现 Session Memory（72h TTL）和 Long-term Fact Store
- 实现 `pkg/memory/prompt.go`（记忆格式化注入 system prompt）
- 注册 `remember` / `forget` / `recall` 三个工具
- Orchestrator 集成：会话开始注入记忆，结束后异步更新 Session Memory
- **验收标准**：`remember` 后重开会话，Agent 能感知记住的信息

### Phase 3 — 多 Agent 专化
- 实现 `ReviewerAgent` 和 `ResearcherAgent`（不同 prompt + 工具子集）
- 实现 `router.go` 意图路由（关键词匹配，兜底 CoderAgent）
- 注册 `dispatch_agent` 工具
- **验收标准**：说"帮我 review 代码"时触发 ReviewerAgent，工具集受限

### Phase 4 — Telegram Channel
- 实现 `pkg/channel/telegram/bot.go`（Long Polling + 白名单鉴权）
- 实现附件下载（图片/文档 → 临时文件）
- 实现流式刷新（edit_message 每 1.5s）+ 任务完成新消息通知
- 实现 `/reset` `/memory` `/status` `/cancel` 命令
- `main.go` 支持 `--channel telegram` 启动参数
- **验收标准**：手机 Telegram 发消息，收到流式响应；任务完成后收到推送通知

---

## 10. 新增依赖

| 依赖 | 用途 | 引入阶段 |
|------|------|---------|
| `modernc.org/sqlite` | SQLite 驱动（纯 Go，无 CGO） | Phase 2 |
| `github.com/go-telegram-bot-api/telegram-bot-api/v5` | Telegram Bot API | Phase 4 |

现有依赖（`go-openai`、`readline`、`fastwalk` 等）全部保留。

---

## 11. 测试策略

| 测试类型 | 覆盖范围 |
|---------|---------|
| 单元测试 | Memory CRUD、FTS5 检索、Channel 消息封装、路由规则 |
| 集成测试 | Orchestrator + BaseAgent + Memory 完整流程（mock LLM） |
| 现有测试 | Phase 1 完成后全部通过，后续不退化 |
| 手动验收 | Telegram 附件收发、流式刷新、推送通知 |
