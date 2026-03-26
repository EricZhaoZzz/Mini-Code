package main

import (
	"bufio"
	"context"
	"fmt"
	"mini-claw/pkg/agent"
	_ "mini-claw/pkg/tools"
	"os"
	"strings"

	"github.com/sashabaranov/go-openai"
)

func main() {
	apiKey := os.Getenv("DASHSCOPE_API_KEY")
	if apiKey == "" {
		fmt.Println("缺少环境变量 DASHSCOPE_API_KEY")
		os.Exit(1)
	}

	baseURL := os.Getenv("DASHSCOPE_BASE_URL")
	if baseURL == "" {
		baseURL = "https://coding.dashscope.aliyuncs.com/v1"
	}

	modelID := os.Getenv("DASHSCOPE_MODEL")
	if modelID == "" {
		modelID = "qwen3-coder-next"
	}

	config := openai.DefaultConfig(apiKey)
	config.BaseURL = baseURL

	client := openai.NewClientWithConfig(config)
	engine := agent.NewClawEngine(client, modelID)
	ctx := context.Background()

	fmt.Println("Mini-Claw 已启动，输入你的任务。输入 exit 或 quit 退出。")

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("> ")

		if !scanner.Scan() {
			fmt.Println("\n输入结束，程序退出。")
			break
		}

		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}

		if input == "exit" || input == "quit" {
			fmt.Println("Bye.")
			break
		}

		engine.AddUserMessage(input)

		reply, err := engine.RunTurn(ctx)
		if err != nil {
			fmt.Printf("运行失败: %v\n", err)
			continue
		}

		fmt.Printf("\nMini-Claw: %s\n\n", reply)
	}

	if err := scanner.Err(); err != nil {
		fmt.Printf("读取输入失败: %v\n", err)
	}
}
