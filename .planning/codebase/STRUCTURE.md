# Codebase Structure

**Analysis Date:** 2026-04-20

## Directory Layout

```
mini-code/
├── cmd/
│   ├── agent/              # CLI entry point (supervisor + worker)
│   │   ├── main.go         # Process bootstrap, supervisor/worker roles
│   │   └── main_test.go    # CLI integration tests
│   └── telegram/           # Telegram Bot entry point
│       └── main.go         # Bot bootstrap, agent factory wiring
├── pkg/
│   ├── agent/              # LLM Agent implementations
│   │   ├── agent.go        # Agent interface definition
│   │   ├── base.go         # BaseAgent: streaming loop, concurrent tool execution
│   │   ├── helpers.go      # Tool execution, stream consumption, logging
│   │   ├── engine.go       # Legacy compatibility shim (ClawEngine alias)
│   │   ├── coder.go        # CoderAgent: full tool access, execution-focused prompt
│   │   ├── reviewer.go     # ReviewerAgent: read-only tools, code review prompt
│   │   ├── researcher.go   # ResearcherAgent: search/download tools, research prompt
│   │   └── prompt.go       # System prompt builder with memory injection
│   ├── channel/
│   │   ├── types.go        # Channel interface, IncomingMessage, OutgoingMessage
│   │   ├── cli/            # CLI channel (readline REPL)
│   │   └── telegram/       # Telegram channel (streaming EditMessage)
│   │       ├── telegram.go # TelegramChannel implementation
│   │       ├── runner.go   # Runner: message loop, session concurrency guard
│   │       └── commands.go # CommandHandler: /start /reset /status /cancel /memory
│   ├── memory/
│   │   ├── store.go        # Store: dual SQLite connections, schema migration
│   │   ├── longterm.go     # Long-term memory CRUD + FTS5 search
│   │   ├── session.go      # Session memory (72h expiry summaries)
│   │   └── prompt.go       # BuildPromptSuffix: inject memories into system prompt
│   ├── orchestrator/
│   │   ├── orchestrator.go # Orchestrator: session map, Handle, CLI/Telegram dispatch
│   │   ├── router.go       # Router: keyword matching → agent type
│   │   └── session.go      # Session: message history, snapshot export/restore
│   ├── provider/
│   │   ├── interface.go    # Provider interface (Chat + ChatStream)
│   │   └── openai.go       # OpenAIProvider: go-openai wrapper
│   ├── supervisor/
│   │   ├── control.go      # ControlServer, ControlClient, SwitchCoordinator
│   │   └── worker_runtime.go # WorkerRuntime: binary build + restart request
│   ├── tools/
│   │   ├── registry.go     # Tool registration, Definitions + Executors globals
│   │   ├── workspace.go    # Path validation (resolveWorkspacePath)
│   │   ├── file.go         # write_file, read_file, list_files, search_in_files
│   │   ├── file_ops.go     # rename_file, delete_file, copy_file, create_directory, get_file_info
│   │   ├── edit.go         # replace_in_file (minimal-diff editing)
│   │   ├── system.go       # run_shell (cross-platform: cmd /C vs /bin/sh -c)
│   │   ├── git.go          # git_status, git_diff, git_log
│   │   ├── download.go     # download_file
│   │   ├── memory_tools.go # remember, forget, recall
│   │   ├── dispatch.go     # dispatch_agent (sub-agent invocation)
│   │   ├── restart.go      # RestartHandler injection point
│   │   ├── atomic_write_unix.go    # Atomic write via tmp+rename (Unix)
│   │   └── atomic_write_windows.go # Atomic write via tmp+rename (Windows)
│   ├── textutil/
│   │   └── utf8.go         # TruncateWithEllipsis, TruncateRunes, SplitByRuneLength, SanitizeUTF8
│   └── ui/
│       ├── colors.go       # Color constants, SprintColor
│       ├── format.go       # WelcomeBanner, HelpPanel, PrintAssistantLabel
│       ├── tools.go        # ToolNames, ToolIcons display maps
│       ├── progress.go     # StreamingText (progressive terminal output)
│       ├── input.go        # Input helpers
│       ├── esc_monitor.go  # ESC key cancellation monitor interface
│       ├── esc_monitor_unix.go    # Unix ESC monitor (raw terminal)
│       └── esc_monitor_windows.go # Windows ESC monitor
├── docs/                   # Architecture documents, plans
│   └── superpowers/
│       ├── plans/
│       └── specs/
├── .mini-code/             # Runtime data directory (gitignored)
│   ├── memory.db           # NOT here — lives in $HOME/.mini-code/memory.db (global)
│   ├── project.db          # Project-scoped SQLite memory
│   ├── runtime/            # Session snapshots for hot-reload
│   └── releases/           # Built binaries for hot-reload
├── .planning/              # GSD planning documents
│   └── codebase/           # Codebase analysis documents
├── go.mod                  # Module: mini-code, Go 1.26
├── go.sum
└── CLAUDE.md               # Project instructions for Claude Code
```

