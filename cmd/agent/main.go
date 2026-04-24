package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mini-code/pkg/agent"
	"mini-code/pkg/channel/cli"
	"mini-code/pkg/memory"
	"mini-code/pkg/orchestrator"
	"mini-code/pkg/provider"
	"mini-code/pkg/supervisor"
	"mini-code/pkg/tools"
	"mini-code/pkg/ui"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/ergochat/readline"
	"github.com/sashabaranov/go-openai"
)

const (
	workerFlag       = "--worker"
	standbyFlag      = "--standby"
	controlAddrFlag  = "--control-addr"
	workerIDFlag     = "--worker-id"
	snapshotPathFlag = "--session-snapshot"
)

var errWorkerShutdown = errors.New("worker shutdown requested")

type workerOptions struct {
	controlAddr  string
	workerID     string
	snapshotPath string
	standby      bool
}

type workerProcess struct {
	id    string
	cmd   *exec.Cmd
	ready bool
}

type processExit struct {
	workerID string
	err      error
}

type serverPeer struct {
	server   *supervisor.ControlServer
	workerID string
}

func (p *serverPeer) Send(message supervisor.ControlMessage) error {
	return p.server.Send(p.workerID, message)
}

type workerControl struct {
	client *supervisor.ControlClient
}

func (c *workerControl) markReady() error {
	return c.client.Send(supervisor.ControlMessage{Type: supervisor.MessageTypeReady})
}

func (c *workerControl) waitForActivate() error {
	for message := range c.client.Incoming() {
		switch message.Type {
		case supervisor.MessageTypeActivate:
			return nil
		case supervisor.MessageTypeShutdown:
			return errWorkerShutdown
		case supervisor.MessageTypeFailed:
			return fmt.Errorf("standby worker 启动失败: %s", message.Error)
		}
	}
	return fmt.Errorf("控制通道已关闭")
}

func (c *workerControl) requestRestart(request supervisor.RestartRequest) error {
	if err := c.client.Send(supervisor.ControlMessage{
		Type:           supervisor.MessageTypeRestart,
		ExecutablePath: request.ExecutablePath,
		SnapshotPath:   request.SnapshotPath,
		WorkspaceRoot:  request.WorkspaceRoot,
	}); err != nil {
		return err
	}

	for message := range c.client.Incoming() {
		switch message.Type {
		case supervisor.MessageTypeShutdown:
			return errWorkerShutdown
		case supervisor.MessageTypeFailed:
			return fmt.Errorf("重启失败: %s", message.Error)
		}
	}
	return fmt.Errorf("控制通道已关闭")
}

func (c *workerControl) reportFailure(err error) {
	if c == nil || c.client == nil || err == nil {
		return
	}
	_ = c.client.Send(supervisor.ControlMessage{
		Type:  supervisor.MessageTypeFailed,
		Error: err.Error(),
	})
}

func main() {
	if err := run(); err != nil {
		ui.PrintError("运行失败: %v", err)
		os.Exit(1)
	}
}

func run() error {
	options, isWorker, err := parseWorkerOptions(os.Args[1:])
	if err != nil {
		return err
	}
	if isWorker {
		return runWorker(options)
	}
	return runSupervisor()
}

func parseWorkerOptions(args []string) (workerOptions, bool, error) {
	var options workerOptions
	isWorker := false

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case workerFlag:
			isWorker = true
		case standbyFlag:
			options.standby = true
		case controlAddrFlag:
			if i+1 >= len(args) {
				return workerOptions{}, false, fmt.Errorf("%s 缺少参数", controlAddrFlag)
			}
			i++
			options.controlAddr = args[i]
		case workerIDFlag:
			if i+1 >= len(args) {
				return workerOptions{}, false, fmt.Errorf("%s 缺少参数", workerIDFlag)
			}
			i++
			options.workerID = args[i]
		case snapshotPathFlag:
			if i+1 >= len(args) {
				return workerOptions{}, false, fmt.Errorf("%s 缺少参数", snapshotPathFlag)
			}
			i++
			options.snapshotPath = args[i]
		}
	}

	return options, isWorker, nil
}

