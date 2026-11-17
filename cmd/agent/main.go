package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"mini-code/pkg/agent"
	"mini-code/pkg/channel/cli"
	"mini-code/pkg/memory"
	"mini-code/pkg/orchestrator"
	"mini-code/pkg/provider"
	"mini-code/pkg/sandbox"
	"mini-code/pkg/supervisor"
	"mini-code/pkg/textutil"
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
	// sandbox runner 子进程：由 run_shell 唤起，收紧文件边界后 exec 目标命令。
	// 必须在任何初始化之前分派，否则子进程会把 supervisor/worker 的
	// 整套环境（记忆库、控制通道）都建起来，白白浪费且可能写脏数据。
	if sandbox.IsRunnerInvocation(os.Args) {
		if err := sandbox.RunSandboxed(os.Args[1:]); err != nil {
			return err
		}
		return nil
	}

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

// setupSandbox 初始化审计日志并把沙箱配置注入 run_shell。
//
// 只在真正支持 OS 沙箱的平台上谈「启用与否」；Windows 上 GetStatus
// 恒为不可用，这里照实告知用户，不假装等价。
func setupSandbox() {
	root, err := tools.CurrentWorkspaceRoot()
	if err != nil {
		root = "."
	}

	cfg := tools.CurrentSandboxConfig()
	cfg.WorkspaceRoot = root
	cfg.Strict = os.Getenv("LM_SANDBOX_STRICT") == "1"

	if len(cfg.ExtraWritableRoots) == 0 {
		cfg.ExtraWritableRoots = sandbox.DefaultWritableRoots()
	}
	if len(cfg.ReadOnlyPaths) == 0 {
		cfg.ReadOnlyPaths = sandbox.DefaultReadOnlyPaths()
	}

	tools.SetSandboxConfig(cfg)

	if err := sandbox.InitAudit(root); err != nil {
		ui.PrintError("审计日志初始化失败: %v", err)
		return
	}

	st := sandbox.GetStatus(cfg)
	if st.Available {
		ui.PrintInfo("沙箱状态: %s 已启用", st.Mechanism)
		return
	}

	// 不可用时不静默：明确告知用户当前只有 L1+L3 保护
	if reason := tools.SandboxDisabledReason(); reason != "" {
		ui.PrintInfo("沙箱状态: 未启用（%s）", reason)
		return
	}
	ui.PrintInfo("沙箱状态: 当前平台无 OS 级文件沙箱（%s）", st.Reason)
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

	// 固定工作区根目录：所有文件工具的安全边界都基于它，
	// 必须在任何 chdir 发生前确定，否则边界会随 CWD 静默漂移。
	if err := tools.SetWorkspaceRoot("."); err != nil {
		return fmt.Errorf("设置工作区根目录失败: %w", err)
	}

	// 初始化审计与沙箱：审计日志落在工作区内，
	// 沙箱配置随后注入给 run_shell。
	setupSandbox()
	defer sandbox.CloseAudit()

	pieAppID := os.Getenv("PIE_APP_ID")
	pieAppSecret := os.Getenv("PIE_APP_SECRET")
	pieGatewayPath := os.Getenv("PIE_GATEWAY_PATH")
	modelID := os.Getenv("MODEL")

	var p provider.Provider
	if pieAppID != "" && pieAppSecret != "" && pieGatewayPath != "" {
		if modelID == "" {
			modelID = "deepseek-v3-2"
		}
		p = provider.NewPieGatewayProvider(provider.PieGatewayConfig{
			AppID:       pieAppID,
			AppSecret:   pieAppSecret,
			GatewayPath: pieGatewayPath,
			Model:       modelID,
		})
	} else {
		apiKey := os.Getenv("API_KEY")
		baseURL := os.Getenv("BASE_URL")
		if apiKey == "" || baseURL == "" || modelID == "" {
			return fmt.Errorf("缺少必要环境变量: 请配置 PIE_APP_ID/PIE_APP_SECRET/PIE_GATEWAY_PATH 或 API_KEY/BASE_URL/MODEL")
		}
		cfg := openai.DefaultConfig(apiKey)
		cfg.BaseURL = baseURL
		p = provider.NewOpenAIProvider(cfg, modelID)
	}

	// 限流（429）与网关 5xx 在长任务中很常见，单次失败就中断整个任务代价太高。
	// 包在最外层，主 Agent、子 Agent 与上下文压缩共用同一策略。
	retryCfg := provider.DefaultRetryConfig()
	retryCfg.MaxRetries = provider.MaxRetriesFromEnv()
	retryCfg.OnRetry = func(attempt int, wait time.Duration, err error) {
		ui.ClearLine() // 覆盖可能存在的「等待响应」状态行
		ui.PrintWarning("模型请求失败，%.1fs 后第 %d/%d 次重试: %s",
			wait.Seconds(), attempt, retryCfg.MaxRetries, textutil.TruncateWithEllipsis(err.Error(), 120))
	}
	p = provider.WithRetry(p, retryCfg)

	var control *workerControl
	if options.controlAddr != "" && options.workerID != "" {
		client, err := supervisor.DialControlClient(options.controlAddr, options.workerID)
		if err != nil {
			return fmt.Errorf("连接 supervisor 失败: %w", err)
		}
		defer client.Close()
		control = &workerControl{client: client}
	}

	memCtx := setupMemory()
	tools.SetMemory(memCtx)
	defer tools.SetMemory(nil)

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
	orch := orchestrator.New(memCtx)
	defer orch.Close() // 停止后台 Session 清理协程
	// 注入 provider 以启用上下文压缩（超限时生成历史摘要）
	orch.SetProvider(p, modelID)
	// 会话持久化：每次任务结束后保存，退出后可用 resume 命令恢复
	if wd, err := os.Getwd(); err == nil {
		orch.SetPersistPath(orchestrator.DefaultSessionPath(wd))
	}

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

	newAgent := func(agentType string) *agent.BaseAgent {
		switch agentType {
		case "reviewer":
			return agent.NewReviewerAgent(p, modelID)
		case "researcher":
			return agent.NewResearcherAgent(p, modelID)
		default:
			return agent.NewCoderAgent(p, modelID)
		}
	}
	factory := func(agentType string) agent.Agent { return newAgent(agentType) }

	// 子 Agent：
	//  - ctx 来自父任务，双击 Esc 时一并停止；
	//  - 使用与主 Agent 相同的共享系统提示（含工作区规则、沙箱说明与记忆），
	//    角色提示仍由 BaseAgent 追加在其后；
	//  - 静默模式，只输出单行进度，避免与父 Agent 的工具调用面板互相覆盖。
	tools.SetDispatchFactory(func(ctx context.Context, role string, task string) (string, error) {
		subAgent := newAgent(role)
		subAgent.SetQuiet(true)
		messages := []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: agent.BuildSystemPromptWithMemory(memCtx)},
			{Role: openai.ChatMessageRoleUser, Content: task},
		}
		reply, _, err := subAgent.Run(ctx, messages, nil)
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

	// 危险操作（如 delete_file）必须由用户本人确认，而不是模型自己填 confirm=true
	tools.SetConfirmHandler(func(prompt string) (bool, error) {
		resume := ui.GlobalEscMonitor.Pause()
		defer resume()
		return cliCh.Confirm(ctx, prompt)
	})
	defer tools.SetConfirmHandler(nil)

	printWelcome(options.snapshotPath != "")
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
			if handleMemoryCommand(ctx, msg.Text, orch, cliCh.Ask) {
				continue
			}
			if strings.EqualFold(strings.TrimSpace(msg.Text), "init") {
				msg.Text = initPrompt
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
	return orchestrator.LoadSnapshot(path)
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
		readline.PcItem("resume"),
		readline.PcItem("memory", readline.PcItem("edit",
			readline.PcItem("user"), readline.PcItem("project"),
			readline.PcItem("local"), readline.PcItem("auto"))),
		readline.PcItem("init"),
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
		orch.ResetLazyMemory()
		ui.PrintSuccess("对话已重置，会话上下文已清空。")
		return true

	case "history", "hist":
		session := orch.GetOrCreateSession(channelID, userID)
		count := session.MessageCount()
		ui.PrintInfo("对话消息数: %d", count)
		return true

	case "resume":
		turns, err := orch.ResumeSession(channelID, userID)
		if err != nil {
			ui.PrintWarning("无法恢复会话: %v", err)
			return true
		}
		ui.PrintSuccess("已恢复上次的会话（%d 轮对话），可以直接继续", turns)
		return true

	case "version", "v":
		ui.PrintInfo("Mini-Code v1.0")
		return true
	}

	return false
}

