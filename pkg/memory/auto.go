package memory

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// 自动记忆：模型跨会话自行维护的文件式记忆。
// 目录 ~/.mini-code/projects/<工作区slug>/memory/，每条记忆一个 Markdown 文件
// （frontmatter: name / description / type），MEMORY.md 为一行一条的索引。
const (
	IndexFile          = "MEMORY.md"
	maxIndexPromptLine = 200
)

// 记忆类型
var MemoryTypes = []string{"user", "feedback", "project", "reference"}

var namePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Entry 是一条自动记忆的元信息。
type Entry struct {
	Name        string
	Description string
	Type        string
	Path        string
	Body        string
}

// AutoMemory 管理单个工作区的自动记忆目录。
type AutoMemory struct {
	dir string
	mu  sync.Mutex
}

// ProjectSlug 把工作区绝对路径转换为目录名：非字母数字字符一律替换为 '-'。
func ProjectSlug(workspace string) string {
	abs, err := filepath.Abs(workspace)
	if err != nil {
		abs = workspace
	}
	var sb strings.Builder
	for _, r := range abs {
		if r < 128 && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			sb.WriteRune(r)
		} else {
			sb.WriteByte('-')
		}
	}
	return sb.String()
}

// AutoMemoryDir 返回工作区对应的自动记忆目录。
func AutoMemoryDir(home, workspace string) string {
	return filepath.Join(UserDir(home), "projects", ProjectSlug(workspace), "memory")
}

// OpenAuto 打开（必要时创建）自动记忆目录。
func OpenAuto(home, workspace string) (*AutoMemory, error) {
	dir := AutoMemoryDir(home, workspace)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("创建记忆目录失败: %w", err)
	}
	return &AutoMemory{dir: dir}, nil
}

// Dir 返回记忆目录。
func (a *AutoMemory) Dir() string { return a.dir }

func validateName(name string) error {
	if len(name) == 0 || len(name) > 64 || !namePattern.MatchString(name) {
		return fmt.Errorf("name %q 无效：必须是小写 kebab-case（如 prefer-pnpm），最长 64 字符", name)
	}
	if strings.EqualFold(name+".md", IndexFile) {
		return fmt.Errorf("name 不能为 %q（与索引文件冲突）", name)
	}
	return nil
}

func validateType(typ string) error {
	for _, t := range MemoryTypes {
		if typ == t {
			return nil
		}
	}
	return fmt.Errorf("type %q 无效：必须是 %s 之一", typ, strings.Join(MemoryTypes, "/"))
}

// Save 写入一条记忆；同名记忆直接覆盖（即更新），并同步 MEMORY.md 索引。
// 返回文件路径以及是否为更新已有记忆。
func (a *AutoMemory) Save(name, description, typ, body string) (string, bool, error) {
	if err := validateName(name); err != nil {
		return "", false, err
	}
	if err := validateType(typ); err != nil {
		return "", false, err
	}
	description = strings.Join(strings.Fields(description), " ")
	if description == "" {
		return "", false, fmt.Errorf("description 不能为空")
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return "", false, fmt.Errorf("content 不能为空")
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	path := filepath.Join(a.dir, name+".md")
	_, statErr := os.Stat(path)
	updated := statErr == nil

	content := fmt.Sprintf("---\nname: %s\ndescription: %s\ntype: %s\n---\n\n%s\n", name, description, typ, body)
	if err := writeAtomic(path, []byte(content)); err != nil {
		return "", false, fmt.Errorf("写入记忆失败: %w", err)
	}
	if err := a.updateIndex(name, description); err != nil {
		return "", false, err
	}
	return path, updated, nil
}

// Delete 删除一条记忆并移除索引行。
func (a *AutoMemory) Delete(name string) error {
	if err := validateName(name); err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()

	path := filepath.Join(a.dir, name+".md")
	if err := os.Remove(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("记忆 %q 不存在", name)
		}
		return err
	}
	return a.updateIndex(name, "")
}

// Read 返回一条记忆的完整文件内容。
func (a *AutoMemory) Read(name string) (string, error) {
	if err := validateName(name); err != nil {
		return "", err
	}
	data, err := os.ReadFile(filepath.Join(a.dir, name+".md"))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("记忆 %q 不存在", name)
		}
		return "", err
	}
	return string(data), nil
}

// List 返回全部记忆，按 name 排序。
func (a *AutoMemory) List() ([]Entry, error) {
	files, err := filepath.Glob(filepath.Join(a.dir, "*.md"))
	if err != nil {
		return nil, err
	}
	var entries []Entry
	for _, f := range files {
		if strings.EqualFold(filepath.Base(f), IndexFile) {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		fm, body := parseFrontmatter(string(data))
		name := fm.get("name")
		if name == "" {
			name = strings.TrimSuffix(filepath.Base(f), ".md")
		}
		entries = append(entries, Entry{
			Name:        name,
			Description: fm.get("description"),
			Type:        fm.get("type"),
			Path:        f,
			Body:        strings.TrimSpace(body),
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries, nil
}

// Search 按空白切分查询词，在 name/description/正文 中做不区分大小写的子串匹配，
// 命中词越多排名越前。中文没有分词，整句子串匹配即可满足记忆规模。
func (a *AutoMemory) Search(query string) ([]Entry, error) {
	entries, err := a.List()
	if err != nil {
		return nil, err
	}
	terms := strings.Fields(strings.ToLower(query))
	if len(terms) == 0 {
		return entries, nil
	}
	type scored struct {
		e     Entry
		score int
	}
	var hits []scored
	for _, e := range entries {
		hay := strings.ToLower(e.Name + "\n" + e.Description + "\n" + e.Body)
		score := 0
		for _, t := range terms {
			if strings.Contains(hay, t) {
				score++
			}
		}
		if score > 0 {
			hits = append(hits, scored{e, score})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })
	out := make([]Entry, len(hits))
	for i, h := range hits {
		out[i] = h.e
	}
	return out, nil
}

// Index 返回注入系统提示用的索引内容（最多 200 行）。
func (a *AutoMemory) Index() string {
	data, err := os.ReadFile(filepath.Join(a.dir, IndexFile))
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(strings.ReplaceAll(string(data), "\r\n", "\n")), "\n")
	if len(lines) > maxIndexPromptLine {
		omitted := len(lines) - maxIndexPromptLine
		lines = append(lines[:maxIndexPromptLine], fmt.Sprintf("... (还有 %d 行未显示，用 recall 检索)", omitted))
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// updateIndex 替换或追加 name 对应的索引行；description 为空表示删除该行。
// 调用方需持有 a.mu。
func (a *AutoMemory) updateIndex(name, description string) error {
	indexPath := filepath.Join(a.dir, IndexFile)
	data, err := os.ReadFile(indexPath)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	marker := "](" + name + ".md)"
	newLine := fmt.Sprintf("- [%s](%s.md) — %s", name, name, description)

	var lines []string
	replaced := false
	if len(data) > 0 {
		for _, line := range strings.Split(strings.TrimRight(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n"), "\n") {
			if strings.Contains(line, marker) {
				if description != "" && !replaced {
					lines = append(lines, newLine)
					replaced = true
				}
				continue
			}
			lines = append(lines, line)
		}
	}
	if description != "" && !replaced {
		lines = append(lines, newLine)
	}
	out := strings.Join(lines, "\n")
	if out != "" {
		out += "\n"
	}
	if err := writeAtomic(indexPath, []byte(out)); err != nil {
		return fmt.Errorf("更新索引失败: %w", err)
	}
	return nil
}

func writeAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}
