package main

import (
	"bufio"
	"context"
	"fmt"
	"mini-code/pkg/agent"
	"mini-code/pkg/channel/cli"
	"mini-code/pkg/memory"
	"mini-code/pkg/orchestrator"
	"mini-code/pkg/provider"
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
	baseURL := os.Getenv("BASE_URL")
	modelID := os.Getenv("MODEL")
	if apiKey == "" || baseURL == "" || modelID == "" {
		ui.PrintError("缺少必要环境变量 API_KEY / BASE_URL / MODEL")
		os.Exit(1)
	}

	cfg := openai.DefaultConfig(apiKey)
	cfg.BaseURL = baseURL
	p := provider.NewOpenAIProvider(cfg, modelID)

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
	}

	// 创建 readline 实例
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

	cliCh := cli.New(rl)
	orch := orchestrator.New(memStore)

	// 创建 Agent 工厂函数
	factory := func(agentType string) agent.Agent {
		switch agentType {
		case "reviewer":
			return agent.NewReviewerAgent(p, modelID)
		case "researcher":
			return agent.NewResearcherAgent(p, modelID)
		default:
			return agent.NewCoderAgent(p, modelID)
		}
	}

	// 注入 dispatch_agent 工厂的工厂函数
	tools.SetDispatchFactory(func(role string, task string) (string, error) {
		subAgent := factory(role)
		messages := []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: "你是专业的 AI 助手。"},
			{Role: openai.ChatMessageRoleUser, Content: task},
		}
		reply, _, err := subAgent.Run(context.Background(), messages, nil)
		return reply, err
	})

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

	// 在 goroutine 中启动 CLI 监听
	go cliCh.Start(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-cliCh.Messages():
			if !ok {
				ui.PrintInfo("再见！感谢使用 Mini-Code。")
				return
			}

			// 处理内置命令
			if handleBuiltinCommand(msg.Text, orch, cliCh.ChannelID(), "local") {
				continue
			}

			// 创建任务上下文
			taskCtx, taskCancel := context.WithCancel(ctx)
			
			// 设置 Esc 监控器的取消函数
			ui.GlobalEscMonitor.SetCancelFunc(taskCancel)

			// 启动 Esc 监控
			ui.GlobalEscMonitor.Start()

			if err := orch.Handle(taskCtx, msg, factory, cliCh); err != nil {
				ui.PrintError("运行失败: %v", err)
			}
			fmt.Println()

			// 停止 Esc 监控
			ui.GlobalEscMonitor.Stop()
			taskCancel()
		}
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

// handleBuiltinCommand 处理内置命令，返回 true 表示已处理
func handleBuiltinCommand(input string, orch *orchestrator.Orchestrator, channelID, userID string) bool {
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
		session := orch.GetOrCreateSession(channelID, userID)
		session.Reset()
		// 重新注入记忆
		orch.RefreshSessionPrompt(channelID, userID)
		ui.PrintSuccess("对话已重置，会话上下文已清空。")
		return true

	case "history", "hist":
		session := orch.GetOrCreateSession(channelID, userID)
		count := session.MessageCount()
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