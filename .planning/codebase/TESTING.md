# Testing Patterns

**Analysis Date:** 2026-04-20

## Test Framework

**Runner:**
- Go standard `testing` package — no third-party test runner
- Config: none (uses `go test` defaults)

**Assertion Library:**
- None — raw `t.Fatal`, `t.Fatalf`, `t.Error`, `t.Errorf` throughout

**Run Commands:**
```bash
go test ./...                                     # Run all tests
go test -v ./pkg/agent                            # Run a single package (verbose)
go test -v ./pkg/tools -run TestWriteFile         # Run a single test function
go test -cover ./...                              # Coverage report
go test -race ./...                               # Race condition detection
```

## Test File Organization

**Location:**
- Co-located with source: `pkg/agent/base.go` + `pkg/agent/base_test.go`
- All test files in same directory as production code

**Naming:**
- `{source_file}_test.go` when testing one file: `edit_test.go`, `prompt_test.go`
- `{feature}_test.go` for cross-file tests: `workspace_test.go`, `agents_test.go`, `session_snapshot_test.go`

**Package:**
- White-box (internal) tests use the same package: `package agent`, `package tools`, `package supervisor`
- Black-box (external) tests use `_test` suffix package: `package agent_test`, `package orchestrator_test`, `package tools_test`, `package memory_test`
- Use black-box when testing public API contracts; white-box when testing internal helpers or unexported functions

## Test Structure

**Suite Organization:**
```go
// Single test function per scenario
func TestWriteFile_SuccessCase(t *testing.T) { ... }
func TestWriteFile_BlocksOutsideWorkspace(t *testing.T) { ... }

// Table-driven tests for multiple related inputs
func TestReplaceInFileSpecialCharacters(t *testing.T) {
    tests := []struct {
        name     string
        content  string
        old      string
        new      string
        expected string
    }{
        {"tabs", "col1\tcol2", "\t", ",", "col1,col2"},
        {"quotes", `say "hello"`, `"hello"`, `"hi"`, `say "hi"`},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) { ... })
    }
}
```

**Patterns:**
- Setup: `t.TempDir()` for isolated file system state; `chdirForTest(t, workspace)` + `defer restore()` to change working directory
- Teardown: `defer store.Close()`, `defer delete(tools.Executors, toolName)`, `defer restore()`
- Subtests: `t.Run(name, func(t *testing.T) {...})` for grouped related cases
- `t.Helper()` used in test utilities: `chdirForTest` calls `t.Helper()` to improve error location reporting

**chdirForTest helper (defined in `pkg/tools/edit_test.go`, used across tools tests):**
```go
func chdirForTest(t *testing.T, dir string) func() {
    t.Helper()
    original, err := os.Getwd()
    if err != nil { t.Fatalf("getwd failed: %v", err) }
    if err := os.Chdir(dir); err != nil { t.Fatalf("chdir failed: %v", err) }
    return func() {
        if err := os.Chdir(original); err != nil { t.Fatalf("restore chdir failed: %v", err) }
    }
}
```

## Mocking

**Framework:**
- No mock generation library (no `gomock`, `testify/mock`) — handwritten fakes

**Patterns:**

Interface-satisfying fake structs (capture calls, return pre-set responses):
```go
// In pkg/agent/base_test.go
type fakeChatCompletionClientBase struct {
    responses []openai.ChatCompletionResponse
    requests  []openai.ChatCompletionRequest
    err       error
}

func (f *fakeChatCompletionClientBase) Chat(_ context.Context, req openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error) {
    f.requests = append(f.requests, req)
    if f.err != nil { return openai.ChatCompletionResponse{}, f.err }
    if len(f.responses) == 0 { return openai.ChatCompletionResponse{}, fmt.Errorf("unexpected call") }
    resp := f.responses[0]
    f.responses = f.responses[1:]
    return resp, nil
}
```

Queue-based response fakes — responses consumed one by one to simulate multi-turn conversations:
```go
fakeClient := &fakeChatCompletionClientBase{
    responses: []openai.ChatCompletionResponse{
        // Turn 1: model calls a tool
        {Choices: []openai.ChatCompletionChoice{{Message: openai.ChatCompletionMessage{ToolCalls: [...]}}}}
        // Turn 2: model gives final reply
        {Choices: []openai.ChatCompletionChoice{{Message: ..., FinishReason: openai.FinishReasonStop}}},
    },
}
```

Tool executor injection for testing:
```go
// In pkg/agent/base_test.go
tools.Executors[toolName] = func(args interface{}) (interface{}, error) {
    return "tool-output", nil
}
defer delete(tools.Executors, toolName)
```

Compile-time interface verification in test files:
```go
var _ provider.Provider = (*promptCaptureProvider)(nil)
```