func runSupervisor() error {
	loadDotEnv(".env")

	controlServer, err := supervisor.NewControlServer()
	if err != nil {
		return fmt.Errorf("创建控制服务失败: %w", err)
	}
	defer controlServer.Close()

	executablePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("获取当前可执行文件失败: %w", err)
	}

	coordinator := supervisor.NewSwitchCoordinator()
	processEvents := make(chan processExit, 8)

	activeID := newWorkerID()
	activeCmd, err := startWorkerProcess(executablePath, workerOptions{
		controlAddr: controlServer.Addr(),
		workerID:    activeID,
	})
	if err != nil {
		return fmt.Errorf("启动初始 worker 失败: %w", err)
	}
	active := workerProcess{id: activeID, cmd: activeCmd}
	go waitForProcess(active, processEvents)

	var standby *workerProcess

	for {
		select {
		case event, ok := <-controlServer.Events():
			if !ok {
				return nil
			}
			switch event.Message.Type {
			case supervisor.MessageTypeReady:
				if event.WorkerID == active.id && coordinator.ActiveWorkerID() == "" {
					active.ready = true
					coordinator.SetActive(active.id, &serverPeer{server: controlServer, workerID: active.id})
					continue
				}
				if standby != nil && event.WorkerID == standby.id {
					standby.ready = true
					if err := coordinator.BeginPromotion(standby.id, &serverPeer{server: controlServer, workerID: standby.id}); err != nil {
						_ = controlServer.Send(active.id, supervisor.ControlMessage{
							Type:  supervisor.MessageTypeFailed,
							Error: err.Error(),
						})
						_ = standby.cmd.Process.Kill()
						standby = nil
						continue
					}
				}
			case supervisor.MessageTypeRestart:
				if event.WorkerID != active.id {
					_ = controlServer.Send(event.WorkerID, supervisor.ControlMessage{
						Type:  supervisor.MessageTypeFailed,
						Error: "只有当前活跃 worker 可以请求重启",
					})
					continue
				}
				if standby != nil {
					_ = controlServer.Send(active.id, supervisor.ControlMessage{
						Type:  supervisor.MessageTypeFailed,
						Error: "已有重启切换正在进行中",
					})
					continue
				}

				standbyID := newWorkerID()
				standbyCmd, startErr := startWorkerProcess(event.Message.ExecutablePath, workerOptions{
					controlAddr:  controlServer.Addr(),
					workerID:     standbyID,
					snapshotPath: event.Message.SnapshotPath,
					standby:      true,
				})
				if startErr != nil {
					_ = controlServer.Send(active.id, supervisor.ControlMessage{
						Type:  supervisor.MessageTypeFailed,
						Error: fmt.Sprintf("启动新 worker 失败: %v", startErr),
					})
					continue
				}
				standby = &workerProcess{id: standbyID, cmd: standbyCmd}
				go waitForProcess(*standby, processEvents)
			case supervisor.MessageTypeFailed:
				if standby != nil && event.WorkerID == standby.id {
					_ = controlServer.Send(active.id, supervisor.ControlMessage{
						Type:  supervisor.MessageTypeFailed,
						Error: event.Message.Error,
					})
					standby = nil
				}
			case supervisor.MessageTypeDisconnected:
				if standby != nil && event.WorkerID == standby.id {
					standby = nil
				}
			}
		case procEvent := <-processEvents:
			if standby != nil && procEvent.workerID == standby.id {
				if !standby.ready || procEvent.err != nil {
					_ = controlServer.Send(active.id, supervisor.ControlMessage{
						Type:  supervisor.MessageTypeFailed,
						Error: fmt.Sprintf("新 worker 退出: %v", procEvent.err),
					})
				}
				standby = nil
				continue
			}
			if procEvent.workerID == active.id {
				if standby != nil && standby.ready {
					if err := coordinator.CompletePromotion(); err != nil {
						return err
					}
					active = *standby
					standby = nil
					continue
				}
				if procEvent.err != nil {
					return procEvent.err
				}
				return nil
			}
		}
	}
}

