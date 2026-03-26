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

	apiKey := "sk-sp-5e6f8a1ab7cd4b0b9e596a0b26058c54"
	config := openai.DefaultConfig(apiKey)
	config.BaseURL = "https://coding.dashscope.aliyuncs.com/v1"

	ModelID := "qwen3-coder-next"

	client := openai.NewClientWithConfig(config)

	ctx := context.Background()

	engine := agent.NewClawEngine(client, ModelID)

	fmt.Println("Mini-Claw 已启动，输入你的任务。输入 exit 或 quit 退出。")

	scanner := bufio.NewScanner(os.Stdin)

	for {
		fmt.Print("> ")

		if !scanner.Scan() {
			fmt.Println("\n输入已结束, 程序退出。")
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
