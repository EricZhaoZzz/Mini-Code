package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// LazyLoader 在文件工具访问到工作区子目录时，按需加载该子目录链上的
// 指令文件，以及 paths 与被访问文件匹配的条件规则（对应 Claude Code
// 子目录 CLAUDE.md 与 .claude/rules 的行为）。每个文件每个会话只注入一次。
type LazyLoader struct {
	home      string
	workspace string

	mu      sync.Mutex
	visited map[string]bool
	seeded  bool
}

// NewLazyLoader 创建按需加载器。
func NewLazyLoader(home, workspace string) *LazyLoader {
	return &LazyLoader{home: home, workspace: workspace, visited: map[string]bool{}}
}

// Reset 清空已注入记录。会话重置、恢复或上下文压缩后，之前注入在工具结果里的
// 指令已不在上下文中，需要允许再次注入。
func (z *LazyLoader) Reset() {
	if z == nil {
		return
	}
	z.mu.Lock()
	defer z.mu.Unlock()
	z.visited = map[string]bool{}
	z.seeded = false
}

// OnFileAccess 返回因访问 absPath 而新加载的指令（已格式化），无新内容时返回空串。
func (z *LazyLoader) OnFileAccess(absPath string) string {
	if z == nil || absPath == "" {
		return ""
	}
	rel, err := filepath.Rel(z.workspace, absPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ""
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return ""
	}

	z.mu.Lock()
	defer z.mu.Unlock()

	if !z.seeded {
		// 系统提示中已常驻的文件不重复注入（例如子目录文件 @import 了根目录文件）
		for _, s := range LoadInstructions(z.home, z.workspace).Sources {
			z.visited[pathKey(s.Path)] = true
		}
		z.seeded = true
	}

	l := &loader{home: z.home, visited: z.visited}
	targetDir := absPath
	if !info.IsDir() {
		targetDir = filepath.Dir(absPath)
	}
	for _, dir := range dirsBelow(z.workspace, targetDir) {
		for _, f := range instructionFilesIn(dir) {
			l.loadDepth(f, LevelSubdir, 0)
		}
	}

	if !info.IsDir() {
		relSlash := filepath.ToSlash(rel)
		for _, rule := range z.conditionalRules() {
			if ruleMatches(rule, relSlash) {
				l.loadRule(rule, true)
			}
		}
	}

	return formatInjected(l.sources)
}

func (z *LazyLoader) conditionalRules() []string {
	rules := ruleFiles(filepath.Join(UserDir(z.home), "rules"))
	return append(rules, ruleFiles(filepath.Join(z.workspace, configDirName, "rules"))...)
}

func ruleMatches(rulePath, rel string) bool {
	data, err := os.ReadFile(rulePath)
	if err != nil {
		return false
	}
	fm, _ := parseFrontmatter(string(data))
	for _, p := range fm.list("paths") {
		if matchGlob(strings.TrimPrefix(p, "./"), rel) {
			return true
		}
	}
	return false
}

// dirsBelow 返回 root 之下（不含 root）到 target（含）的目录链，自上而下。
func dirsBelow(root, target string) []string {
	root = filepath.Clean(root)
	var chain []string
	for dir := filepath.Clean(target); pathKey(dir) != pathKey(root); {
		chain = append(chain, dir)
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil // target 不在 root 之下
		}
		dir = parent
	}
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	return chain
}

func formatInjected(sources []Source) string {
	if len(sources) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("<system-reminder>\n访问该路径加载了以下项目指令，处理相关文件时必须遵守：\n")
	for _, s := range sources {
		fmt.Fprintf(&sb, "\n### %s\n%s\n", s.Path, s.Content)
	}
	sb.WriteString("</system-reminder>")
	return sb.String()
}