## Directory Purposes

**`cmd/agent/`:**
- Purpose: CLI binary that handles both supervisor (parent) and worker (child) roles in the same executable
- Contains: Process flag parsing, `runSupervisor()` and `runWorker()` functions, session snapshot load/save, binary self-rebuild
- Key files: `cmd/agent/main.go`

**`cmd/telegram/`:**
- Purpose: Telegram Bot binary entry point
- Contains: Bot initialization, agent factory wiring, memory store setup
- Key files: `cmd/telegram/main.go`

**`pkg/agent/`:**
- Purpose: Core LLM interaction logic — the agentic loop
- Contains: Interface definition, base implementation, three specialized agents, system prompt builder
- Key files: `pkg/agent/agent.go`, `pkg/agent/base.go`, `pkg/agent/prompt.go`

**`pkg/channel/`:**
- Purpose: IO transport abstraction
- Contains: Unified interface, CLI and Telegram implementations
- Key files: `pkg/channel/types.go`, `pkg/channel/telegram/runner.go`

**`pkg/orchestrator/`:**
- Purpose: Conversation session management and routing
- Contains: Session state machine, keyword router, snapshot serialization
- Key files: `pkg/orchestrator/orchestrator.go`, `pkg/orchestrator/session.go`

**`pkg/tools/`:**
- Purpose: All tool implementations registered for LLM use
- Contains: ~20 tools across file/shell/git/memory/dispatch, workspace safety, atomic writes
- Key files: `pkg/tools/registry.go`, `pkg/tools/workspace.go`

**`pkg/memory/`:**
- Purpose: Persistent memory across sessions
- Contains: Dual SQLite with FTS5, prompt injection builder
- Key files: `pkg/memory/store.go`, `pkg/memory/prompt.go`

**`pkg/supervisor/`:**
- Purpose: Hot-reload coordination between supervisor and worker processes
- Contains: TCP control channel, promotion logic, build+restart coordination
- Key files: `pkg/supervisor/control.go`, `pkg/supervisor/worker_runtime.go`

**`pkg/provider/`:**
- Purpose: LLM client abstraction
- Contains: Provider interface + OpenAI implementation
- Key files: `pkg/provider/interface.go`, `pkg/provider/openai.go`

**`pkg/textutil/`:**
- Purpose: UTF-8 safe string operations shared by channel and memory layers
- Key files: `pkg/textutil/utf8.go`

**`pkg/ui/`:**
- Purpose: Terminal output formatting, streaming display, ESC key handling
- Key files: `pkg/ui/colors.go`, `pkg/ui/progress.go`, `pkg/ui/esc_monitor.go`

## Key File Locations

**Entry Points:**
- `cmd/agent/main.go`: CLI bootstrap — `runSupervisor()` and `runWorker()`
- `cmd/telegram/main.go`: Telegram Bot bootstrap

**Interfaces (contracts):**
- `pkg/agent/agent.go`: `Agent` interface
- `pkg/channel/types.go`: `Channel` interface
- `pkg/provider/interface.go`: `Provider` interface

**Core Logic:**
- `pkg/agent/base.go`: Streaming LLM loop with concurrent tool execution
- `pkg/orchestrator/orchestrator.go`: Session management and message dispatch
- `pkg/tools/registry.go`: All tool registrations in `init()`
- `pkg/tools/workspace.go`: Workspace path security

