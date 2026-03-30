# Mini-Code

<p align="center">
  <strong>一个本地运行的中文 AI 编程助手（CLI）</strong>
</p>

<p align="center">
  基于 OpenAI 兼容接口，支持多种 LLM 提供商，通过丰富的工具集帮助您完成软件开发任务。
</p>

---

## ✨ 特性

- 🔧 **丰富的工具集** - 20+ 内置工具，覆盖文件操作、代码编辑、搜索、Shell 命令、Git 操作等
- 🚀 **并发执行** - 智能并发调用多个工具，提升执行效率
- 📡 **流式输出** - 实时显示 AI 响应，无需等待完整回复
- 🖥️ **交互式界面** - 支持命令历史、自动补全、彩色输出
- 🔐 **安全设计** - 工作区路径限制，防止目录遍历攻击
- 🌐 **OpenAI 兼容** - 支持任何 OpenAI 兼容的 API 端点
- 🖧 **跨平台支持** - 支持 Windows、Linux、macOS

### ✅ 已实现功能

| 功能模块 | 状态 | 说明 |
|---------|------|------|
| **核心引擎** | ✅ | 对话循环、流式响应、并发工具调用、轮次限制 |
| **文件读写** | ✅ | 创建、读取、修改、删除文件和目录 |
| **文件操作** | ✅ | 重命名、复制、移动、获取文件信息（大小、SHA256等） |
| **文本搜索** | ✅ | 在目录中搜索文本内容 |
| **Shell 命令** | ✅ | 执行系统命令，支持 Windows/Unix |
| **Git 操作** | ✅ | status、diff、log 查看 |
| **网络下载** | ✅ | 下载远程文件到本地 |
| **长期记忆** | ✅ | 三级记忆存储（project/user/global），FTS5 全文检索 |
| **Agent 专化** | ✅ | CoderAgent、ReviewerAgent、ResearcherAgent 三类专化 Agent |
| **Telegram Bot** | ✅ | Telegram 频道支持，流式消息更新，命令处理 |
| **命令历史** | ✅ | readline 支持，历史记录持久化 |
| **自动补全** | ✅ | 内置命令自动补全 |
| **彩色输出** | ✅ | 工具调用状态、成功/失败提示 |
| **中断处理** | ✅ | Ctrl+C 优雅退出 |
| **调试日志** | ✅ | LM_DEBUG 开启详细日志 |

### 🚧 计划中功能

| 功能模块 | 状态 | 说明 |
|---------|------|------|
| **Markdown 渲染** | 📋 | 终端 Markdown 格式化显示 |
| **代码高亮** | 📋 | 代码块语法高亮 |
| **会话持久化** | 📋 | 保存/恢复对话历史 |
| **多会话管理** | 📋 | 支持多个独立会话 |
| **Token 统计** | 📋 | 显示 Token 使用量和成本 |
| **版本管理** | 📋 | 自动版本检测和更新提示 |
| **插件系统** | 📋 | 自定义工具扩展机制 |
| **LSP 集成** | 💡 | 语言服务器协议支持（代码补全、跳转） |
| **多模型切换** | 💡 | 运行时切换不同模型 |

> 图例：✅ 已完成 | 📋 计划中 | 💡 构想中

## 📦 安装

### 前置要求

- Go 1.26.1 或更高版本
- OpenAI 兼容的 API 密钥
- 确保已启用 Go 模块模式（GO111MODULE=on）

### 从源码构建

```bash
# 克隆仓库
git clone https://github.com/yourusername/mini-code.git
cd mini-code

# 确保启用 Go 模块模式（推荐设置为全局配置）
go env -w GO111MODULE=on

# 安装依赖
go mod download

# 编译
go build -o mini-code.exe ./cmd/agent
```

### 常见问题

**问题：编译报错 "package mini-code is not in std"**

**原因：** Go 模块模式未启用，编译器尝试在标准库中查找包。

**解决方案：**
```bash
# 方法1：设置全局 Go 模块模式（推荐）
go env -w GO111MODULE=on

# 方法2：仅为当前项目设置（在项目根目录创建 go.env 文件）
echo "GO111MODULE=on" > go.env
```

**问题：找不到依赖包**

**解决方案：**
```bash
# 清理并重新下载依赖
go clean -modcache
go mod download
```

## 🚀 快速开始

### 1. 配置环境变量

复制 `.env.example` 为 `.env` 并填写：

```bash
cp .env.example .env
```

编辑 `.env` 文件：

```env
# 必需配置
API_KEY=your-api-key-here
BASE_URL=https://api.openai.com/v1
MODEL=gpt-4

# 可选配置
LM_LOG_LEVEL=normal
LM_DEBUG=
LM_MAX_TURNS=50
```

### 2. 运行

```bash
# 直接运行（开发模式）
go run ./cmd/agent

# 或使用编译后的二进制
# Windows:
mini-code.exe
# Linux/macOS:
./mini-code
```

启动后直接输入任务即可开始对话。

### 💡 Windows 用户注意

