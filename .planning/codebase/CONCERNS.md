# Codebase Concerns

**Analysis Date:** 2026-04-20

## Tech Debt

**Legacy Compatibility Layer (`pkg/agent/engine.go`):**
- Issue: Entire file is a Phase 1 shim that was explicitly marked "deleted at end of Phase 1" but never removed. Contains `ClawEngine`, `legacyClientAdapter`, `chatCompletionClient`, `NewClawEngine`, `RunTurn`, `RunTurnStream`, `RunStream`, `RunLegacy`, and six other forwarding methods — 280 lines of dead-weight wrappers.
- Files: `pkg/agent/engine.go`
- Impact: Doubles the surface area of `BaseAgent`. All new agent logic (session memory, turn limits) is duplicated between `RunTurnStream` (engine.go) and `BaseAgent.Run` (base.go). Future changes must be applied in both places or drift silently.
- Fix approach: Remove `engine.go` after migrating `pkg/agent/engine_test.go` to test `BaseAgent.Run` directly. `engine_test.go` already only uses `NewClawEngine` + `RunTurn`; rewriting those three tests against `BaseAgent` is a one-hour task.

**Duplicated `loadDotEnv` Function:**
- Issue: Identical `loadDotEnv` implementation is copy-pasted into both `cmd/agent/main.go` (line 626) and `cmd/telegram/main.go` (line 159).
- Files: `cmd/agent/main.go`, `cmd/telegram/main.go`
- Impact: Any bug fix or feature (e.g., supporting `export KEY=value` syntax) must be applied twice.
- Fix approach: Extract to `internal/envloader/` or a shared `cmd/shared/` package.

**Duplicated `dispatch_agent` Factory Wiring:**
- Issue: The identical sub-agent factory closure — including the bare system prompt `"你是专业的 AI 助手。"` — is copy-pasted in both `cmd/agent/main.go` (line 382) and `cmd/telegram/main.go` (line 112).
- Files: `cmd/agent/main.go`, `cmd/telegram/main.go`
- Impact: Sub-agent system prompts diverge from main agents silently; same duplication risk as above.
- Fix approach: Extract to a shared `newDispatchFactory(factory AgentFactory) tools.AgentFactory` helper.

**`Session.currentTurn` / `lastToolName` Never Updated:**
- Issue: `Session` has fields `currentTurn int` and `lastToolName string` (session.go line 32-33) that are read by `/status` command but are never written during agent execution. Both always return zero/empty.
- Files: `pkg/orchestrator/session.go`, `pkg/orchestrator/orchestrator.go`
- Impact: `/status` command always shows misleading defaults regardless of actual task state.
- Fix approach: Increment `currentTurn` and set `lastToolName` in `handleCLI`/`handleTelegram` inside the stream handler, or accept this as a known display gap and document it.

---

## Known Bugs

**`GetAll()` Uses `defer rows.Close()` in a Loop — Connection May Stay Open:**
- Symptoms: Two separate query result sets in `GetAll()` both use `defer rows.Close()`. The first set's `defer` will not execute until the function returns, meaning the first `*sql.Rows` holds an open connection while the second query runs on the same `db.SetMaxOpenConns(1)` connection.
- Files: `pkg/memory/longterm.go` lines 145, 159
- Trigger: Calling `/memory` command in Telegram or any code path that calls `Store.GetAll()`.
- Workaround: SQLite with WAL mode is lenient but this is technically incorrect. Replace both `defer` calls with explicit `rows.Close()` after each iteration loop.

**`Recall()` Silently Swallows FTS5 Errors:**
- Symptoms: If FTS5 index is corrupted or query syntax is invalid, `Recall()` silently continues to the next DB with `continue` (longterm.go line 83). The function returns `(nil, nil)` — empty results with no error.
- Files: `pkg/memory/longterm.go` lines 82-84
- Trigger: Malformed FTS5 query string (e.g., special characters) or database integrity issue.
- Workaround: None — caller receives empty results and believes memory is empty.

**`Recall()` Does Not Filter by Workspace for Project Scope:**
- Symptoms: `Recall()` performs FTS5 search without any `workspace =` filter even when `scope == "project"`. A project-scoped memory from workspace A will appear in results for workspace B.
- Files: `pkg/memory/longterm.go` lines 74-94
- Trigger: Multiple projects sharing the same global DB (default setup uses a single `~/.mini-code/memory.db`).
- Workaround: The `workspace` parameter is accepted but unused in the SQL query.

**`handleTelegram` Does Not Update Final Message with Full Content:**
- Symptoms: The stream handler's `done=true` branch calls `ch.EditMessage(msgID, fullContent)` to show the final response. However, the Orchestrator then sends a *separate* `ch.NotifyDone` message with `"✅ 任务已完成"`. Users receive two messages: the streamed content message, then a redundant completion notification.
- Files: `pkg/orchestrator/orchestrator.go` lines 190-241
- Trigger: Every Telegram interaction.

