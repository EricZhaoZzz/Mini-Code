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

## 📦 安装

### 前置要求

- Go 1.21 或更高版本
- OpenAI 兼容的 API 密钥

### 从源码构建

```bash
# 克隆仓库
git clone https://github.com/yourusername/mini-code.git
cd mini-code

# 安装依赖
go mod download

# 编译
go build -o mini-code ./cmd/agent
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
# 直接运行
go run ./cmd/agent

# 或使用编译后的二进制
./mini-code
```

启动后直接输入任务即可开始对话。

## 🛠️ 内置工具

Mini-Code 提供以下工具供 AI 助手使用：

### 文件操作

| 工具 | 描述 |
|------|------|
| `write_file` | 创建或修改文件 |
| `read_file` | 读取文件内容 |
| `list_files` | 列出目录下的文件 |
| `search_in_files` | 在目录下搜索文本 |
| `replace_in_file` | 局部替换文件内容 |
| `rename_file` | 重命名/移动文件或目录 |
| `delete_file` | 删除文件或目录 |
| `copy_file` | 复制文件或目录 |
| `create_directory` | 创建目录 |
| `get_file_info` | 获取文件详细信息（大小、修改时间、SHA256等） |

### 网络操作

| 工具 | 描述 |
|------|------|
| `download_file` | 下载远程文件到本地 |

### Shell 命令

| 工具 | 描述 |
|------|------|
| `run_shell` | 执行 shell 命令 |

### Git 操作

| 工具 | 描述 |
|------|------|
| `git_status` | 查看 Git 仓库状态 |
| `git_diff` | 查看 Git 差异 |
| `git_log` | 查看 Git 提交历史 |

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
├── cmd/agent/           # CLI 入口点
│   ├── main.go          # 程序入口，交互式命令行界面
│   └── main_test.go     # 主程序测试
├── pkg/
│   ├── agent/           # 核心引擎逻辑
│   │   ├── engine.go    # ClawEngine 实现，对话循环
│   │   └── engine_test.go
│   ├── provider/        # LLM 提供商客户端
│   │   └── client.go
│   ├── tools/           # 工具注册与执行
│   │   ├── registry.go  # 工具定义和注册
│   │   ├── file.go      # 文件读写工具
│   │   ├── file_ops.go  # 文件操作工具
│   │   ├── edit.go      # 文件编辑工具
│   │   ├── search.go    # 搜索工具
│   │   ├── system.go    # 系统工具
│   │   ├── git.go       # Git 工具
│   │   └── download.go  # 下载工具
│   └── ui/              # 用户界面组件
│       ├── colors.go    # 颜色定义
│       ├── format.go    # 格式化输出
│       ├── input.go     # 输入处理
│       ├── progress.go  # 进度显示
│       └── tools.go     # 工具显示
├── .env.example         # 环境变量模板
├── go.mod
├── go.sum
└── README.md
```

## 🔧 开发指南

### 运行测试

```bash
# 运行所有测试
go test ./...

# 运行特定包的测试
go test -v ./pkg/tools

# 运行特定测试函数
go test -v ./pkg/tools -run TestWriteFile

# 显示测试覆盖率
go test -cover ./...
```

### 代码格式化

```bash
gofmt -w ./cmd ./pkg
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