在 Windows 上运行时：
- 编译生成的可执行文件为 `mini-code.exe`
- Shell 命令会使用 Windows CMD 语法
- 路径分隔符自动适配为反斜杠 `\`

## 🛠️ 内置工具

Mini-Code 提供以下工具供 AI 助手使用：

### 文件操作

| 工具 | 描述 |
|------|------|
| `write_file` | 创建或修改文件（完整内容写入） |
| `read_file` | 读取文件内容 |
| `list_files` | 列出目录下的文件 |
| `search_in_files` | 在目录下搜索文本 |
| `replace_in_file` | 局部替换文件内容（只替换第一个匹配项） |
| `rename_file` | 重命名/移动文件或目录 |
| `delete_file` | 删除文件或目录（需确认参数） |
| `copy_file` | 复制文件或目录 |
| `create_directory` | 创建目录（支持递归创建父目录） |
| `get_file_info` | 获取文件详细信息（大小、修改时间、SHA256等） |

### 网络操作

| 工具 | 描述 |
|------|------|
| `download_file` | 下载远程文件到本地（支持超时设置） |

### Shell 命令

| 工具 | 描述 |
|------|------|
| `run_shell` | 执行 shell 命令（Windows 使用 CMD，Linux/macOS 使用 Bash） |

### Git 操作

| 工具 | 描述 |
|------|------|
| `git_status` | 查看 Git 仓库状态 |
| `git_diff` | 查看 Git 差异（支持已暂存和未暂存变更） |
| `git_log` | 查看 Git 提交历史 |

## 🤖 Telegram Bot

Mini-Code 支持 Telegram Bot 模式，让你可以通过 Telegram 使用 AI 编程助手。

### 配置

在 `.env` 文件中添加以下配置：

```env
# Telegram Bot Token（从 @BotFather 获取）
TELEGRAM_BOT_TOKEN=your-bot-token

# 允许使用的用户 ID（逗号分隔，为空则允许所有用户）
TELEGRAM_ALLOWED_USERS=12345678,87654321
```

### 获取 User ID

1. 在 Telegram 搜索 `@userinfobot`
2. 发送 `/start`
3. 它会回复你的 User ID

或者临时设置 `TELEGRAM_DEBUG=1`，Bot 会在控制台打印发送消息的用户 ID。

### 启动 Telegram Bot

```bash
# 直接运行
go run ./cmd/telegram

# 或编译后运行
go build -o mini-code-telegram ./cmd/telegram
./mini-code-telegram
```

### Telegram 命令

| 命令 | 说明 |
|------|------|
| `/start` | 显示欢迎信息和当前工作区 |
| `/help` | 显示帮助信息 |
| `/reset` | 重置当前会话（保留长期记忆） |
| `/memory` | 查看已保存的记忆 |
| `/status` | 显示当前任务状态 |
| `/cancel` | 取消当前正在执行的任务 |

### 特性

- **流式更新** - 每 1.5 秒刷新消息内容，避免消息轰炸
- **完成通知** - 任务完成后发送新消息，触发手机推送
- **附件支持** - 支持发送文件/图片作为附件
- **并发安全** - 每个 chat_id 独立会话，互不干扰
- **白名单控制** - 可限制允许访问的用户

## ⌨️ 交互命令

在交互提示符下可使用以下命令：

| 命令 | 别名 | 描述 |
|------|------|------|
| `help` | `h`, `?` | 显示帮助信息 |
| `clear` | `cls` | 清屏 |
| `new` | `n`, `reset`, `r` | 清空会话上下文 |
| `history` | `hist` | 显示对话消息数 |
| `version` | `v` | 显示版本信息 |
| `exit` | `quit`, `q` | 退出程序 |

## ⚙️ 配置选项

### 环境变量

| 变量名 | 必需 | 描述 |
|--------|------|------|
| `API_KEY` | ✅ | API 密钥 |
| `BASE_URL` | ✅ | OpenAI 兼容接口地址 |
| `MODEL` | ✅ | 模型 ID |
| `LM_LOG_LEVEL` | ❌ | 日志级别：`minimal`/`normal`/`verbose`（或 `0`/`1`/`2`） |
| `LM_DEBUG` | ❌ | 开启调试日志，写入 `lm_debug.log` |
| `LM_MAX_TURNS` | ❌ | 最大工具调用轮次（默认 50，0 表示无限制） |

### 日志级别

- `minimal` / `0` - 最精简日志
- `normal` / `1` - 正常日志（默认）
- `verbose` / `2` - 详细日志

## 📁 项目结构

```
mini-code/
├── cmd/
│   ├── agent/           # CLI 入口点
│   │   ├── main.go      # 程序入口，交互式命令行界面
│   │   └── main_test.go # 主程序测试
│   └── telegram/        # Telegram Bot 入口点
│       └── main.go      # Telegram Bot 启动程序
├── pkg/
│   ├── agent/           # 核心引擎逻辑
│   │   ├── base.go      # BaseAgent 实现，对话循环
│   │   ├── coder.go     # CoderAgent 专化
│   │   ├── reviewer.go  # ReviewerAgent 专化
│   │   └── researcher.go # ResearcherAgent 专化
│   ├── channel/         # 输入输出频道
│   │   ├── types.go     # Channel 接口定义
│   │   ├── cli/         # CLI 频道实现
│   │   └── telegram/    # Telegram 频道实现
│   ├── memory/          # 记忆系统
│   │   ├── store.go     # SQLite 存储层
│   │   ├── longterm.go  # 长期记忆（FTS5）
│   │   └── prompt.go    # 系统提示注入
│   ├── orchestrator/    # 消息编排
│   │   ├── orchestrator.go # 会话管理，消息路由
│   │   ├── router.go    # Agent 路由策略
│   │   └── session.go   # 会话状态管理
│   ├── provider/        # LLM 提供商客户端
│   │   └── openai.go    # OpenAI 兼容客户端
│   ├── tools/           # 工具注册与执行
│   │   ├── registry.go  # 工具定义和注册
│   │   ├── file.go      # 文件读写工具
│   │   ├── file_ops.go  # 文件操作工具
│   │   ├── edit.go      # 文件编辑工具
│   │   ├── git.go       # Git 工具
│   │   ├── download.go  # 下载工具
│   │   ├── memory_tools.go # 记忆工具
│   │   └── dispatch.go  # Agent 调度工具
│   └── ui/              # 用户界面组件
│       ├── colors.go    # 颜色定义
│       ├── format.go    # 格式化输出
│       └── progress.go  # 进度显示
├── .env.example         # 环境变量模板
├── go.mod
├── go.sum
└── README.md
```

## 🔧 开发指南

### 环境要求

- Go 1.26.1 或更高版本
- Git（用于版本控制）

### 运行测试

```bash
# 运行所有测试
go test ./...

