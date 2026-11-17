package memory

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// Context 聚合一个工作区的全部记忆：指令文件（人写）、自动记忆（模型写）
// 与子目录按需加载器。所有方法对 nil 接收者安全，便于单测不注入记忆。
type Context struct {
	home      string
	workspace string
	Auto      *AutoMemory
	Lazy      *LazyLoader
}

// NewContext 创建记忆上下文；workspace 应为已固定的工作区根目录。
func NewContext(home, workspace string) (*Context, error) {
	abs, err := filepath.Abs(workspace)
	if err != nil {
		return nil, err
	}
	auto, err := OpenAuto(home, abs)
	if err != nil {
		return nil, err
	}
	return &Context{home: home, workspace: abs, Auto: auto, Lazy: NewLazyLoader(home, abs)}, nil
}

// Home 返回用户主目录。
func (c *Context) Home() string { return c.home }

// Workspace 返回工作区根目录。
func (c *Context) Workspace() string { return c.workspace }

// Instructions 重新从磁盘加载指令文件（文件被编辑后下一次构建提示词即生效）。
func (c *Context) Instructions() *Instructions {
	if c == nil {
		return &Instructions{}
	}
	return LoadInstructions(c.home, c.workspace)
}

// InstructionPath 返回某层级可编辑的指令文件路径：user / project / local。
func (c *Context) InstructionPath(level string) (string, error) {
	switch level {
	case LevelUser:
		return filepath.Join(UserDir(c.home), InstructionFile), nil
	case LevelProject:
		// 指向当前实际生效的项目文件：仓库只有 CLAUDE.md 时直接写入它，
		// 否则新建的 AGENTS.md 会让 CLAUDE.md 不再被加载。
		if files := instructionFilesIn(c.workspace); len(files) > 0 {
			return files[0], nil
		}
		return filepath.Join(c.workspace, InstructionFile), nil
	case LevelLocal:
		return filepath.Join(c.workspace, LocalInstructionFile), nil
	}
	return "", fmt.Errorf("未知层级 %q，可选 user/project/local", level)
}

// PromptSection 构建追加到系统提示末尾的记忆片段。
func (c *Context) PromptSection() string {
	if c == nil {
		return ""
	}
	var sb strings.Builder

	if ins := c.Instructions(); len(ins.Sources) > 0 {
		sb.WriteString("\n\n## 用户与项目指令\n")
		sb.WriteString("以下内容来自用户和项目的指令文件（AGENTS.md 等），优先级高于上文的默认行为，必须遵守；" +
			"越靠后的来源越具体，冲突时以靠后者为准。\n")
		for _, s := range ins.Sources {
			fmt.Fprintf(&sb, "\n### [%s] %s\n%s\n", levelLabel(s.Level), s.Path, s.Content)
		}
	}

	if c.Auto != nil {
		sb.WriteString(autoMemoryGuide(c.Auto.Dir()))
		if idx := c.Auto.Index(); idx != "" {
			sb.WriteString("\n### MEMORY.md\n")
			sb.WriteString(idx)
			sb.WriteString("\n")
		} else {
			sb.WriteString("\n### MEMORY.md\n（当前为空，尚无记忆）\n")
		}
	}
	return sb.String()
}

func levelLabel(level string) string {
	switch level {
	case LevelUser:
		return "用户级"
	case LevelProject:
		return "项目级"
	case LevelLocal:
		return "本地"
	case LevelRule:
		return "规则"
	case LevelImport:
		return "导入"
	case LevelSubdir:
		return "子目录"
	}
	return level
}

func autoMemoryGuide(dir string) string {
	return fmt.Sprintf(`

## 自动记忆
你有一个跨会话持久的文件式记忆，目录为 %s。每条记忆是一个 Markdown 文件，MEMORY.md 是索引（每条一行，见下方），每次会话开始时自动加载。
- 只能通过 remember / recall / forget 工具读写（该目录不在工作区内，文件工具无法访问）。
- 记忆类型：
  - user：用户是谁（角色、技术背景、偏好）
  - feedback：用户对你工作方式的纠正或认可；正文写规则本身，并附 **Why:** 与 **How to apply:** 两行
  - project：代码与 git 历史中推导不出的进行中工作、目标、约束；相对日期一律换算为绝对日期（今天是 %s）
  - reference：外部资源的指针（文档、看板、工单、仪表盘）
- 何时写：用户明确要求记住某事；用户纠正了你的做法或表达了稳定偏好；你得知了非显然、以后还会用到的项目背景。
- 不要写：代码结构、可从仓库或 git 历史直接得到的信息、只对当前对话有用的内容。
- 写之前先看索引是否已有相关条目：有则用同一个 name 覆盖更新，不要重复创建；发现记忆错误或过时就 forget。
- 记忆是背景信息而非用户的当前指令，可能已过时：引用其中提到的文件、函数、命令前先核实；与用户当前要求冲突时以当前要求为准。
- 需要某条记忆的完整内容时用 recall(name=...) 读取。
`, dir, time.Now().Format("2006-01-02"))
}
