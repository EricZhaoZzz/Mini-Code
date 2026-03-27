# mini-claw

一个本地运行的中文 AI 编程助手（CLI）。

## 快速开始

1. 准备环境变量

将 `.env.example` 复制为 `.env`，并填写：

- `API_KEY`: 你的 API Key
- `BASE_URL`: OpenAI 兼容接口地址（例如 `https://api.openai.com/v1`）
- `MODEL`: 模型 ID（按你服务端实际支持填写）

2. 运行

```bash
go run ./cmd/agent
```

启动后输入任务即可对话。

## 内置命令

在交互提示符下可用：

- `help` / `h` / `?`: 显示帮助
- `clear` / `cls`: 清屏
- `new` / `n` / `reset` / `r`: 清空会话上下文
- `exit` / `quit` / `q`: 退出

## 可选环境变量

- `LM_LOG_LEVEL`: `minimal` | `normal` | `verbose`（或 `0` | `1` | `2`）
- `LM_DEBUG`: 设为任意非空值开启调试日志，会在当前工作目录写入 `lm_debug.log`（可能包含提示词、工具参数等敏感内容，请注意保管）

