package main

import (
	"context"
	"fmt"
	"mini-code/pkg/memory"
	"mini-code/pkg/orchestrator"
	"mini-code/pkg/ui"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// initPrompt 是 init 命令发给 Agent 的任务（仿 Claude Code /init）。
const initPrompt = `请分析当前代码库，为它编写项目指令文件，供以后的会话作为系统提示加载。

要求：
1. 先用 list_files / read_file 了解项目：构建、测试、lint 等常用命令（包括如何运行单个测试），以及需要读多个文件才能理解的高层架构。
2. 若工作区已有 AGENTS.md，在其基础上改进；否则若已有 CLAUDE.md，直接改进它（它就是当前生效的项目指令）；都没有则新建 AGENTS.md。
3. 只写非显然、对以后改代码有用的内容：不要罗列每个文件、不要写通用的开发建议、不要编造仓库里不存在的信息。
4. 若存在 README、.cursorrules、.github/copilot-instructions.md 等，提炼其中的重要部分。
5. 保持简洁，使用 Markdown。`

// memoryAsker 向用户提问并读取一行回答（由 CLIChannel.Ask 实现）。
type memoryAsker func(ctx context.Context, prompt string) (string, error)

// handleMemoryCommand 处理记忆相关命令，返回 true 表示已处理。
//
//	memory | mem                         查看已加载的指令文件与自动记忆
//	memory edit [user|project|local|auto] 用编辑器打开对应文件/目录
//	# <内容>                             把一行内容追加到指令文件
func handleMemoryCommand(ctx context.Context, input string, orch *orchestrator.Orchestrator, ask memoryAsker) bool {
	trimmed := strings.TrimSpace(input)
	mem := orch.Memory()

	if strings.HasPrefix(trimmed, "#") {
		note := strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
		if note == "" {
			return false
		}
		if mem == nil {
			ui.PrintWarning("记忆未初始化")
			return true
		}
		quickAddInstruction(ctx, mem, note, ask)
		return true
	}

	fields := strings.Fields(strings.ToLower(trimmed))
	if len(fields) == 0 || (fields[0] != "memory" && fields[0] != "mem") {
		return false
	}
	if mem == nil {
		ui.PrintWarning("记忆未初始化")
		return true
	}

	switch {
	case len(fields) == 1:
		printMemoryStatus(mem)
	case fields[1] == "edit":
		level := "project"
		if len(fields) > 2 {
			level = fields[2]
		}
		editMemory(mem, level)
	default:
		ui.PrintWarning("用法: memory | memory edit [user|project|local|auto]")
	}
	return true
}

func printMemoryStatus(mem *memory.Context) {
	ins := mem.Instructions()
	ui.PrintInfo("已加载的指令文件（%d 个，越靠后优先级越高）:", len(ins.Sources))
	if len(ins.Sources) == 0 {
		ui.PrintDim("  （无）可用 init 生成项目指令，或 memory edit user 编辑用户级指令")
	}
	for _, s := range ins.Sources {
		ui.PrintDim("  [%s] %s", s.Level, s.Path)
	}

	entries, _ := mem.Auto.List()
	ui.PrintInfo("自动记忆: %d 条 — %s", len(entries), mem.Auto.Dir())
	for _, e := range entries {
		ui.PrintDim("  %s [%s] — %s", e.Name, e.Type, e.Description)
	}
	ui.PrintDim("编辑: memory edit [user|project|local|auto]；快速追加: # <内容>")
}

func quickAddInstruction(ctx context.Context, mem *memory.Context, note string, ask memoryAsker) {
	projectPath, _ := mem.InstructionPath(memory.LevelProject)
	localPath, _ := mem.InstructionPath(memory.LevelLocal)
	userPath, _ := mem.InstructionPath(memory.LevelUser)

	level := memory.LevelProject
	if ask != nil {
		answer, err := ask(ctx, fmt.Sprintf(
			"写入哪一级？ 1) 项目 %s（默认）  2) 本地 %s  3) 用户 %s  其他输入取消",
			filepath.Base(projectPath), filepath.Base(localPath), userPath))
		if err != nil {
			return
		}
		switch answer {
		case "", "1":
			level = memory.LevelProject
		case "2":
			level = memory.LevelLocal
		case "3":
			level = memory.LevelUser
		default:
			ui.PrintDim("已取消")
			return
		}
	}

	path, err := mem.InstructionPath(level)
	if err == nil {
		err = appendLine(path, "- "+note)
	}
	if err != nil {
		ui.PrintError("写入失败: %v", err)
		return
	}
	ui.PrintSuccess("已追加到 %s（下一次任务起生效）", path)
}

func appendLine(path, line string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	prefix := ""
	if data, err := os.ReadFile(path); err == nil && len(data) > 0 && !strings.HasSuffix(string(data), "\n") {
		prefix = "\n"
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(prefix + line + "\n")
	return err
}

func editMemory(mem *memory.Context, level string) {
	var target string
	if level == "auto" {
		target = mem.Auto.Dir()
	} else {
		path, err := mem.InstructionPath(level)
		if err != nil {
			ui.PrintWarning("%v", err)
			return
		}
		if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err == nil {
				_ = os.WriteFile(path, []byte("# "+filepath.Base(path)+"\n\n"), 0o644)
			}
		}
		target = path
	}

	if err := openInEditor(target); err != nil {
		ui.PrintWarning("无法打开编辑器（%v），请手动编辑: %s", err, target)
		return
	}
	ui.PrintSuccess("已打开 %s，保存后下一次任务起生效", target)
}

// openInEditor 以非阻塞方式打开文件或目录。readline 仍在读取 stdin，
// 因此不使用终端编辑器（vim 等会与之争抢输入），优先 $VISUAL（如 code），
// 否则用系统默认的图形化程序。
func openInEditor(target string) error {
	var cmd *exec.Cmd
	if visual := strings.TrimSpace(os.Getenv("VISUAL")); visual != "" {
		parts := strings.Fields(visual)
		cmd = exec.Command(parts[0], append(parts[1:], target)...)
	} else {
		switch runtime.GOOS {
		case "windows":
			if info, err := os.Stat(target); err == nil && info.IsDir() {
				cmd = exec.Command("explorer", target)
			} else {
				cmd = exec.Command("notepad", target)
			}
		case "darwin":
			cmd = exec.Command("open", target)
		default:
			cmd = exec.Command("xdg-open", target)
		}
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