---

## Security Considerations

**`run_shell` Has No Command Allowlist/Denylist:**
- Risk: The LLM can execute any shell command on the host system. There is no filtering, rate limiting, or confirmation step. Prompt injection via file contents could escalate to `rm -rf`, credential exfiltration, or network calls.
- Files: `pkg/tools/file.go` lines 230-285
- Current mitigation: Workspace path restriction in `resolveWorkspacePath` protects file operations, but `run_shell` uses `cmd.Dir = root` only as the working directory — it does not prevent absolute path access or network commands.
- Recommendations: Add a configurable `LM_SHELL_DISABLED=true` env var to disable shell entirely; or present a confirmation prompt for destructive patterns (`rm`, `curl`, `wget`, `chmod`).

**Telegram Bot Has No Rate Limiting:**
- Risk: Any allowed user (or all users if `TELEGRAM_ALLOWED_USERS` is empty) can send unlimited requests, causing unbounded LLM API spend and CPU/memory usage.
- Files: `pkg/channel/telegram/telegram.go`, `pkg/channel/telegram/runner.go`
- Current mitigation: Message channel buffer of 10 (`make(chan channel.IncomingMessage, 10)`) — messages beyond 10 are silently dropped after 5-second timeout.
- Recommendations: Add per-user request rate limiting and concurrent-task limit (one active task per user).

**Telegram File Downloads Use `http.Get` Without Timeout:**
- Risk: `downloadFile` in telegram.go calls `http.Get(url)` with no HTTP client timeout. A slow or malicious Telegram CDN response can block the goroutine indefinitely.
- Files: `pkg/channel/telegram/telegram.go` line 194
- Current mitigation: None.
- Recommendations: Use `http.Client{Timeout: 30 * time.Second}` instead of the default client.

**`TELEGRAM_ALLOWED_USERS` Empty Means All Users Allowed:**
- Risk: If `TELEGRAM_ALLOWED_USERS` is not set (the default), the bot serves all Telegram users worldwide — the bot has shell execution capability.
- Files: `pkg/channel/telegram/telegram.go` lines 125-131, `cmd/telegram/main.go` line 133
- Current mitigation: Warning is printed to console but bot starts anyway.
- Recommendations: Fail-closed: require explicit `TELEGRAM_ALLOWED_USERS=*` opt-in for open access rather than open-by-default.

**Session Snapshot Written to Disk in Plaintext:**
- Risk: `writeSnapshot` in `pkg/supervisor/worker_runtime.go` writes full conversation history (including tool results, file contents, API responses) to `.mini-code/runtime/<id>.json` with permissions `0644`. No cleanup occurs after successful restart.
- Files: `pkg/supervisor/worker_runtime.go` lines 113-134
- Current mitigation: File is in the workspace directory (presumably private).
- Recommendations: Delete snapshot file after successful load; use `0600` permissions.

---

## Performance Bottlenecks

**`getDirSize` Uses `filepath.Walk` (Single-Threaded):**
- Problem: `GetFileInfo` on a large directory calls `getDirSize` which uses stdlib `filepath.Walk` — single-threaded, stat-per-file. On directories with thousands of files this is slow.
- Files: `pkg/tools/file_ops.go` lines 403-415
- Cause: `filepath.Walk` is O(n) with blocking stat calls.
- Improvement path: Use `fastwalk` (already imported for `walkWorkspace`) or skip directory size calculation for directories above a threshold.

**`SearchInFiles` Reads Entire File Into Memory:**
- Problem: For each file, `SearchInFiles` calls `os.ReadFile(path)` loading the full content into memory before scanning line by line. On large files (e.g., generated files, log files) this is wasteful.
- Files: `pkg/tools/file.go` lines 172-174
- Cause: No streaming/buffered read; no file size check before read.
- Improvement path: Use `bufio.Scanner` for line-by-line reading; skip files above a size limit.

**Session Memory Grows Unbounded Per Session:**
- Problem: `Session.messages` is appended to indefinitely. A long multi-tool task can accumulate hundreds of messages. All messages are sent with every LLM request, growing token cost super-linearly.
- Files: `pkg/orchestrator/session.go`
- Cause: No message history pruning or summarization strategy.
- Improvement path: Implement sliding window (keep last N messages + system prompt) or summarize old turns using a lightweight model call.

---

## Fragile Areas

**`pkg/tools/` Global Mutable State (Three Package-Level Vars):**
- Files: `pkg/tools/memory_tools.go` (globalMemoryStore), `pkg/tools/dispatch.go` (dispatchFactory), `pkg/tools/restart.go` (restartHandler)
- Why fragile: All three are package-level variables set via setter functions at startup. Tests that register tool executors directly (`tools.Executors[name] = ...`) can accidentally interact with these globals. The Telegram entry point sets `tools.SetMemoryStore` but never calls `tools.SetRestartHandler`, meaning `mini-code restart` silently does nothing in the Telegram process.
- Safe modification: Always check that all three vars are set when adding a new entry point. Consider bundling them into a `ToolContext` struct injected at initialization.
- Test coverage: No integration test covers the Telegram `restart` code path.

