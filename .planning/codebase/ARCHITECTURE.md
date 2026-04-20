# Architecture

**Analysis Date:** 2026-04-20

## Pattern Overview

**Overall:** Layered event-driven architecture with stateless Agent execution and stateful Session management

**Key Characteristics:**
- Channel abstraction decouples IO transport (CLI, Telegram) from Agent logic
- Orchestrator owns Session state; Agent is stateless and receives full message history each invocation
- Tool execution is concurrent within a single LLM turn (goroutine pool, max 5)
- Supervisor/Worker process split enables hot-reload without losing conversation context
- Dual-scope SQLite memory (global per user + per project) injected into system prompt at session creation

## Layers

**Entry Points (`cmd/`):**
- Purpose: Process bootstrap, environment loading, wiring dependencies together
- Location: `cmd/agent/main.go`, `cmd/telegram/main.go`
- Contains: Supervisor/worker process logic, provider/memory/tool initialization, agent factory closure
- Depends on: All `pkg/` packages
- Used by: OS process invocation

**Channel Layer (`pkg/channel/`):**
- Purpose: Normalize all user IO into `IncomingMessage` / `OutgoingMessage` structs
- Location: `pkg/channel/types.go` (interface), `pkg/channel/cli/`, `pkg/channel/telegram/`
- Contains: `Channel` interface, CLI readline wrapper, Telegram Bot API wrapper with streaming EditMessage
- Depends on: `pkg/textutil` for UTF-8 safe truncation
- Used by: Orchestrator (`Handle` receives `channel.Channel`)

**Orchestrator Layer (`pkg/orchestrator/`):**
- Purpose: Session lifecycle management, message routing, and response dispatching
- Location: `pkg/orchestrator/orchestrator.go`, `router.go`, `session.go`
- Contains: `Orchestrator` struct, `Router` (keyword-based), `Session` (conversation state), `SessionSnapshot` (hot-reload serialization)
- Depends on: `pkg/agent` (interface only), `pkg/memory`, `pkg/channel`, `pkg/textutil`
- Used by: `cmd/agent/main.go`, `pkg/channel/telegram/runner.go`

**Agent Layer (`pkg/agent/`):**
- Purpose: Stateless LLM conversation execution with tool call loop
- Location: `pkg/agent/agent.go` (interface), `base.go` (core loop), `coder.go`, `reviewer.go`, `researcher.go`, `prompt.go`, `engine.go`
- Contains: `Agent` interface, `BaseAgent` with streaming+non-streaming paths, specialized agents with prompt overrides and tool allowlists
- Depends on: `pkg/provider`, `pkg/tools`, `pkg/memory`, `pkg/ui`
- Used by: Orchestrator via `AgentFactory func(agentType string) agent.Agent`

**Provider Layer (`pkg/provider/`):**
- Purpose: Decouple LLM SDK from Agent logic
- Location: `pkg/provider/interface.go`, `pkg/provider/openai.go`
- Contains: `Provider` interface with `Chat` and `ChatStream` methods, `OpenAIProvider` backed by `go-openai`
- Depends on: `github.com/sashabaranov/go-openai`
- Used by: `BaseAgent` only

**Tools Layer (`pkg/tools/`):**
- Purpose: Tool definitions (JSON schema), executors, and workspace safety enforcement
- Location: `pkg/tools/registry.go` (registration), `workspace.go` (path validation), individual tool files
- Contains: ~20 registered tools across file/shell/git/memory/dispatch categories, `ToolExecutor func(args interface{}) (interface{}, error)` type
- Depends on: `pkg/memory` (for memory tools), external: `go-git`, `fastwalk`, `mimetype`, `google/renameio`
- Used by: `BaseAgent.executeToolCallsConcurrently`

**Memory Layer (`pkg/memory/`):**
- Purpose: Persistent context across sessions via dual SQLite stores
- Location: `pkg/memory/store.go`, `longterm.go`, `session.go`, `prompt.go`
- Contains: `Store` with `globalDB` (user/global scope + session summaries) and `projectDB` (project scope), FTS5 full-text search, `BuildPromptSuffix` for system prompt injection
- Depends on: `modernc.org/sqlite`
- Used by: `Orchestrator.buildSystemPrompt`, `tools/memory_tools.go`

**Supervisor Layer (`pkg/supervisor/`):**
- Purpose: Zero-downtime hot-reload of worker process via TCP control channel
- Location: `pkg/supervisor/control.go`, `worker_runtime.go`
- Contains: `ControlServer` (TCP listener in supervisor), `ControlClient` (in worker), `SwitchCoordinator` (active/standby promotion), `WorkerRuntime` (build + restart request)
- Depends on: `pkg/orchestrator` (for `SessionSnapshot`)
- Used by: `cmd/agent/main.go` only (not used by Telegram)

**Support Packages:**
- `pkg/textutil/utf8.go`: UTF-8 safe truncation, rune-level splitting — used by channel and memory layers
- `pkg/ui/`: Terminal colors, streaming text, progress display, ESC key monitor

## Data Flow

**Normal Request (CLI):**

1. User types input → `pkg/channel/cli` sends `channel.IncomingMessage` to channel
2. `cmd/agent/main.go` reads from `cliCh.Messages()` channel
3. `Orchestrator.Handle(ctx, msg, factory, cliCh)` is called
4. `Router.Route(msg.Text)` matches keywords → returns `"coder"` / `"reviewer"` / `"researcher"`
5. `factory(agentType)` creates specialized `BaseAgent`
6. `session.AppendUserMessage(msg.Text)` — user turn added to history
7. `Agent.Run(ctx, session.Messages(), streamHandler)` — full history passed, agent is stateless
8. Inside `BaseAgent.Run`: streaming LLM call → `consumeStream` → if tool_calls present → `executeToolCallsConcurrently` → append tool results → loop
9. Stream chunks forwarded to `StreamChunkHandler` → `ui.StreamingText.Write()` for CLI
10. `session.AppendMessages(newMsgs)` — assistant + tool messages persisted to session
11. `Orchestrator.applyPendingRestart(session)` — checks for pending hot-reload

