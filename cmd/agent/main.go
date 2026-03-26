package main

import (
	"context"
	"mini-claw/pkg/agent"
	_ "mini-claw/pkg/tools"

	"github.com/sashabaranov/go-openai"
)

func main() {

	apiKey := "sk-sp-5e6f8a1ab7cd4b0b9e596a0b26058c54"
	config := openai.DefaultConfig(apiKey)
	config.BaseURL = "https://coding.dashscope.aliyuncs.com/v1"

	ModelID := "qwen3-coder-next"

	client := openai.NewClientWithConfig(config)

	ctx := context.Background()
	engine := agent.NewClawEngine(client, ModelID)

	// engine.Run(ctx, "请用中文写一个hello world程序")
	engine.Run(ctx, "在当前目录下创建一个 test 文件夹，里面写一个 data.txt，内容是 'Go Agent 运行正常'，然后读取它确认内容。")
}