# 运行特定包的测试并显示详细信息
go test -v ./pkg/tools

# 运行特定测试函数
go test -v ./pkg/tools -run TestWriteFile

# 显示测试覆盖率
go test -cover ./...

# 生成覆盖率报告
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out -o coverage.html
```

### 代码格式化

```bash
# 格式化所有代码
gofmt -w ./cmd ./pkg

# 检查代码规范
go vet ./...
```

### 添加新工具

1. 在 `pkg/tools/` 创建实现文件
2. 定义参数结构体（使用 `json` 标签）
3. 实现执行函数，签名为 `ToolExecutor`
4. 在 `registry.go` 的 `init()` 中调用 `register()` 注册

示例：

```go
// pkg/tools/my_tool.go
package tools

type MyToolArguments struct {
    Path string `json:"path" jsonschema:"required,description=文件路径"`
}

func MyTool(args interface{}) (interface{}, error) {
    // 实现逻辑
    return "result", nil
}

// pkg/tools/registry.go
func init() {
    // ...
    register("my_tool", "工具描述", MyToolArguments{}, MyTool)
}
```

## 🔒 安全考虑

- **工作区限制** - 所有文件操作限制在工作区内，防止目录遍历攻击
- **路径验证** - 工具执行前验证路径有效性
- **敏感信息** - 调试日志可能包含敏感信息，请注意保管
- **API 密钥** - 不要提交 `.env` 文件到版本控制

## 📝 工作流程

Mini-Code 遵循以下工作流程：

1. **理解项目** - 浏览目录结构，搜索关键代码，阅读关键文件
2. **分析需求** - 理解任务目标，识别修改范围
3. **执行修改** - 最小化修改，保持代码一致性
4. **验证结果** - 运行测试验证正确性

### 工作原理

Mini-Code 作为一个 AI 编程助手，通过以下方式工作：

1. **对话交互** - 用户输入任务描述或问题
2. **工具调用** - AI 根据需求调用相应的工具（文件操作、Shell 命令等）
3. **执行反馈** - 工具执行结果反馈给 AI，AI 根据结果继续工作
4. **循环迭代** - 持续这个循环直到任务完成

### 工具执行特性

- **并发执行** - 多个独立的工具调用会并发执行，提高效率
- **安全限制** - 所有文件操作限制在工作区内，防止意外访问系统文件
- **错误处理** - 工具执行失败时会返回详细错误信息，AI 可以根据错误调整策略

## 🤝 贡献

欢迎贡献代码！请遵循：

1. Fork 本仓库
2. 创建特性分支 (`git checkout -b feature/amazing-feature`)
3. 提交更改 (`git commit -m 'feat: add amazing feature'`)
4. 推送到分支 (`git push origin feature/amazing-feature`)
5. 创建 Pull Request

### 提交格式

遵循 [Conventional Commits](https://www.conventionalcommits.org/)：

- `feat:` 新功能
- `fix:` 修复 bug
- `test:` 测试相关
- `docs:` 文档更新
- `refactor:` 代码重构

## 📄 许可证

MIT License

---

<p align="center">
  Made with ❤️ for developers
</p>