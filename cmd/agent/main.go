package main

import (
	"context"
	"bufio"
	"fmt"
	"mini-code/pkg/agent"
	"mini-code/pkg/memory"
	"mini-code/pkg/tools"
	"mini-code/pkg/ui"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/ergochat/readline"
	"github.com/sashabaranov/go-openai"
)

func main() {
	loadDotEnv(".env")

	apiKey := os.Getenv("API_KEY")
	if apiKey == "" {
		ui.PrintError("缺少环境变量 API_KEY")
		os.Exit(1)
	}

	baseURL := os.Getenv("BASE_URL")
	if baseURL == "" {
		ui.PrintError("缺少环境变量 BASE_URL")
		os.Exit(1)
	}

	modelID := os.Getenv("MODEL")
	if modelID == "" {
		ui.PrintError("缺少环境变量 MODEL")
		os.Exit(1)
	}

	config := openai.DefaultConfig(apiKey)
	config.BaseURL = baseURL

	client := openai.NewClientWithConfig(config)
	engine := agent.NewClawEngine(client, modelID)

	// 初始化 Memory Store
	homeDir, _ := os.UserHomeDir()
	globalDBPath := filepath.Join(homeDir, ".mini-code", "memory.db")
	projectDBPath := filepath.Join(".", ".mini-code", "project.db")

	// 确保目录存在
	os.MkdirAll(filepath.Dir(globalDBPath), 0o755)
	os.MkdirAll(filepath.Dir(projectDBPath), 0o755)

	memStore, err := memory.Open(globalDBPath, projectDBPath)
	if err != nil {
		ui.PrintError("Memory 初始化失败: %v", err)
		// 非致命错误，继续运行（无记忆模式）
		memStore = nil
	}
	if memStore != nil {
		defer memStore.Close()
		tools.SetMemoryStore(memStore) // 注入工具层
		engine.SetMemoryStore(memStore)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 设置全局 Esc 监控器的取消函数
	ui.GlobalEscMonitor.SetCancelFunc(cancel)

	// 监听 Ctrl+C 信号
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGINT)
	go func() {
		<-sigChan
		fmt.Println()
		ui.PrintInfo("收到中断信号，正在退出...")
		cancel()
		os.Exit(0)
	}()

	printWelcome()

	// 使用 readline 处理输入
	rl, err := readline.NewEx(&readline.Config{
		Prompt:          ui.SprintColor(ui.User, "➜ ") + ui.SprintColor(ui.Bold, ""),
		HistoryFile:     ".mini-code-history",
		HistoryLimit:    1000,
		AutoComplete:    getCompleter(),
		InterruptPrompt: "^C",
		EOFPrompt:       "exit",
	})
	if err != nil {
		ui.PrintError("初始化输入失败: %v", err)
		os.Exit(1)
	}
	defer rl.Close()

	for {
		line, err := rl.Readline()
		if err != nil {
			// 处理 EOF (Ctrl+D) 或错误
			fmt.Println()
			ui.PrintInfo("再见！感谢使用 Mini-Code。")
			break
		}

		input := strings.TrimSpace(line)
		if input == "" {
			continue
		}

		// 处理内置命令
		if handleBuiltinCommand(input, engine) {
			continue
		}

		// 检查上下文是否已取消
		if ctx.Err() != nil {
			ui.PrintInfo("程序已退出。")
			break
		}

		engine.AddUserMessage(input)

		// 创建任务上下文（每个任务独立）
		taskCtx, taskCancel := context.WithCancel(context.Background())
		
		// 设置 Esc 监控器的取消函数
		ui.GlobalEscMonitor.SetCancelFunc(taskCancel)

		// 显示助手标签
		ui.PrintAssistantLabel()

		// 创建流式输出处理器
		streaming := ui.NewStreamingText()

		// 启动 Esc 监控
		ui.GlobalEscMonitor.Start()

		_, err = engine.RunTurnStream(taskCtx, func(content string, done bool) error {
			if !done && content != "" {
				streaming.Write(content)
			}
			return nil
		})

		// 停止 Esc 监控
		ui.GlobalEscMonitor.Stop()

		// 检查是否被用户中止
		if err != nil && taskCtx.Err() != nil {
			taskCancel()
			fmt.Println()
			ui.PrintInfo("任务已中止。输入新问题继续对话...")
			continue
		}
		taskCancel()

		if err != nil {
			fmt.Println()
			ui.PrintError("运行失败: %v", err)
			continue
		}

		streaming.Complete()
		fmt.Println()
	}
}

// getCompleter 获取自动补全器
func getCompleter() *readline.PrefixCompleter {
	return readline.NewPrefixCompleter(
		readline.PcItem("help"),
		readline.PcItem("h"),
		readline.PcItem("?"),
		readline.PcItem("exit"),
		readline.PcItem("quit"),
		readline.PcItem("q"),
		readline.PcItem("clear"),
		readline.PcItem("cls"),
		readline.PcItem("reset"),
		readline.PcItem("r"),
		readline.PcItem("new"),
		readline.PcItem("n"),
		readline.PcItem("history"),
	)
}

// handleBuiltinCommand 处理内置命令，返回 true 表示已处理（不需要继续执行）
func handleBuiltinCommand(input string, engine *agent.ClawEngine) bool {
	cmd := strings.ToLower(input)

	switch cmd {
	case "exit", "quit", "q":
		ui.PrintSuccess("再见！感谢使用 Mini-Code。")
		os.Exit(0)
		return true

	case "help", "h", "?":
		fmt.Println(ui.HelpPanel())
		return true

	case "clear", "cls":
		clearScreen()
		return true

	case "reset", "r", "new", "n":
		ui.PrintInfo("正在重置对话历史...")
		engine.Reset()
		ui.PrintSuccess("对话已重置，会话上下文已清空。")
		return true

	case "history", "hist":
		count := engine.GetMessageCount()
		ui.PrintInfo("对话消息数: %d", count)
		return true

	case "version", "v":
		ui.PrintInfo("Mini-Code v1.0")
		return true
	}

	return false
}

func printWelcome() {
	fmt.Println(ui.WelcomeBanner())
	fmt.Println()
	ui.PrintDim("命令: help 显示帮助 | exit 退出程序")
	ui.PrintDim("提示: 直接输入你的任务开始对话...")
	fmt.Println()
}

func clearScreen() {
	fmt.Print("\033[2J\033[H")
	ui.PrintSuccess("屏幕已清除。")
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