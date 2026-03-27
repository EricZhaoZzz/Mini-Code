# Mini-Code

<p align="center">
  <strong>A Local Chinese AI Programming Assistant (CLI)</strong>
</p>

<p align="center">
  Built on OpenAI-compatible APIs, supports multiple LLM providers, and helps you accomplish software development tasks through a rich set of tools.
</p>

---

## ✨ Features

- 🔧 **Rich Tool Set** - 20+ built-in tools covering file operations, code editing, search, shell commands, Git operations, and more
- 🚀 **Concurrent Execution** - Intelligently executes multiple tool calls concurrently for improved efficiency
- 📡 **Streaming Output** - Real-time AI response display without waiting for complete replies
- 🖥️ **Interactive Interface** - Supports command history, auto-completion, and colored output
- 🔐 **Security Design** - Workspace path restrictions to prevent directory traversal attacks
- 🌐 **OpenAI Compatible** - Supports any OpenAI-compatible API endpoint

### ✅ Implemented Features

| Module | Status | Description |
|--------|--------|-------------|
| **Core Engine** | ✅ | Conversation loop, streaming response, concurrent tool calls, turn limits |
| **File I/O** | ✅ | Create, read, modify, delete files and directories |
| **File Operations** | ✅ | Rename, copy, move, get file info (size, SHA256, etc.) |
| **Text Search** | ✅ | Search text content in directories |
| **Shell Commands** | ✅ | Execute system commands, Windows/Unix support |
| **Git Operations** | ✅ | status, diff, log viewing |
| **Network Download** | ✅ | Download remote files to local |
| **Command History** | ✅ | readline support, persistent history |
| **Auto-completion** | ✅ | Built-in command auto-completion |
| **Colored Output** | ✅ | Tool call status, success/failure indicators |
| **Interrupt Handling** | ✅ | Graceful exit with Ctrl+C |
| **Debug Logging** | ✅ | Detailed logs with LM_DEBUG |

### 🚧 Planned Features

| Module | Status | Description |
|--------|--------|-------------|
| **Markdown Rendering** | 📋 | Terminal Markdown formatting |
| **Syntax Highlighting** | 📋 | Code block syntax highlighting |
| **Session Persistence** | 📋 | Save/restore conversation history |
| **Multi-session Management** | 📋 | Support multiple independent sessions |
| **Token Statistics** | 📋 | Display token usage and cost |
| **Version Management** | 📋 | Auto version detection and update prompts |
| **Plugin System** | 📋 | Custom tool extension mechanism |
| **LSP Integration** | 💡 | Language Server Protocol support (code completion, navigation) |
| **Multi-model Switching** | 💡 | Runtime model switching |

> Legend: ✅ Completed | 📋 Planned | 💡 Conceptual

## 📦 Installation

### Prerequisites

- Go 1.21 or higher
- OpenAI-compatible API key

### Build from Source

```bash
# Clone the repository
git clone https://github.com/yourusername/mini-code.git
cd mini-code

# Install dependencies
go mod download

# Build
go build -o mini-code ./cmd/agent
```

## 🚀 Quick Start

### 1. Configure Environment Variables

Copy `.env.example` to `.env` and fill in:

```bash
cp .env.example .env
```

Edit `.env` file:

```env
# Required configuration
API_KEY=your-api-key-here
BASE_URL=https://api.openai.com/v1
MODEL=gpt-4

# Optional configuration
LM_LOG_LEVEL=normal
LM_DEBUG=
LM_MAX_TURNS=50
```

### 2. Run

```bash
# Run directly
go run ./cmd/agent

# Or use compiled binary
./mini-code
```

Simply enter your task after startup to begin the conversation.

## 🛠️ Built-in Tools

Mini-Code provides the following tools for the AI assistant:

### File Operations

| Tool | Description |
|------|-------------|
| `write_file` | Create or modify files |
| `read_file` | Read file contents |
| `list_files` | List files in a directory |
| `search_in_files` | Search text in files |
| `replace_in_file` | Replace text in files (first match only) |
| `rename_file` | Rename/move files or directories |
| `delete_file` | Delete files or directories |
| `copy_file` | Copy files or directories |
| `create_directory` | Create directories |
| `get_file_info` | Get detailed file information (size, modification time, SHA256, etc.) |

### Network Operations

| Tool | Description |
|------|-------------|
| `download_file` | Download remote files to local |

### Shell Commands

| Tool | Description |
|------|-------------|
| `run_shell` | Execute shell commands |

### Git Operations

| Tool | Description |
|------|-------------|
| `git_status` | View Git repository status |
| `git_diff` | View Git differences |
| `git_log` | View Git commit history |

