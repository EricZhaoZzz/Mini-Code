# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 项目简介

Mini-Code 是一个基于 Go 实现的本地 AI 编程助手 CLI 工具，通过 OpenAI 兼容接口与大语言模型交互，内置 13 个工具支持文件操作、Shell 执行、Git 操作等。

## 常用命令

```bash
# 构建
go build -o mini-code.exe ./cmd/agent   # Windows
go build -o mini-code ./cmd/agent       # Linux/macOS

# 运行（开发模式）
go run ./cmd/agent

# 测试
go test ./...                                          # 全部测试
go test -v ./pkg/tools                                 # 指定包（详细输出）
go test -v ./pkg/tools -run TestWriteFile              # 运行单个测试
go test -cover ./...                                   # 显示覆盖率

# 代码格式化与检查
gofmt -w ./cmd ./pkg
go vet ./...

# 安装依赖
go mod download
```

## 环境配置

复制 `.env.example` 为 `.env` 并填写：

```env
API_KEY=<your-api-key>
BASE_URL=https://api.openai.com/v1
MODEL=gpt-4

# 可选
LM_LOG_LEVEL=normal    # minimal|normal|verbose
LM_DEBUG=true          # 启用调试日志（写入 lm_debug.log）
LM_MAX_TURNS=50        # 最大工具调用轮次（0=不限）
```

## 架构概览

```
cmd/agent/main.go          CLI 入口，readline REPL，内置命令处理
pkg/agent/engine.go        核心对话引擎：消息管理、并发工具调度、流式响应
pkg/provider/client.go     OpenAI 兼容 API 封装（流式请求）
pkg/tools/                 工具系统
  registry.go              工具注册与 JSON Schema 定义生成
  workspace.go             工作区路径校验（防目录穿越）
  file.go / file_ops.go    文件读写、重命名、删除、复制等
  edit.go                  replace_in_file（首次匹配替换）
  search.go                跨文件文本搜索
  system.go                run_shell（跨平台命令执行）
  git.go                   git_status / git_diff / git_log
  download.go              文件下载
pkg/ui/                    终端 UI：颜色、格式化、进度、工具输出展示
```

**数据流：** 用户输入 → `engine.go` 组装消息 → provider 流式调用 LLM → 解析 tool_calls → 并发执行工具（`pkg/tools`）→ 将结果追加到消息历史 → 继续对话循环

## 关键设计决策

- **工作区安全**：所有文件操作通过 `workspace.go` 校验，限制在配置的工作目录内
- **并发工具执行**：同一轮次中的多个工具调用并发执行，结果汇总后再进入下一轮
- **原子文件写入**：`atomic_write_unix.go` / `atomic_write_windows.go` 分平台实现，防止写入中断导致文件损坏
- **跨平台 Shell**：`run_shell` 在 Windows 使用 `cmd /C`，Unix 使用 `/bin/sh -c`

## 工具系统扩展

新增工具需在 `pkg/tools/registry.go` 中注册，并实现对应处理函数。工具定义使用 `invopop/jsonschema` 自动从 Go 结构体生成 JSON Schema 参数定义。

## 提交规范

使用 Conventional Commits 格式：`feat:` / `fix:` / `docs:` / `refactor:` / `test:`