**`pkg/agent/engine.go` `RunTurnStream` Memory Save Goroutine:**
- Files: `pkg/agent/engine.go` lines 227-238
- Why fragile: On task completion, a bare `go func()` saves session memory without any reference to the goroutine. If the process exits (SIGINT path calls `os.Exit(0)`) the goroutine is abandoned mid-write, potentially corrupting the SQLite WAL.
- Safe modification: Use a `sync.WaitGroup` or context-aware goroutine, or move session memory saving to the Orchestrator after `Handle()` returns.

**Keyword Router Is Brittle for Mixed Messages:**
- Files: `pkg/orchestrator/router.go`
- Why fragile: Simple `strings.Contains` on the lowercased message. A message like "我想了解如何实现代码审查" will match `researcherKeywords` ("了解", "如何实现") but probably belongs to `reviewer`. First-match wins with no priority scoring.
- Safe modification: Expand keyword sets carefully; order matters. Any new keyword addition can change routing for existing messages.
- Test coverage: `router_test.go` covers basic cases but not mixed-keyword conflicts.

**Supervisor `CompletePromotion` Has No Timeout:**
- Files: `pkg/supervisor/control.go` lines 72-87
- Why fragile: `CompletePromotion` sends `activate` to the pending worker but does not check whether the worker actually started handling messages. If the new worker crashes between `ready` and first message, the supervisor has no detection mechanism other than the process exit channel.
- Safe modification: Add a heartbeat or acknowledgment message after `activate`.

---

## Scaling Limits

**Single-Process Tool Registry:**
- Current capacity: One process, all tools share a single global registry (`tools.Definitions`, `tools.Executors`).
- Limit: If multiple Orchestrator instances (e.g., future multi-workspace) are needed in one process, they would all share identical tool configuration.
- Scaling path: Move tool definitions to per-agent configuration rather than package globals.

**Orchestrator Session Map Grows Without Bound (Telegram):**
- Current capacity: `evictLoop` cleans up sessions inactive for >24 hours, running hourly.
- Limit: In a high-traffic Telegram deployment, up to 24 hours × N new users worth of sessions accumulate in memory. Each session holds full message history.
- Scaling path: Persist sessions to SQLite; reduce eviction window; cap message history per session.

---

## Dependencies at Risk

**`pkg/agent/engine.go` Depends on `github.com/sashabaranov/go-openai` Structs Directly:**
- Risk: `RunTurn` and `RunTurnStream` build `openai.ChatCompletionRequest` manually with `tools.Definitions` (the full unfiltered list). When `engine.go` is eventually removed, any code still using `NewClawEngine` will break at compile time — but callers in tests may be silently using the wrong tool set.
- Impact: `engine_test.go` passes tool names that don't exist in `tools.Definitions`, relying on the error-passthrough behavior.
- Migration plan: Remove `engine.go` after all tests reference `BaseAgent.Run`.

---

## Test Coverage Gaps

**Telegram Channel Has No End-to-End Integration Test:**
- What's not tested: Full message flow from Telegram update through Orchestrator to agent response and `EditMessage` call.
- Files: `pkg/channel/telegram/telegram_test.go`, `pkg/channel/telegram/commands_test.go`
- Risk: Regressions in stream batching, message splitting, or cancel handling go undetected until production.
- Priority: High

**`pkg/supervisor/` Restart Handoff Not Tested Under Load:**
- What's not tested: Snapshot serialization + new worker restore under concurrent message handling.
- Files: `pkg/supervisor/control_test.go`, `pkg/supervisor/worker_runtime_test.go`
- Risk: Session data loss or corruption during hot restart if message is in-flight.
- Priority: High

**`pkg/memory/Recall()` Workspace Filtering Not Tested:**
- What's not tested: That project-scoped memories from different workspaces are properly isolated in `Recall()`.
- Files: `pkg/memory/store_test.go`
- Risk: Cross-project memory leakage — silent data correctness bug.
- Priority: High

**`pkg/orchestrator/router.go` Conflicting Keyword Cases:**
- What's not tested: Messages that contain keywords from both `reviewerKeywords` and `researcherKeywords` simultaneously.
- Files: `pkg/orchestrator/router_test.go`
- Risk: Unexpected agent selection in real user interactions.
- Priority: Medium

**`BaseAgent.Run` CLI (Non-Streaming) Path Untested in Integration:**
- What's not tested: The non-streaming `handler == nil` code path in `BaseAgent.Run` (base.go line 107-118) is only exercised in unit tests via the legacy `RunTurn` wrapper, not directly.
- Files: `pkg/agent/base_test.go`
- Risk: A regression in non-streaming path silently survives CI if `engine.go` is removed without migrating tests.
- Priority: Medium

---

*Concerns audit: 2026-04-20*
