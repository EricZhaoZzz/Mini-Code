package main

import (
	"bufio"
	"context"
	"fmt"
	"mini-claw/pkg/agent"
	_ "mini-claw/pkg/tools"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/sashabaranov/go-openai"
)

func main() {
	loadDotEnv(".env")

	apiKey := os.Getenv("API_KEY")
	if apiKey == "" {
		fmt.Println("❌ 缺少环境变量 API_KEY")
		os.Exit(1)
	}

	baseURL := os.Getenv("BASE_URL")
	if baseURL == "" {
		fmt.Println("❌ 缺少环境变量 BASE_URL")
		os.Exit(1)
	}

	modelID := os.Getenv("MODEL")
	if modelID == "" {
		fmt.Println("❌ 缺少环境变量 MODEL")
		os.Exit(1)
	}

	config := openai.DefaultConfig(apiKey)
	config.BaseURL = baseURL

	client := openai.NewClientWithConfig(config)
	engine := agent.NewClawEngine(client, modelID)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 监听 Ctrl+C 信号
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGINT)
	go func() {
		<-sigChan
		fmt.Println("\n\n👋 收到中断信号，正在退出...")
		cancel()
		os.Exit(0)
	}()

	printWelcome()

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("➜ ")

		if !scanner.Scan() {
			fmt.Println("\n\n👋 输入结束，程序退出。")
			break
		}

		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}

		// 处理内置命令
		if handleBuiltinCommand(input, engine) {
			continue
		}

		// 检查上下文是否已取消
		if ctx.Err() != nil {
			fmt.Println("\n\n👋 程序已退出。")
			break
		}

		// 显示思考中状态
		fmt.Println("⏳ 正在处理...")

		engine.AddUserMessage(input)

		reply, err := engine.RunTurn(ctx)
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			fmt.Printf("\n❌ 运行失败: %v\n\n", err)
			continue
		}

		fmt.Printf("\n🤖 Mini-Claw: %s\n\n", reply)
	}

	if err := scanner.Err(); err != nil {
		fmt.Printf("⚠️ 读取输入失败: %v\n", err)
	}
}

// handleBuiltinCommand 处理内置命令，返回 true 表示已处理（不需要继续执行）
func handleBuiltinCommand(input string, engine *agent.ClawEngine) bool {
	cmd := strings.ToLower(input)

	switch cmd {
	case "exit", "quit", "q":
		fmt.Println("\n👋 再见！感谢使用 Mini-Claw。")
		os.Exit(0)
		return true

	case "help", "h", "?":
		printHelp()
		return true

	case "clear", "cls":
		clearScreen()
		return true

	case "reset", "r", "new", "n":
		fmt.Println("🔄 正在重置对话历史...")
		engine.Reset()
		fmt.Println("✅ 对话已重置，会话上下文已清空。")
		return true

	case "history", "hist":
		// 可以后续添加查看历史功能
		fmt.Println("📜 对话历史功能开发中...")
		return true
	}

	return false
}

func printWelcome() {
	fmt.Println("")
	fmt.Println("╔═══════════════════════════════════════════════════════╗")
	fmt.Println("║                    Mini-Claw v1.0                      ║")
	fmt.Println("╠═══════════════════════════════════════════════════════╣")
	fmt.Println("║  🤖 AI 编程助手已启动                                  ║")
	fmt.Println("║                                                       ║")
	fmt.Println("║  命令:                                                ║")
	fmt.Println("║    help    - 显示帮助信息                             ║")
	fmt.Println("║    clear   - 清除屏幕                                 ║")
	fmt.Println("║    new     - 清空会话上下文                           ║")
	fmt.Println("║    reset   - 重置对话历史                             ║")
	fmt.Println("║    exit    - 退出程序                                 ║")
	fmt.Println("║                                                       ║")
	fmt.Println("║  输入你的任务开始对话...                               ║")
	fmt.Println("╚═══════════════════════════════════════════════════════╝")
	fmt.Println("")
}

func printHelp() {
	fmt.Println("")
	fmt.Println("📖 可用命令:")
	fmt.Println("  help    - 显示帮助信息")
	fmt.Println("  clear   - 清除屏幕")
	fmt.Println("  new     - 清空会话上下文")
	fmt.Println("  reset   - 重置对话历史")
	fmt.Println("  exit    - 退出程序")
	fmt.Println("")
	fmt.Println("💡 提示: 直接输入你的任务描述，AI 会帮助你完成。")
	fmt.Println("")
}

func clearScreen() {
	fmt.Print("\033[2J\033[H")
	fmt.Println("屏幕已清除。")
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