**What to Mock:**
- `provider.Provider` interface — always mock in agent unit tests (never make real LLM calls)
- `channel.Channel` interface — mock in orchestrator tests
- `tools.Executors` map — inject test executors directly for tool call flow tests
- `memory.Store` — use real in-memory SQLite with `t.TempDir()` (fast enough, no mock needed)

**What NOT to Mock:**
- `memory.Store` — use real SQLite with temp dirs; FTS5 behavior must be tested against real DB
- File system — use `t.TempDir()` + real OS calls; no FS abstraction layer

## Fixtures and Factories

**Test Data:**
```go
// Inline message construction — no shared fixtures
messages := []openai.ChatCompletionMessage{
    {Role: openai.ChatMessageRoleSystem, Content: "你是助手"},
    {Role: openai.ChatMessageRoleUser, Content: "执行工具"},
}

// Session snapshot construction in pkg/orchestrator/session_snapshot_test.go
snapshot := session.ExportSnapshot()
```

**Setup helpers:**
```go
// In pkg/tools/memory_tools_test.go
func setupMemoryTools(t *testing.T) *memory.Store {
    dir := t.TempDir()
    store, err := memory.Open(
        filepath.Join(dir, "memory.db"),
        filepath.Join(dir, "project.db"),
    )
    if err != nil { t.Fatalf("Open store: %v", err) }
    t.Cleanup(func() { store.Close() })
    tools.SetMemoryStore(store)
    return store
}
```

**Location:**
- No shared fixtures directory — each test creates its own data inline or via local helpers
- Platform-specific command helpers in `pkg/tools/run_shell_test.go`: `normalEchoCommand()`, `stderrEchoCommand()` return OS-appropriate commands

## Coverage

**Requirements:** No enforced minimum — no CI coverage gate detected

**View Coverage:**
```bash
go test -cover ./...
go test -coverprofile=coverage.out ./... && go tool cover -html=coverage.out
```

## Test Types

**Unit Tests:**
- Dominant pattern — all packages have unit tests
- Test single functions/methods with fake dependencies
- Examples: `pkg/agent/base_test.go`, `pkg/textutil/utf8_test.go`, `pkg/tools/edit_test.go`

**Integration Tests:**
- Real SQLite DB with `t.TempDir()` in `pkg/memory/store_test.go` and `pkg/memory/prompt_test.go`
- Real file system operations in all `pkg/tools/*_test.go` files
- `pkg/supervisor/worker_runtime_test.go` uses callback injection to test build + restart flow end-to-end

**E2E Tests:**
- Not present as a formal suite
- `pkg/supervisor/control_test.go` tests TCP control protocol with in-process fakes

## Common Patterns

**Async/Concurrent Testing:**
```go
// In pkg/agent/engine_test.go — verifies concurrent tool calls preserve message order
func TestRunTurnConcurrentToolCallsPreserveOrder(t *testing.T) {
    var callOrder []string
    var mu sync.Mutex
    tools.Executors["tool_a"] = func(args interface{}) (interface{}, error) {
        mu.Lock()
        callOrder = append(callOrder, "tool_a")
        mu.Unlock()
        return "tool_a_result", nil
    }
    // ... verify tool result messages in original index order
}
```

**Error Testing:**
```go
// Always check both: error is non-nil AND error message contains expected text
_, err := ReplaceInFile(ReplaceInFileArguments{Path: "../escape.txt", ...})
if err == nil {
    t.Fatal("expected replace outside workspace to fail")
}
if !strings.Contains(err.Error(), "路径超出工作区") {
    t.Fatalf("unexpected error: %v", err)
}
```

**Table-Driven Boundary Tests:**
```go
// Security boundary testing in pkg/tools/workspace_test.go
traversalPaths := []struct {
    name string
    path string
}{
    {"single_parent", ".."},
    {"parent_with_file", filepath.Join("..", "escape.txt")},
    {"windows_parent", "..\\escape.txt"},
}
for _, tt := range traversalPaths {
    t.Run(tt.name, func(t *testing.T) { ... })
}
```

**Subtests for Parametric Scenarios:**
```go
// In pkg/channel/telegram/telegram_test.go
for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
        segments := splitMessage(tt.text, tt.maxLen)
        if len(segments) < tt.minSegments { t.Errorf(...) }
    })
}
```

**State Verification Pattern:**
- Assert count before AND after mutation:
```go
initialCount := len(s.Messages())
s.AppendUserMessage("test message")
if len(s.Messages()) != initialCount+1 {
    t.Errorf("expected %d messages, got %d", initialCount+1, len(s.Messages()))
}
```

**Cross-Platform Shell Commands:**
- Always use helper functions instead of hardcoded shell commands in tests:
```go
func normalEchoCommand() string {
    if runtime.GOOS == "windows" { return `echo hello` }
    return `printf 'hello\n'`
}
```

---

*Testing analysis: 2026-04-20*
