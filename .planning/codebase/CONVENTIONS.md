# Coding Conventions

**Analysis Date:** 2026-04-20

## Naming Patterns

**Files:**
- Snake_case for multi-word files: `base_test.go`, `atomic_write_unix.go`, `worker_runtime.go`
- Single-word files for primary types: `agent.go`, `session.go`, `router.go`
- Platform-specific build tags via filename suffix: `atomic_write_unix.go` / `atomic_write_windows.go`
- Test files co-located with source: `session.go` + `session_snapshot_test.go`

**Types and Structs:**
- PascalCase for exported types: `BaseAgent`, `SessionSnapshot`, `ToolExecutor`
- PascalCase for exported interfaces: `Agent`, `Channel`, `Provider`, `RestartRuntime`
- Suffix `Arguments` for tool parameter structs: `WriteFileArguments`, `RunShellArguments`, `DispatchAgentArguments`
- Suffix `Record` for database row types: `MemoryRecord`, `SessionRecord`

**Functions and Methods:**
- PascalCase for exported: `NewBaseAgent()`, `BuildSystemPromptWithMemory()`, `GetOrCreateSession()`
- camelCase for unexported: `parseArgs()`, `resolveWorkspacePath()`, `newDebugLogger()`
- Constructor prefix `New`: `NewBaseAgent`, `NewCoderAgent`, `NewRouter`, `NewSwitchCoordinator`
- Method names describe action + target: `AppendUserMessage`, `ExportSnapshot`, `RefreshSessionPrompt`

**Variables and Constants:**
- camelCase for unexported: `currentLogLevel`, `reviewerKeywords`, `updateInterval`
- PascalCase for exported constants: `DefaultMaxTurns`, `StreamModeCLI`, `MessageTypeShutdown`
- `const` iota blocks for enumerations: `LogLevelMinimal`, `LogLevelNormal`, `LogLevelVerbose`

**Test Fakes:**
- Prefix `fake` for test doubles: `fakeChannel`, `fakeAgent`, `fakeRestartRuntime`, `fakeChatCompletionClientBase`

## Code Style

**Formatting:**
- Standard `gofmt` formatting throughout
- `go vet ./...` enforced

**Linting:**
- No `.eslintrc` or `golangci-lint` config detected — relies on `go vet` + `gofmt`

**Struct Tags:**
- JSON schema annotations on tool argument structs use both `json:` and `jsonschema_description:` tags:
  ```go
  type WriteFileArguments struct {
      Path    string `json:"path" validate:"required" jsonschema:"required" jsonschema_description:"要写入的工作区内相对路径"`
      Content string `json:"content" jsonschema:"required" jsonschema_description:"文件完整内容"`
  }
  ```
- Required fields get both `validate:"required"` and `jsonschema:"required"`

## Import Organization

**Order:**
1. Standard library
2. Internal packages (`mini-code/pkg/...`)
3. Third-party packages (`github.com/...`)

**Example from `pkg/agent/helpers.go`:**
```go
import (
    "context"
    "encoding/json"
    "fmt"
    "io"
    "log"
    "mini-code/pkg/textutil"
    "mini-code/pkg/tools"
    "mini-code/pkg/ui"
    "os"
    "strings"
    "sync"
    "time"

    "github.com/sashabaranov/go-openai"
)
```

**Module Path:**
- Module path is `mini-code` (not `github.com/...`): imports like `mini-code/pkg/agent`

**Path Aliases:**
- None used — direct import paths only

## Error Handling

**Patterns:**
- All errors wrapped with `fmt.Errorf("context: %w", err)` for chain visibility
- Chinese error messages for user-facing errors (tool results): `"写入文件失败: %w"`, `"路径超出工作区"`, `"命令执行错误: %w"`
- English error messages for internal/programmer errors: `"chat error: %w"`, `"stream error: %w"`, `"unexpected call"`
- Tools return `(interface{}, error)` — partial output is returned alongside an error when both are available (e.g., `RunShell` returning stdout even on non-zero exit)
- Error strings contain the failed operation + wrapped cause: `fmt.Errorf("open global db: %w", err)`

**Early Return:**
```go
func WriteFile(args interface{}) (interface{}, error) {
    var params WriteFileArguments
    if err := parseArgs(args, &params); err != nil {
        return nil, err
    }
    targetPath, err := resolveWorkspacePath(params.Path)
    if err != nil {
        return nil, err
    }
    // ... continue only when clean
}
```

**Context Cancellation:**
- Always checked before heavy operations in goroutines with `select { case <-ctx.Done(): ... default: }`
- `ctx.Err()` checked after `Agent.Run` returns to distinguish cancel vs real error

## Logging

**Debug Logger:**
- `LM_DEBUG=1` writes structured JSON to `lm_debug.log` via `*log.Logger` in `pkg/agent/helpers.go`
- `nil` check before every call: `if a.debugLogger == nil { return }`
- Tag + JSON payload format: `[15:04:05.000] LM INPUT\n{...}`

**Log Level:**
- Configured via `LM_LOG_LEVEL` env var: `minimal|normal|verbose`
- `LogLevel` iota type in `pkg/agent/helpers.go`, global `currentLogLevel` var
- `init()` reads env var once at startup

**User-facing output:**
- `pkg/ui` package handles all terminal output — never raw `fmt.Println` for UI
- Exception: `fmt.Println()` used for blank-line separators around tool call display

## Comments

**When to Comment:**
- Interface definitions always get doc comments explaining purpose and contract
- Non-obvious design decisions get inline comments (e.g., `// SQLite 单写连接`, `// Phase 3: specialized agent system prompt`)
- Phase/migration annotations mark legacy code: `// Phase 1 遗留兼容层`, `// deleted at end of Phase 1`
- Chinese preferred for business logic comments; English for code-level technical annotations

**Example interface comment:**
```go
// Agent 接口：接受完整消息历史，返回最终响应内容
// 消息历史由调用方（Orchestrator）管理，Agent 不持有状态
type Agent interface { ... }
```

## Function Design

**Size:**
- Small focused functions; complex logic extracted to helpers (e.g., `executeToolCallsConcurrently`, `consumeStream`, `filteredDefinitions`)
- `init()` blocks used only for one-time registration (tool registry, log level)

**Parameters:**
- Constructors accept only essential dependencies: `NewBaseAgent(p provider.Provider, model string, allowedTools []string)`
- Tool executors use untyped `interface{}` input + `parseArgs()` for decoding
- Context always first parameter for functions that may block or be cancelled

**Return Values:**
- Constructors return `*ConcreteType` (not interface) to allow field configuration post-construction
- Tool executors return `(interface{}, error)` — callers type-assert result to `string` or concrete type
- Multi-return errors follow Go convention: named results only when needed for deferred logic

## Interface Design

**Definition:**
- Interfaces defined at the consumer side (e.g., `channel.Channel` in `pkg/channel/types.go`)
- Small, purposeful interfaces: `Provider` (2 methods), `Agent` (3 methods), `Channel` (7 methods)
- Compile-time interface verification: `var _ provider.Provider = (*legacyClientAdapter)(nil)`

## Module Design

**Exports:**
- Only export what's needed; unexported helpers are the norm
- No barrel `index.go` files — import specific packages

**Package Responsibility:**
- One primary responsibility per package: `pkg/tools` handles tool execution, `pkg/memory` handles storage, `pkg/provider` wraps LLM SDK
- Cross-cutting utilities isolated: `pkg/textutil` (UTF-8), `pkg/ui` (terminal output)

---

*Convention analysis: 2026-04-20*
