package memory

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
)

// 指令文件名采用通用的 AGENTS.md 约定（agents.md），不使用 mini-code 专属文件名；
// 某一级目录没有 AGENTS.md 时回退 CLAUDE.md，让只有它的仓库也能直接生效。
const (
	InstructionFile      = "AGENTS.md"
	LocalInstructionFile = "AGENTS.local.md"
	configDirName        = ".mini-code"
	maxImportDepth       = 5
)

var fallbackInstructionFiles = []string{"CLAUDE.md"}

// 指令来源层级
const (
	LevelUser    = "user"    // ~/.mini-code/AGENTS.md
	LevelProject = "project" // 工作区及其祖先目录的 AGENTS.md
	LevelLocal   = "local"   // 工作区 AGENTS.local.md（个人、不提交）
	LevelRule    = "rule"    // rules/*.md
	LevelSubdir  = "subdir"  // 访问到子目录时按需加载
	LevelImport  = "import"  // 经 @path 导入
)

// Source 是一份已加载的指令文件。
type Source struct {
	Level   string
	Path    string
	Content string
}

// Instructions 是启动时（或每次刷新时）加载的全部常驻指令，按优先级从低到高排列。
type Instructions struct {
	Sources []Source
}

// UserDir 返回用户级配置目录 ~/.mini-code。
func UserDir(home string) string { return filepath.Join(home, configDirName) }

// LoadInstructions 按 用户级 → 祖先目录到工作区 → 本地 → 无条件规则 的顺序加载指令文件。
// 带 paths 条件的规则不在此加载，由 LazyLoader 在访问匹配文件时注入。
func LoadInstructions(home, workspace string) *Instructions {
	l := &loader{home: home, visited: map[string]bool{}}
	userDir := UserDir(home)

	l.load(filepath.Join(userDir, InstructionFile), LevelUser)
	for _, rule := range ruleFiles(filepath.Join(userDir, "rules")) {
		l.loadRule(rule, false)
	}

	for _, dir := range ancestorsTopDown(workspace) {
		for _, f := range instructionFilesIn(dir) {
			l.load(f, LevelProject)
		}
	}

	l.load(filepath.Join(workspace, LocalInstructionFile), LevelLocal)
	for _, rule := range ruleFiles(filepath.Join(workspace, configDirName, "rules")) {
		l.loadRule(rule, false)
	}

	return &Instructions{Sources: l.sources}
}

// loader 负责去重与 @import 展开；visited 以规范化绝对路径为键，
// 避免同一文件因不同层级（如符号链接或大小写不同的路径）被加载两次。
type loader struct {
	home    string
	visited map[string]bool
	sources []Source
}

func (l *loader) seen(path string) bool {
	key := pathKey(path)
	if l.visited[key] {
		return true
	}
	l.visited[key] = true
	return false
}

func (l *loader) load(path, level string) {
	l.loadDepth(path, level, 0)
}

func (l *loader) loadDepth(path, level string, depth int) {
	if !isRegularFile(path) || l.seen(path) {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	_, body := parseFrontmatter(string(data))
	body = strings.TrimSpace(body)
	if body != "" {
		l.sources = append(l.sources, Source{Level: level, Path: path, Content: body})
	}
	if depth >= maxImportDepth {
		return
	}
	for _, ref := range findImports(body, filepath.Dir(path), l.home) {
		l.loadDepth(ref, LevelImport, depth+1)
	}
}

// loadRule 加载规则文件；conditional 为 false 时跳过带 paths 的规则，
// 为 true 时只加载带 paths 的规则（由调用方先判断匹配）。
func (l *loader) loadRule(path string, conditional bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	fm, _ := parseFrontmatter(string(data))
	if (len(fm.list("paths")) > 0) != conditional {
		return
	}
	l.load(path, LevelRule)
}

// instructionFilesIn 返回某目录下应加载的指令文件（存在才返回）。
func instructionFilesIn(dir string) []string {
	for _, name := range append([]string{InstructionFile}, fallbackInstructionFiles...) {
		if f := filepath.Join(dir, name); isRegularFile(f) {
			return []string{f}
		}
	}
	return nil
}

// ruleFiles 递归列出目录下的 .md 文件，按路径排序保证顺序稳定。
func ruleFiles(dir string) []string {
	var files []string
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() && strings.EqualFold(filepath.Ext(p), ".md") {
			files = append(files, p)
		}
		return nil
	})
	sort.Strings(files)
	return files
}

// ancestorsTopDown 返回从文件系统根到 dir（含）的目录列表。
func ancestorsTopDown(dir string) []string {
	dir = filepath.Clean(dir)
	var chain []string
	for {
		chain = append(chain, dir)
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	return chain
}

var importPattern = regexp.MustCompile(`(?:^|[\s(])@([^\s)]+)`)

// findImports 提取正文中的 @path 引用，跳过围栏代码块与行内代码；
// 只返回真实存在的文件，`a@b.com` 这类邮箱因 @ 前非空白不会命中。
func findImports(body, baseDir, home string) []string {
	var refs []string
	inFence := false
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		for _, m := range importPattern.FindAllStringSubmatch(stripInlineCode(line), -1) {
			ref := strings.TrimRight(m[1], ".,;:!?")
			if ref == "" {
				continue
			}
			var p string
			switch {
			case strings.HasPrefix(ref, "~/"):
				p = filepath.Join(home, filepath.FromSlash(ref[2:]))
			case filepath.IsAbs(ref):
				p = ref
			default:
				p = filepath.Join(baseDir, filepath.FromSlash(ref))
			}
			if isRegularFile(p) {
				refs = append(refs, p)
			}
		}
	}
	return refs
}

func stripInlineCode(line string) string {
	var sb strings.Builder
	inCode := false
	for _, r := range line {
		if r == '`' {
			inCode = !inCode
			sb.WriteRune(' ')
			continue
		}
		if !inCode {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func pathKey(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return strings.ToLower(abs)
	}
	return abs
}
