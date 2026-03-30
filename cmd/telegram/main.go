package main

import (
	"bufio"
	"context"
	"fmt"
	"mini-code/pkg/agent"
	"mini-code/pkg/channel/telegram"
	"mini-code/pkg/memory"
	"mini-code/pkg/provider"
	"mini-code/pkg/tools"
	"mini-code/pkg/ui"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/sashabaranov/go-openai"
)

func main() {
	loadDotEnv(".env")

	// 读取配置
	apiKey := os.Getenv("API_KEY")
	baseURL := os.Getenv("BASE_URL")
	modelID := os.Getenv("MODEL")
	botToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	allowedUsersStr := os.Getenv("TELEGRAM_ALLOWED_USERS")

	if apiKey == "" || baseURL == "" || modelID == "" {
		ui.PrintError("缺少必要环境变量 API_KEY / BASE_URL / MODEL")
		os.Exit(1)
	}
	if botToken == "" {
		ui.PrintError("缺少 TELEGRAM_BOT_TOKEN 环境变量")
		os.Exit(1)
	}

	// 解析允许的用户列表
	var allowedUsers []int64
	if allowedUsersStr != "" {
		for _, uidStr := range strings.Split(allowedUsersStr, ",") {
			uidStr = strings.TrimSpace(uidStr)
			if uidStr == "" {
				continue
			}
			uid, err := strconv.ParseInt(uidStr, 10, 64)
			if err != nil {
				ui.PrintError("无效的用户 ID: %s", uidStr)
				os.Exit(1)
			}
			allowedUsers = append(allowedUsers, uid)
		}
	}

	// 创建 Provider
	cfg := openai.DefaultConfig(apiKey)
	cfg.BaseURL = baseURL
	p := provider.NewOpenAIProvider(cfg, modelID)

	// 初始化 Memory Store
	homeDir, _ := os.UserHomeDir()
	globalDBPath := filepath.Join(homeDir, ".mini-code", "memory.db")
	projectDBPath := filepath.Join(".", ".mini-code", "project.db")

	os.MkdirAll(filepath.Dir(globalDBPath), 0o755)
	os.MkdirAll(filepath.Dir(projectDBPath), 0o755)

	memStore, err := memory.Open(globalDBPath, projectDBPath)
	if err != nil {
		ui.PrintError("Memory 初始化失败: %v", err)
		memStore = nil
	}
	if memStore != nil {
		defer memStore.Close()
		tools.SetMemoryStore(memStore)
	}

	// 创建 Telegram Runner
	runner, err := telegram.NewRunner(telegram.Config{
		Token:        botToken,
		AllowedUsers: allowedUsers,
	}, memStore)
	if err != nil {
		ui.PrintError("Telegram Bot 初始化失败: %v", err)
		os.Exit(1)
	}

	// 设置 Agent 工厂
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
	runner.SetAgentFactory(factory)

	// 注入 dispatch_agent 工厂函数
	tools.SetDispatchFactory(func(role string, task string) (string, error) {
		subAgent := factory(role)
		messages := []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: "你是专业的 AI 助手。"},
			{Role: openai.ChatMessageRoleUser, Content: task},
		}
		reply, _, err := subAgent.Run(context.Background(), messages, nil)
		return reply, err
	})

	// 获取 Bot 信息
	botInfo, err := runner.GetChannel().GetBotInfo()
	if err != nil {
		ui.PrintError("获取 Bot 信息失败: %v", err)
	} else {
		ui.PrintSuccess("Telegram Bot 已启动: %s", botInfo)
	}

	if len(allowedUsers) > 0 {
		ui.PrintInfo("白名单用户数: %d", len(allowedUsers))
	} else {
		ui.PrintInfo("未设置白名单，允许所有用户访问")
	}

	// 创建上下文
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 监听信号
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		fmt.Println()
		ui.PrintInfo("收到终止信号，正在关闭...")
		cancel()
	}()

	// 启动 Bot
	ui.PrintSuccess("正在启动 Telegram Bot...")
	if err := runner.Start(ctx); err != nil && ctx.Err() == nil {
		ui.PrintError("Bot 运行错误: %v", err)
	}

	ui.PrintSuccess("Bot 已停止")
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