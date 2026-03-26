package main

import (
	"bufio"
	"context"
	"fmt"
	"mini-claw/pkg/agent"
	_ "mini-claw/pkg/tools"
	"os"
	"path/filepath"
	"strings"

	"github.com/sashabaranov/go-openai"
)

func main() {
	loadDotEnv(".env")

	apiKey := os.Getenv("API_KEY")
	if apiKey == "" {
		fmt.Println("缺少环境变量 API_KEY")
		os.Exit(1)
	}

	baseURL := os.Getenv("BASE_URL")
	if baseURL == "" {
		fmt.Println("缺少环境变量 BASE_URL")
		os.Exit(1)
	}

	modelID := os.Getenv("MODEL")
	if modelID == "" {
		fmt.Println("缺少环境变量 MODEL")
		os.Exit(1)
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

func loadDotEnv(path string) {
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}

		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}

		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if _, exists := os.LookupEnv(key); exists {
			continue
		}

		_ = os.Setenv(key, value)
	}
}