**Normal Request (Telegram):**

1. Telegram long-poll → `TelegramChannel.Start` → sends `IncomingMessage` to channel
2. `telegram.Runner.processMessages` → `go handleMessage(ctx, msg)`
3. Command prefix check (`/start`, `/reset`, etc.) handled by `CommandHandler`
4. Otherwise: `Orchestrator.Handle(taskCtx, msg, factory, channel)` → `handleTelegram`
5. Initial "⏳ 正在处理..." placeholder message sent via `ch.Send()`
6. Stream chunks accumulate in buffer, `ch.EditMessage(msgID, text)` called every 1.5s
7. On stream complete: final `EditMessage` with full content, then `ch.NotifyDone` triggers Telegram push notification

**Hot-Reload Flow (CLI only):**

1. LLM calls `run_shell` with `mini-code restart` command
2. `tools/restart.go` → `restartHandler()` → `WorkerRuntime.PrepareRestart()` → builds new binary via `go build`
3. After current task completes: `Orchestrator.applyPendingRestart(session.ExportSnapshot())`
4. `WorkerRuntime.ApplyPendingRestart` writes `SessionSnapshot` to `.mini-code/runtime/<id>.json`
5. Worker sends `restart_request` message to supervisor via TCP
6. Supervisor spawns standby worker with `--standby --session-snapshot <path>` flags
7. Standby worker loads snapshot, sends `ready` to supervisor
8. Supervisor sends `shutdown` to old worker, `activate` to standby
9. Standby becomes active, conversation continues from restored session

**State Management:**
- `Session.messages` holds the full OpenAI message history as `[]openai.ChatCompletionMessage`
- Session keyed by `channelID + ":" + userID` in Orchestrator's `sessions` map
- Sessions evicted after 24h inactivity via background goroutine
- Memory (long-term) stored in SQLite, injected into `system` message at session creation

## Key Abstractions

**`channel.Channel` interface (`pkg/channel/types.go`):**
- Purpose: Uniform IO for all transports
- Examples: `pkg/channel/cli/cli.go`, `pkg/channel/telegram/telegram.go`
- Pattern: `Send` returns a `messageID` used by `EditMessage` for streaming updates; `EditMessage` is no-op on CLI

**`agent.Agent` interface (`pkg/agent/agent.go`):**
- Purpose: Stateless agent contract — receives full history, returns final reply + appended messages
- Signature: `Run(ctx, messages, handler) (reply, newMessages, error)`
- Pattern: All three specialized agents embed `*BaseAgent`, differentiated only by `systemPromptOverride` and `allowedTools` allowlist

**`tools.ToolExecutor` type (`pkg/tools/registry.go`):**
- Purpose: Uniform tool execution callable from `BaseAgent`
- Pattern: `func(args interface{}) (interface{}, error)` — args are raw JSON parsed by `parseArgs` inside executor

**`orchestrator.AgentFactory` type (`pkg/orchestrator/orchestrator.go`):**
- Purpose: Decouple orchestrator from concrete agent types
- Pattern: Closure defined in `cmd/*/main.go` that captures `provider.Provider`, enabling injection

**`supervisor.ControlMessage` (`pkg/supervisor/control.go`):**
- Purpose: JSON messages over TCP for supervisor↔worker coordination
- Message types: `ready`, `restart_request`, `activate`, `shutdown`, `failed`, `disconnected`

## Entry Points

**CLI Worker (`cmd/agent/main.go`):**
- Location: `runWorker()` function
- Triggers: Process started with `--worker` flag by supervisor, or directly for single-process mode
- Responsibilities: Load `.env`, init provider/memory/tools, create readline channel, run message loop

**CLI Supervisor (`cmd/agent/main.go`):**
- Location: `runSupervisor()` function
- Triggers: Default process start (no `--worker` flag)
- Responsibilities: Spawn worker process, manage TCP control server, handle restart/promotion events

**Telegram Entry (`cmd/telegram/main.go`):**
- Location: `main()` function
- Triggers: Direct process start
- Responsibilities: Load `.env`, init provider/memory/tools, create `telegram.Runner`, start long-poll loop

## Error Handling

**Strategy:** Errors propagate up to entry point; LLM errors surface as formatted strings in stream handler; tool errors return as tool result strings (not Go errors to LLM loop)

**Patterns:**
- Tool executors return `(result, error)` but `BaseAgent` converts errors to result strings so the LLM sees error messages in tool results
- Context cancellation (`ctx.Err() != nil`) is checked explicitly in orchestrator to send "任务已取消" rather than surface as error
- Worker shutdown (`errWorkerShutdown`) is a sentinel error propagated cleanly from worker to supervisor

## Cross-Cutting Concerns

**Logging:** `LM_DEBUG=1` → `lm_debug.log` (file-based via `log.Logger` in `BaseAgent`); `LM_LOG_LEVEL` controls terminal verbosity; Telegram: `TELEGRAM_DEBUG=1` prints user IDs to stdout

**Validation:** Workspace path validation in `pkg/tools/workspace.go` — all file tools call `resolveWorkspacePath()` which returns 中文 error "路径超出工作区" for out-of-bounds paths

**Authentication:** Telegram user allowlist checked in `TelegramChannel.isAllowed()` before messages enter the processing pipeline; empty allowlist permits all users

---

*Architecture analysis: 2026-04-20*