// setupMemory 初始化记忆上下文并迁移旧版 SQLite 记忆；失败时返回 nil（记忆功能关闭）。
func setupMemory() *memory.Context {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		ui.PrintError("Memory 初始化失败: %v", err)
		return nil
	}
	workspace, err := tools.CurrentWorkspaceRoot()
	if err != nil {
		ui.PrintError("Memory 初始化失败: %v", err)
		return nil
	}
	memCtx, err := memory.NewContext(homeDir, workspace)
	if err != nil {
		ui.PrintError("Memory 初始化失败: %v", err)
		return nil
	}
	if n, err := memCtx.MigrateLegacy(); err != nil {
		ui.PrintWarning("旧版记忆迁移失败: %v", err)
	} else if n > 0 {
		ui.PrintInfo("已将 %d 条旧版记忆迁移到 %s", n, memCtx.Auto.Dir())
	}
	return memCtx
}

func printWelcome(restored bool) {
	fmt.Println(ui.WelcomeBanner())
	fmt.Println()
	ui.PrintDim("命令: help 显示帮助 | resume 恢复上次会话 | exit 退出程序")
	ui.PrintDim("提示: 直接输入你的任务开始对话...")
	// 热重启已自动恢复会话，无需再提示
	if !restored {
		if wd, err := os.Getwd(); err == nil {
			if info, ok := orchestrator.InspectSavedSession(orchestrator.DefaultSessionPath(wd)); ok {
				ui.PrintInfo("发现上次的会话（%d 轮对话，%s）：输入 resume 恢复",
					info.Turns, info.LastSeen.Local().Format("01-02 15:04"))
			}
		}
	}
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