**Configuration:**
- `go.mod`: Module `mini-code`, Go 1.26
- `.env` (not committed): `API_KEY`, `BASE_URL`, `MODEL`, optional `TELEGRAM_BOT_TOKEN`

**Testing:**
- `pkg/agent/base_test.go`, `agents_test.go`, `prompt_test.go`
- `pkg/orchestrator/orchestrator_test.go`, `router_test.go`, `session_snapshot_test.go`
- `pkg/tools/edit_test.go`, `file_ops_test.go`, `workspace_test.go`, etc.
- `pkg/memory/store_test.go`, `prompt_test.go`
- `pkg/supervisor/control_test.go`, `worker_runtime_test.go`

## Naming Conventions

**Files:**
- Snake_case for multi-word files: `worker_runtime.go`, `atomic_write_unix.go`, `run_shell_test.go`
- Named after primary type or concept: `orchestrator.go`, `session.go`, `registry.go`
- Platform variants: `atomic_write_unix.go` / `atomic_write_windows.go`, `esc_monitor_unix.go` / `esc_monitor_windows.go`
- Tests co-located: `base_test.go` alongside `base.go`

**Directories:**
- Lowercase, short, single-word: `agent`, `tools`, `memory`, `channel`
- Sub-packages for transport variants: `channel/cli/`, `channel/telegram/`

**Types:**
- PascalCase exported types: `BaseAgent`, `Orchestrator`, `SessionSnapshot`, `ControlMessage`
- Interfaces named by capability: `Agent`, `Channel`, `Provider`, `RestartRuntime`
- Error variables: `errWorkerShutdown` (lowercase, package-private)

**Functions:**
- Constructors: `New*` prefix — `NewBaseAgent`, `NewCoderAgent`, `NewRouter`, `NewControlServer`
- Lowercase for unexported helpers: `buildSystemPrompt`, `resolveWorkspacePath`, `newSession`

## Where to Add New Code

**New Tool:**
1. Create `pkg/tools/<toolname>.go` with args struct (use `jsonschema` tags), executor function
2. Call `register("tool_name", "description", ArgsStruct{}, ExecutorFunc)` in `pkg/tools/registry.go`'s `init()`
3. Add test in `pkg/tools/<toolname>_test.go`
4. To restrict to specific agents: add tool name to allowlist in `pkg/agent/reviewer.go` or `researcher.go`

**New Specialized Agent:**
1. Create `pkg/agent/<role>.go` defining `var <role>Tools []string` and `const <role>SystemPrompt`
2. Implement `New<Role>Agent(p provider.Provider, model string) *BaseAgent`
3. Add case to agent factory closure in `cmd/agent/main.go` and `cmd/telegram/main.go`
4. Add routing keywords in `pkg/orchestrator/router.go`

**New Channel (IO transport):**
1. Create `pkg/channel/<transport>/` package
2. Implement all methods of `channel.Channel` interface from `pkg/channel/types.go`
3. `EditMessage` can be no-op if streaming updates are not supported
4. Wire up in appropriate `cmd/*/main.go`

**New Telegram Command:**
- Add case to `CommandHandler.Handle` switch in `pkg/channel/telegram/commands.go`
- Register command in `TelegramChannel.RegisterCommands()` in `pkg/channel/telegram/telegram.go`

**New Memory Operation:**
- Add methods to `pkg/memory/store.go` (or `longterm.go` / `session.go`)
- Add corresponding tool in `pkg/tools/memory_tools.go` and register in `pkg/tools/registry.go`

**Utilities:**
- UTF-8 string helpers: `pkg/textutil/utf8.go`
- Terminal output: `pkg/ui/format.go` or `pkg/ui/colors.go`

## Special Directories

**`.mini-code/` (project root):**
- Purpose: Runtime data — project SQLite DB, session snapshots, built binaries
- Generated: Yes (auto-created at runtime)
- Committed: No (in `.gitignore`)
- Global counterpart: `$HOME/.mini-code/memory.db`

**`.planning/codebase/`:**
- Purpose: GSD codebase analysis documents
- Generated: Yes (by `/gsd-map-codebase` commands)
- Committed: Yes

**`.claude/`:**
- Purpose: Claude Code configuration — skills, agents, hooks, commands
- Generated: Partially
- Committed: Yes

---

*Structure analysis: 2026-04-20*