## ⌨️ Interactive Commands

The following commands are available in the interactive prompt:

| Command | Aliases | Description |
|---------|---------|-------------|
| `help` | `h`, `?` | Show help information |
| `clear` | `cls` | Clear screen |
| `new` | `n`, `reset`, `r` | Clear conversation context |
| `history` | `hist` | Show message count |
| `version` | `v` | Show version information |
| `exit` | `quit`, `q` | Exit program |

## ⚙️ Configuration Options

### Environment Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `API_KEY` | ✅ | API key |
| `BASE_URL` | ✅ | OpenAI-compatible API endpoint |
| `MODEL` | ✅ | Model ID |
| `LM_LOG_LEVEL` | ❌ | Log level: `minimal`/`normal`/`verbose` (or `0`/`1`/`2`) |
| `LM_DEBUG` | ❌ | Enable debug logging, writes to `lm_debug.log` |
| `LM_MAX_TURNS` | ❌ | Maximum tool call turns (default 50, 0 for unlimited) |

### Log Levels

- `minimal` / `0` - Minimal logging
- `normal` / `1` - Normal logging (default)
- `verbose` / `2` - Verbose logging

## 📁 Project Structure

```
mini-code/
├── cmd/agent/           # CLI entry point
│   ├── main.go          # Program entry, interactive CLI interface
│   └── main_test.go     # Main program tests
├── pkg/
│   ├── agent/           # Core engine logic
│   │   ├── engine.go    # ClawEngine implementation, conversation loop
│   │   └── engine_test.go
│   ├── provider/        # LLM provider client
│   │   └── client.go
│   ├── tools/           # Tool registration and execution
│   │   ├── registry.go  # Tool definitions and registration
│   │   ├── file.go      # File read/write tools
│   │   ├── file_ops.go  # File operation tools
│   │   ├── edit.go      # File editing tools
│   │   ├── search.go    # Search tools
│   │   ├── system.go    # System tools
│   │   ├── git.go       # Git tools
│   │   └── download.go  # Download tools
│   └── ui/              # User interface components
│       ├── colors.go    # Color definitions
│       ├── format.go    # Format output
│       ├── input.go     # Input handling
│       ├── progress.go  # Progress display
│       └── tools.go     # Tool display
├── .env.example         # Environment variable template
├── go.mod
├── go.sum
└── README.md
```

## 🔧 Development Guide

### Run Tests

```bash
# Run all tests
go test ./...

# Run tests for specific package
go test -v ./pkg/tools

# Run specific test function
go test -v ./pkg/tools -run TestWriteFile

# Show test coverage
go test -cover ./...
```

### Code Formatting

```bash
gofmt -w ./cmd ./pkg
```

### Adding New Tools

1. Create implementation file in `pkg/tools/`
2. Define parameter struct (use `json` tags)
3. Implement executor function with `ToolExecutor` signature
4. Register in `registry.go`'s `init()` by calling `register()`

Example:

```go
// pkg/tools/my_tool.go
package tools

type MyToolArguments struct {
    Path string `json:"path" jsonschema:"required,description=File path"`
}

func MyTool(args interface{}) (interface{}, error) {
    // Implementation logic
    return "result", nil
}

// pkg/tools/registry.go
func init() {
    // ...
    register("my_tool", "Tool description", MyToolArguments{}, MyTool)
}
```

## 🔒 Security Considerations

- **Workspace Restriction** - All file operations are restricted within the workspace to prevent directory traversal attacks
- **Path Validation** - Path validity is verified before tool execution
- **Sensitive Information** - Debug logs may contain sensitive information, handle with care
- **API Keys** - Do not commit `.env` files to version control

## 📝 Workflow

Mini-Code follows this workflow:

1. **Understand Project** - Browse directory structure, search key code, read important files
2. **Analyze Requirements** - Understand task goals, identify modification scope
3. **Execute Modifications** - Minimal changes, maintain code consistency
4. **Verify Results** - Run tests to validate correctness

## 🤝 Contributing

Contributions are welcome! Please follow these steps:

1. Fork this repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit changes (`git commit -m 'feat: add amazing feature'`)
4. Push to branch (`git push origin feature/amazing-feature`)
5. Create a Pull Request

### Commit Format

Follow [Conventional Commits](https://www.conventionalcommits.org/):

- `feat:` New feature
- `fix:` Bug fix
- `test:` Test related
- `docs:` Documentation update
- `refactor:` Code refactoring

## 📄 License

MIT License

---

<p align="center">
  Made with ❤️ for developers
</p>