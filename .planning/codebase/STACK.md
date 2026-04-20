# Technology Stack

**Analysis Date:** 2026-04-20

## Languages

**Primary:**
- Go 1.26.1 - All application code (`cmd/`, `pkg/`)

**Secondary:**
- None detected

## Runtime

**Environment:**
- Go runtime 1.26.1

**Package Manager:**
- Go modules (`go mod`)
- Lockfile: `go.sum` (present)

## Frameworks

**Core:**
- None (standard library + thin packages; no web framework)
- `github.com/sashabaranov/go-openai v1.41.2` - OpenAI-compatible LLM client, used as the core AI provider abstraction in `pkg/provider/openai.go`
- `github.com/go-telegram-bot-api/telegram-bot-api/v5 v5.5.1` - Telegram Bot SDK, used in `pkg/channel/telegram/telegram.go`

**CLI / Interactive:**
- `github.com/ergochat/readline v0.1.0` - Interactive REPL with history and autocomplete in `cmd/agent/main.go`
- `github.com/fatih/color v1.18.0` - Terminal color output in `pkg/ui/colors.go`
- `golang.org/x/term v0.38.0` - Terminal state detection

**Testing:**
- Standard `testing` package (no third-party test framework detected)

**Build/Dev:**
- Standard `go build` / `go test` / `go vet` / `gofmt`

## Key Dependencies

**Critical:**
- `github.com/sashabaranov/go-openai v1.41.2` - Entire LLM interaction layer; wraps `Chat` and `ChatStream` calls in `pkg/provider/interface.go`. All agent types use this.
- `modernc.org/sqlite v1.47.0` - Pure-Go SQLite driver for the two-tier memory system (`~/.mini-code/memory.db` + `.mini-code/project.db`) in `pkg/memory/store.go`
- `github.com/go-git/go-git/v5 v5.16.2` - Pure-Go Git implementation; powers `git_status`, `git_diff`, `git_log` tools in `pkg/tools/git.go`
- `github.com/invopop/jsonschema v0.13.0` - Generates JSON Schema from Go structs for OpenAI function calling tool definitions in `pkg/tools/system.go`

**Infrastructure:**
- `github.com/google/renameio/v2 v2.0.2` - Cross-platform atomic file rename; used in `pkg/tools/atomic_write_unix.go` (on Windows, platform-specific implementation in `atomic_write_windows.go`)
- `github.com/go-cmd/cmd v1.4.3` - Async shell command execution in `pkg/tools/file.go`
- `github.com/charlievieth/fastwalk v1.0.14` - Fast parallel directory traversal for `list_files` tool
- `github.com/denormal/go-gitignore v0.0.0-20180930084346-ae8ad1d07817` - Gitignore-aware file filtering in `pkg/tools/file.go`
- `github.com/gabriel-vasile/mimetype v1.4.13` - MIME type detection for files in `pkg/tools/file.go`
- `github.com/dustin/go-humanize v1.0.1` - Human-readable sizes/timestamps in `pkg/tools/git.go` and `pkg/tools/download.go`
- `github.com/go-playground/validator/v10 v10.30.1` - Struct validation for tool arguments in `pkg/tools/system.go`

## Configuration

**Environment:**
- All config via `.env` file (parsed manually by `loadDotEnv()` in both `cmd/agent/main.go` and `cmd/telegram/main.go`; no third-party dotenv library)
- Key required vars: `API_KEY`, `BASE_URL`, `MODEL`
- Optional vars: `LM_LOG_LEVEL`, `LM_DEBUG`, `LM_MAX_TURNS`, `TELEGRAM_BOT_TOKEN`, `TELEGRAM_ALLOWED_USERS`, `TELEGRAM_DEBUG`
- Template: `.env.example`

**Build:**
- `go.mod` / `go.sum` — module definition and lockfile
- No Makefile, Dockerfile, or CI config detected

## Platform Requirements

**Development:**
- Go 1.26.1+
- Cross-platform: Windows (`cmd /C` shell) and Unix (`/bin/sh -c` shell) handled via build tags in `pkg/tools/file.go`
- Atomic write split by platform: `pkg/tools/atomic_write_unix.go` and `pkg/tools/atomic_write_windows.go`

**Production:**
- Single self-contained binary (no external runtime)
- SQLite databases stored locally (`~/.mini-code/memory.db` global, `.mini-code/project.db` per-workspace)
- Hot-restart binaries written to `.mini-code/releases/<buildID>/` in workspace root

---

*Stack analysis: 2026-04-20*
