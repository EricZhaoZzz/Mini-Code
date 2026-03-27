# Repository Guidelines

## Project Structure & Module Organization

`mini-claw` 是一个本地运行的中文 AI 编程助手（CLI），采用 Go 语言开发。

### 目录结构
```
mini-claw/
├── cmd/agent/           # CLI 入口点
│   ├── main.go          # 程序入口，交互式命令行界面
│   └── main_test.go     # 主程序测试
├── pkg/
│   ├── agent/           # 核心引擎逻辑
│   │   ├── engine.go    # ClawEngine 实现，对话循环
│   │   └── engine_test.go
│   ├── provider/        # LLM 提供商客户端
│   │   └── client.go    # OpenAI 兼容客户端
│   └── tools/           # 工具注册与执行
│       ├── registry.go  # 工具定义和注册
│       ├── file.go      # 文件读写工具
│       ├── workspace.go # 工作区路径解析
│       ├── edit.go      # 文件编辑工具
│       ├── system.go    # 系统工具（shell 执行等）
│       ├── workspace_test.go
│       └── edit_test.go
├── .env.example         # 环境变量模板
├── .gitignore
├── go.mod
├── go.sum
└── README.md
```

### 模块划分原则
- `pkg/<domain>` 放置领域逻辑，每个包职责单一
- 避免在 `main.go` 中添加跨领域逻辑
- 工具实现按功能分文件：文件操作、编辑、系统命令

## Build, Test, and Development Commands

### 基础命令
- `go run ./cmd/agent`: 运行本地开发版本
- `go build ./cmd/agent`: 编译可执行文件，检查编译错误
- `go test ./...`: 运行所有包测试
- `gofmt -w ./cmd ./pkg`: 格式化所有 Go 源文件

### 运行单个测试
```bash
# 运行特定测试函数
go test -v ./pkg/tools -run TestWriteFileBlocksOutsideWorkspace

# 运行特定包的测试
go test -v ./pkg/tools

# 运行包含特定关键词的测试
go test -v ./pkg/agent -run TestRunTurn

# 并行运行测试
go test -parallel 4 ./...

# 显示测试覆盖率
go test -cover ./...
```

### 调试和日志
- 设置 `LM_DEBUG=1` 开启调试日志（写入 `lm_debug.log`）
- 设置 `LM_LOG_LEVEL=verbose` 获取详细日志
- 日志级别：`minimal` | `normal` | `verbose`（或 `0` | `1` | `2`）

## Coding Style & Naming Conventions

### Go 语言规范
遵循 idiomatic Go 风格：
- 使用 `gofmt` 格式化代码
- Tab 缩进，行宽建议不超过 100 字符
- 导入分组：标准库 → 第三方库 → 内部包

### 命名约定
- **包名**：简短、小写、单数形式（如 `tools`, `agent`）
- **导出标识符**：`PascalCase`（如 `ClawEngine`, `WriteFile`）
- **未导出标识符**：`camelCase`（如 `resolveWorkspacePath`）
- **测试函数**：`Test<功能>_<场景>` 格式（如 `TestWriteFileBlocksOutsideWorkspace`）
- **常量**：`PascalCase` 或 `camelCase` 根据可见性

### 文件组织
- 按功能分文件，避免大文件
- 测试文件与源文件同目录，命名 `*_test.go`
- 工具注册在 `registry.go`，实现分文件

### 错误处理
- 返回 `error` 而非 panic
- 使用 `fmt.Errorf` 添加上下文信息
- 工具执行器返回 `(interface{}, error)` 元组
- 路径安全检查返回中文错误信息（如 `"路径超出工作区"`）

### 工具系统
工具通过 `registry.go` 注册，每个工具：
1. 定义参数结构体（使用 `json` 标签）
2. 实现 `ToolExecutor` 函数签名
3. 在 `init()` 中调用 `register()` 注册

当前工具集：
- `write_file`: 创建或修改文件
- `read_file`: 读取文件内容
- `list_files`: 列出目录文件
- `search_in_files`: 文件内容搜索
- `run_shell`: 执行 shell 命令
- `replace_in_file`: 文件内容替换（仅首个匹配）

## Testing Guidelines

### 测试策略
- 为新功能添加单元测试，修复 bug 时添加回归测试
- 使用表驱动测试处理多场景（如 `TestParentTraversalPathsBlocked`）
- 测试函数命名体现行为：`Test<组件>_<场景>`
- 使用 `t.TempDir()` 创建临时目录，测试后自动清理

### 测试模式
```go
// 表驱动测试示例
func TestFeature_Scenario(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        want    string
        wantErr bool
    }{
        {"case1", "input1", "output1", false},
        {"case2", "input2", "", true},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := Feature(tt.input)
            if (err != nil) != tt.wantErr {
                t.Errorf("Feature() error = %v, wantErr %v", err, tt.wantErr)
                return
            }
            if got != tt.want {
                t.Errorf("Feature() = %v, want %v", got, tt.want)
            }
        })
    }
}
```

### 测试辅助
- 使用 `chdirForTest()` 临时切换工作目录（`pkg/tools/edit_test.go`）
- 使用 `captureOutput()` 捕获标准输出（`cmd/agent/main_test.go`）
- Mock 客户端实现 `chatCompletionClient` 接口

## Commit & Pull Request Guidelines

### Commit 格式
遵循 Conventional Commits：
- `feat(agent): 添加新功能`
- `fix(tools): 修复路径检查问题`
- `test(agent): 增加测试覆盖`
- `docs: 更新 README`
- `refactor: 重构代码结构`

### PR 要求
- 简短摘要，说明变更目的
- 关联 issue（如有）
- 测试说明：如何验证变更
- 示例输出：对于行为变更，提供示例提示或终端输出

## Security & Configuration Tips

### 环境变量
- 复制 `.env.example` 为 `.env` 并填写
- 必需变量：`API_KEY`, `BASE_URL`, `MODEL`
- 可选变量：`LM_LOG_LEVEL`, `LM_DEBUG`

### 安全实践
- 不要提交真实 API 密钥或 `.env` 文件
- 不要提交生成的二进制文件（`*.exe`）
- 调试日志可能包含敏感信息，注意保管
- 工作区路径检查防止目录遍历攻击

### 代码审查要点
- 检查 `cmd/agent/main.go` 中的演示提示，避免提交本地专用值
- 验证所有工具路径检查是否完善
- 确保错误信息不泄露内部路径信息

## 架构扩展指南

### 添加新工具
1. 在 `pkg/tools/` 创建实现文件
2. 定义参数结构体和执行函数
3. 在 `registry.go` 的 `init()` 中注册
4. 添加相应测试文件

### 添加新提供商
1. 在 `pkg/provider/` 实现客户端
2. 确保实现 `chatCompletionClient` 接口
3. 在 `cmd/agent/main.go` 中初始化

### 修改系统提示
编辑 `pkg/agent/engine.go` 中的 `buildSystemPrompt()` 函数，注意保持中文响应要求。