func runWorker(options workerOptions) error {
	loadDotEnv(".env")

	apiKey := os.Getenv("API_KEY")
	baseURL := os.Getenv("BASE_URL")
	modelID := os.Getenv("MODEL")
	if apiKey == "" || baseURL == "" || modelID == "" {
		return fmt.Errorf("缺少必要环境变量 API_KEY / BASE_URL / MODEL")
	}

	var control *workerControl
	if options.controlAddr != "" && options.workerID != "" {
		client, err := supervisor.DialControlClient(options.controlAddr, options.workerID)
		if err != nil {
			return fmt.Errorf("连接 supervisor 失败: %w", err)
		}
		defer client.Close()
		control = &workerControl{client: client}
	}

	cfg := openai.DefaultConfig(apiKey)
	cfg.BaseURL = baseURL
	p := provider.NewOpenAIProvider(cfg, modelID)

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

	rl, err := readline.NewEx(&readline.Config{
		Prompt:          ui.SprintColor(ui.User, "➜ ") + ui.SprintColor(ui.Bold, ""),
		HistoryFile:     ".mini-code-history",
		HistoryLimit:    1000,
		AutoComplete:    getCompleter(),
		InterruptPrompt: "^C",
		EOFPrompt:       "exit",
	})
	if err != nil {
		if control != nil {
			control.reportFailure(err)
		}
		return fmt.Errorf("初始化输入失败: %w", err)
	}
	defer rl.Close()

	cliCh := cli.New(rl)
	orch := orchestrator.New(memStore)

	if options.snapshotPath != "" {
		snapshot, err := loadSessionSnapshot(options.snapshotPath)
		if err != nil {
			if control != nil {
				control.reportFailure(err)
			}
			return err
		}
		orch.RestoreSession(snapshot)
	}

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

	tools.SetDispatchFactory(func(role string, task string) (string, error) {
		subAgent := factory(role)
		messages := []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: "你是专业的 AI 助手。"},
			{Role: openai.ChatMessageRoleUser, Content: task},
		}
		reply, _, err := subAgent.Run(context.Background(), messages, nil)
		return reply, err
	})

	if control != nil {
		workspaceRoot, _ := os.Getwd()
		workerRuntime := supervisor.NewWorkerRuntime(supervisor.WorkerRuntimeConfig{
			WorkspaceRoot: workspaceRoot,
			BuildBinary:   buildUpdatedBinary,
			RequestRestart: func(request supervisor.RestartRequest) error {
				return control.requestRestart(request)
			},
		})
		orch.SetRestartRuntime(workerRuntime)
		tools.SetRestartHandler(workerRuntime.PrepareRestart)
		defer tools.SetRestartHandler(nil)
	} else {
		tools.SetRestartHandler(nil)
	}

	if control != nil {
		if err := control.markReady(); err != nil {
			return err
		}
		if options.standby {
			if err := control.waitForActivate(); err != nil {
				if errors.Is(err, errWorkerShutdown) {
					return nil
				}
				return err
			}
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ui.GlobalEscMonitor.SetCancelFunc(cancel)

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
	go cliCh.Start(ctx)

	for {
		select {
		case <-ctx.Done():
			return nil
		case msg, ok := <-cliCh.Messages():
			if !ok {
				ui.PrintInfo("再见！感谢使用 Mini-Code。")
				return nil
			}

			if handleBuiltinCommand(msg.Text, orch, cliCh.ChannelID(), "local") {
				continue
			}

			taskCtx, taskCancel := context.WithCancel(ctx)
			ui.GlobalEscMonitor.SetCancelFunc(taskCancel)
			ui.GlobalEscMonitor.Start()

			err := orch.Handle(taskCtx, msg, factory, cliCh)
			fmt.Println()
			ui.GlobalEscMonitor.Stop()
			taskCancel()

			if err != nil {
				if errors.Is(err, errWorkerShutdown) {
					return nil
				}
				ui.PrintError("运行失败: %v", err)
			}
		}
	}
}

func startWorkerProcess(executablePath string, options workerOptions) (*exec.Cmd, error) {
	args := []string{workerFlag}
	if options.workerID != "" {
		args = append(args, workerIDFlag, options.workerID)
	}
	if options.controlAddr != "" {
		args = append(args, controlAddrFlag, options.controlAddr)
	}
	if options.snapshotPath != "" {
		args = append(args, snapshotPathFlag, options.snapshotPath)
	}
	if options.standby {
		args = append(args, standbyFlag)
	}

	cmd := exec.Command(executablePath, args...)
	cmd.Dir, _ = os.Getwd()
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()

	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd, nil
}

func waitForProcess(process workerProcess, events chan<- processExit) {
	events <- processExit{workerID: process.id, err: process.cmd.Wait()}
}

func loadSessionSnapshot(path string) (orchestrator.SessionSnapshot, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return orchestrator.SessionSnapshot{}, fmt.Errorf("读取会话快照失败: %w", err)
	}

	var snapshot orchestrator.SessionSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return orchestrator.SessionSnapshot{}, fmt.Errorf("解析会话快照失败: %w", err)
	}
	return snapshot, nil
}

func buildUpdatedBinary(workspaceRoot string) (string, error) {
	executablePath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("获取当前可执行文件失败: %w", err)
	}

	baseName := filepath.Base(executablePath)
	if runtime.GOOS == "windows" && filepath.Ext(baseName) == "" {
		baseName += ".exe"
	}

	buildID := time.Now().Format("20060102-150405")
	targetDir := filepath.Join(workspaceRoot, ".mini-code", "releases", buildID)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return "", fmt.Errorf("创建发布目录失败: %w", err)
	}

	targetPath := filepath.Join(targetDir, baseName)
	command := exec.Command("go", "build", "-o", targetPath, "./cmd/agent")
	command.Dir = workspaceRoot
	output, err := command.CombinedOutput()
	if err != nil {
		trimmed := strings.TrimSpace(string(output))
		if trimmed == "" {
			return "", fmt.Errorf("构建失败: %w", err)
		}
		return "", fmt.Errorf("构建失败: %s", trimmed)
	}

	return targetPath, nil
}

func newWorkerID() string {
	return fmt.Sprintf("worker-%d", time.Now().UnixNano())
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
