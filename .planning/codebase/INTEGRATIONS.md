# External Integrations

**Analysis Date:** 2026-04-20

## APIs & External Services

**LLM / AI Provider:**
- OpenAI-compatible API - All AI inference (chat completions, streaming)
  - SDK/Client: `github.com/sashabaranov/go-openai v1.41.2`
  - Implementation: `pkg/provider/openai.go` (`OpenAIProvider`)
  - Auth: `API_KEY` environment variable
  - Endpoint: `BASE_URL` environment variable (defaults to `https://api.openai.com/v1` per `.env.example`; any OpenAI-compatible endpoint works, e.g., local Ollama, Azure OpenAI, DeepSeek, etc.)
  - Model: `MODEL` environment variable
  - Methods used: `CreateChatCompletion` (non-streaming) and `CreateChatCompletionStream` (streaming)

**Messaging Platform:**
- Telegram Bot API - Bot channel for user interaction
  - SDK/Client: `github.com/go-telegram-bot-api/telegram-bot-api/v5 v5.5.1`
  - Implementation: `pkg/channel/telegram/telegram.go`
  - Auth: `TELEGRAM_BOT_TOKEN` environment variable (obtained from @BotFather)
  - Access control: `TELEGRAM_ALLOWED_USERS` (comma-separated int64 user IDs; empty = allow all)
  - Polling interval: long-polling with 60s timeout via `GetUpdatesChan`
  - Stream refresh: edits existing messages every 1500ms during streaming responses
  - File handling: downloads Telegram documents/photos to local temp dir (`os.TempDir()/mini-code-telegram/`)

**HTTP downloads:**
- Generic HTTP/HTTPS - `download_file` tool downloads arbitrary URLs
  - Implementation: `pkg/tools/download.go`
  - Client: standard `net/http` with configurable timeout (default 300s)
  - No auth (public URLs only)

## Data Storage

**Databases:**
- SQLite (two independent databases per deployment)
  - Driver: `modernc.org/sqlite v1.47.0` (pure Go, no CGO required)
  - Global DB: `~/.mini-code/memory.db` — long-term memory (user/global scope) + session memory
  - Project DB: `.mini-code/project.db` (relative to workspace) — long-term memory (project scope)
  - Both use WAL journal mode and foreign keys
  - Both use FTS5 virtual tables for full-text search on memory content
  - Schema auto-migrated on `memory.Open()` in `pkg/memory/store.go`

**File Storage:**
- Local filesystem only
  - Tool writes go to workspace directory (validated by `pkg/tools/workspace.go`)
  - Atomic writes via `pkg/tools/atomic_write_unix.go` / `pkg/tools/atomic_write_windows.go`
  - Hot-restart binaries: `.mini-code/releases/<buildID>/` in workspace
  - Telegram temp files: `os.TempDir()/mini-code-telegram/`
  - CLI readline history: `.mini-code-history` in working directory

**Caching:**
- None (no Redis, Memcached, or in-memory cache layer)

## Authentication & Identity

**Auth Provider:**
- No third-party auth provider
- LLM API: API key via `API_KEY` env var passed to `openai.DefaultConfig(apiKey)`
- Telegram: bot token via `TELEGRAM_BOT_TOKEN` env var; user allowlist via `TELEGRAM_ALLOWED_USERS`
- Workspace security: path traversal prevention in `pkg/tools/workspace.go`

## Monitoring & Observability

**Error Tracking:**
- None (no Sentry, Datadog, etc.)

**Logs:**
- Debug log file: `lm_debug.log` in working directory when `LM_DEBUG` env var is set (non-empty)
- Implementation: `newDebugLogger()` in `pkg/agent/base.go`
- Log level: `LM_LOG_LEVEL` env var (`minimal`|`normal`|`verbose`, or `0`|`1`|`2`)
- Telegram debug: `TELEGRAM_DEBUG` env var prints `[DEBUG] Chat ID / User ID / Username` to stdout (`pkg/channel/telegram/telegram.go`)

## CI/CD & Deployment

**Hosting:**
- Local machine only (no cloud deployment detected)

**CI Pipeline:**
- Not detected (no `.github/`, `.gitlab-ci.yml`, etc.)

**Hot Restart (in-process upgrade):**
- `cmd/agent` only: supervisor spawns new standby worker, exchanges session snapshot via temp JSON file, promotes standby via TCP control channel
- New binary built with `go build -o .mini-code/releases/<buildID>/<name> ./cmd/agent` at runtime
- Control protocol: TCP loopback (`pkg/supervisor/control.go`, `pkg/supervisor/worker_runtime.go`)

## Environment Configuration

**Required env vars:**
- `API_KEY` - LLM provider API key
- `BASE_URL` - LLM provider base URL (e.g., `https://api.openai.com/v1`)
- `MODEL` - Model ID string (e.g., `gpt-4`, `deepseek-coder`, etc.)
- `TELEGRAM_BOT_TOKEN` - Required only for `cmd/telegram`

**Optional env vars:**
- `LM_LOG_LEVEL` - `minimal`|`normal`|`verbose`
- `LM_DEBUG` - Any non-empty value enables debug log file
- `LM_MAX_TURNS` - Max tool call rounds per task (default 50; 0 = unlimited)
- `TELEGRAM_ALLOWED_USERS` - Comma-separated Telegram user IDs
- `TELEGRAM_DEBUG` - Any non-empty value prints sender info to stdout
- `LM_VERBOSE` - Any non-empty value enables verbose BaseAgent logging

**Secrets location:**
- `.env` file in working directory (gitignored; template at `.env.example`)
- Never committed to version control

## Webhooks & Callbacks

**Incoming:**
- None (Telegram uses long-polling, not webhooks)

**Outgoing:**
- None

---

*Integration audit: 2026-04-20*
