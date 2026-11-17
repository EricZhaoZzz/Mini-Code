package memory

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func sourcePaths(ins *Instructions) []string {
	var out []string
	for _, s := range ins.Sources {
		out = append(out, s.Level+":"+filepath.Base(filepath.Dir(s.Path))+"/"+filepath.Base(s.Path))
	}
	return out
}

func TestLoadInstructions_HierarchyOrder(t *testing.T) {
	home := t.TempDir()
	root := t.TempDir()
	ws := filepath.Join(root, "repo")

	writeFile(t, filepath.Join(home, ".mini-code", "MINICODE.md"), "user rule")
	writeFile(t, filepath.Join(root, "MINICODE.md"), "parent rule")
	writeFile(t, filepath.Join(ws, "MINICODE.md"), "project rule")
	writeFile(t, filepath.Join(ws, ".mini-code", "MINICODE.md"), "project dot rule")
	writeFile(t, filepath.Join(ws, "MINICODE.local.md"), "local rule")
	writeFile(t, filepath.Join(ws, ".mini-code", "rules", "style.md"), "always rule")
	writeFile(t, filepath.Join(ws, ".mini-code", "rules", "go.md"), "---\npaths:\n  - \"**/*.go\"\n---\ngo only")

	got := sourcePaths(LoadInstructions(home, ws))
	want := []string{
		"user:.mini-code/MINICODE.md",
		"project:" + filepath.Base(root) + "/MINICODE.md",
		"project:repo/MINICODE.md",
		"project:.mini-code/MINICODE.md",
		"local:repo/MINICODE.local.md",
		"rule:rules/style.md",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("order mismatch\n got: %v\nwant: %v", got, want)
	}
}

func TestLoadInstructions_FallbackFiles(t *testing.T) {
	ws := t.TempDir()
	writeFile(t, filepath.Join(ws, "AGENTS.md"), "agents")
	writeFile(t, filepath.Join(ws, "CLAUDE.md"), "claude")

	ins := LoadInstructions(t.TempDir(), ws)
	if len(ins.Sources) != 1 || ins.Sources[0].Content != "agents" {
		t.Fatalf("expected only AGENTS.md, got %v", sourcePaths(ins))
	}

	writeFile(t, filepath.Join(ws, "MINICODE.md"), "mini")
	ins = LoadInstructions(t.TempDir(), ws)
	if len(ins.Sources) != 1 || ins.Sources[0].Content != "mini" {
		t.Fatalf("MINICODE.md should take precedence, got %v", sourcePaths(ins))
	}
}

func TestLoadInstructions_HomeAncestorNotDuplicated(t *testing.T) {
	home := t.TempDir()
	ws := filepath.Join(home, "proj")
	writeFile(t, filepath.Join(home, ".mini-code", "MINICODE.md"), "user rule")
	os.MkdirAll(ws, 0o755)

	ins := LoadInstructions(home, ws)
	if len(ins.Sources) != 1 || ins.Sources[0].Level != LevelUser {
		t.Fatalf("user file should load once as user level, got %v", sourcePaths(ins))
	}
}

func TestImports(t *testing.T) {
	home := t.TempDir()
	ws := t.TempDir()
	writeFile(t, filepath.Join(ws, "MINICODE.md"), strings.Join([]string{
		"see @docs/a.md and @~/personal.md",
		"mail me at foo@bar.com",
		"`@docs/inline.md`",
		"```",
		"@docs/fenced.md",
		"```",
		"@missing.md stays",
	}, "\n"))
	writeFile(t, filepath.Join(ws, "docs", "a.md"), "A imports @b.md")
	writeFile(t, filepath.Join(ws, "docs", "b.md"), "B imports @a.md") // 循环
	writeFile(t, filepath.Join(ws, "docs", "inline.md"), "inline")
	writeFile(t, filepath.Join(ws, "docs", "fenced.md"), "fenced")
	writeFile(t, filepath.Join(home, "personal.md"), "personal")

	var contents []string
	for _, s := range LoadInstructions(home, ws).Sources {
		contents = append(contents, s.Content)
	}
	joined := strings.Join(contents, "|")
	for _, want := range []string{"A imports", "B imports", "personal"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing import %q in %q", want, joined)
		}
	}
	for _, unwanted := range []string{"inline", "fenced"} {
		for _, c := range contents {
			if c == unwanted {
				t.Errorf("%q should not be imported", unwanted)
			}
		}
	}
	if len(contents) != 4 {
		t.Errorf("expected 4 sources (main, a, b, personal), got %d", len(contents))
	}
}

func TestImports_MaxDepth(t *testing.T) {
	ws := t.TempDir()
	writeFile(t, filepath.Join(ws, "MINICODE.md"), "@f1.md")
	for i := 1; i <= 7; i++ {
		writeFile(t, filepath.Join(ws, fmt.Sprintf("f%d.md", i)), fmt.Sprintf("level %d @f%d.md", i, i+1))
	}
	ins := LoadInstructions(t.TempDir(), ws)
	if len(ins.Sources) != 1+maxImportDepth {
		t.Fatalf("expected %d sources, got %d", 1+maxImportDepth, len(ins.Sources))
	}
}

func TestMatchGlob(t *testing.T) {
	cases := []struct {
		pattern, path string
		want          bool
	}{
		{"**/*.go", "main.go", true},
		{"**/*.go", "pkg/a/b.go", true},
		{"src/**/*.ts", "src/x.ts", true},
		{"src/**/*.ts", "lib/x.ts", false},
		{"src/*.{ts,tsx}", "src/a.tsx", true},
		{"src/*.{ts,tsx}", "src/a.js", false},
		{"*.md", "docs/a.md", false},
	}
	for _, c := range cases {
		if got := matchGlob(c.pattern, c.path); got != c.want {
			t.Errorf("matchGlob(%q, %q) = %v, want %v", c.pattern, c.path, got, c.want)
		}
	}
}

func TestLazyLoader(t *testing.T) {
	home := t.TempDir()
	ws := t.TempDir()
	writeFile(t, filepath.Join(ws, "MINICODE.md"), "root rule")
	writeFile(t, filepath.Join(ws, "pkg", "MINICODE.md"), "pkg rule @../MINICODE.md")
	writeFile(t, filepath.Join(ws, "pkg", "sub", "x.go"), "package sub")
	writeFile(t, filepath.Join(ws, "pkg", "sub", "y.txt"), "text")
	writeFile(t, filepath.Join(ws, ".mini-code", "rules", "go.md"), "---\npaths: \"**/*.go\"\n---\ngo rule")

	z := NewLazyLoader(home, ws)
	out := z.OnFileAccess(filepath.Join(ws, "pkg", "sub", "x.go"))
	if !strings.Contains(out, "pkg rule") || !strings.Contains(out, "go rule") {
		t.Fatalf("expected pkg rule and go rule, got %q", out)
	}
	if strings.Contains(out, "root rule") {
		t.Error("root rule is already in system prompt and must not be re-injected")
	}
	if again := z.OnFileAccess(filepath.Join(ws, "pkg", "sub", "x.go")); again != "" {
		t.Errorf("second access should inject nothing, got %q", again)
	}
	if txt := z.OnFileAccess(filepath.Join(ws, "pkg", "sub", "y.txt")); txt != "" {
		t.Errorf("non-matching file should inject nothing, got %q", txt)
	}
	if out := z.OnFileAccess(filepath.Join(t.TempDir(), "outside.go")); out != "" {
		t.Error("paths outside workspace must be ignored")
	}

	z.Reset()
	if out := z.OnFileAccess(filepath.Join(ws, "pkg", "sub", "x.go")); !strings.Contains(out, "pkg rule") {
		t.Error("after Reset instructions should be injected again")
	}
}

func TestAutoMemory_SaveUpdateDelete(t *testing.T) {
	a, err := OpenAuto(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, updated, err := a.Save("prefer-pnpm", "用 pnpm", "feedback", "用 pnpm\n**Why:** 统一"); err != nil || updated {
		t.Fatalf("save: updated=%v err=%v", updated, err)
	}
	if _, updated, err := a.Save("prefer-pnpm", "用 pnpm 不用 npm", "feedback", "body2"); err != nil || !updated {
		t.Fatalf("update: updated=%v err=%v", updated, err)
	}
	a.Save("team-board", "看板地址", "reference", "https://example.invalid/board")

	idx := a.Index()
	if strings.Count(idx, "prefer-pnpm.md") != 1 || !strings.Contains(idx, "用 pnpm 不用 npm") {
		t.Errorf("index should contain one updated line, got:\n%s", idx)
	}
	entries, _ := a.List()
	if len(entries) != 2 || entries[0].Type != "feedback" || entries[0].Body != "body2" {
		t.Errorf("unexpected entries: %+v", entries)
	}

	if err := a.Delete("prefer-pnpm"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(a.Index(), "prefer-pnpm") {
		t.Error("deleted entry still in index")
	}
	if err := a.Delete("prefer-pnpm"); err == nil {
		t.Error("deleting missing memory should fail")
	}
}

func TestAutoMemory_Validation(t *testing.T) {
	a, _ := OpenAuto(t.TempDir(), t.TempDir())
	bad := []struct{ name, typ string }{
		{"Bad Name", "user"}, {"../escape", "user"}, {"memory", "user"}, {"ok-name", "global"},
	}
	for _, b := range bad {
		if _, _, err := a.Save(b.name, "d", b.typ, "c"); err == nil {
			t.Errorf("expected error for name=%q type=%q", b.name, b.typ)
		}
	}
}

func TestAutoMemory_IndexTruncated(t *testing.T) {
	a, _ := OpenAuto(t.TempDir(), t.TempDir())
	var sb strings.Builder
	for i := 0; i < 250; i++ {
		fmt.Fprintf(&sb, "- line %d\n", i)
	}
	writeFile(t, filepath.Join(a.Dir(), IndexFile), sb.String())
	idx := a.Index()
	if strings.Contains(idx, "line 200") || !strings.Contains(idx, "还有 50 行") {
		t.Errorf("index not truncated at 200 lines")
	}
}

func TestProjectSlug(t *testing.T) {
	ws := t.TempDir()
	slug := ProjectSlug(ws)
	for _, r := range slug {
		if !(r == '-' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			t.Fatalf("slug contains %q: %s", r, slug)
		}
	}
}

func TestPromptSection(t *testing.T) {
	home := t.TempDir()
	ws := t.TempDir()
	writeFile(t, filepath.Join(ws, "MINICODE.md"), "使用 pnpm")
	c, err := NewContext(home, ws)
	if err != nil {
		t.Fatal(err)
	}
	c.Auto.Save("reply-lang", "回复语言：中文", "user", "中文")

	p := c.PromptSection()
	for _, want := range []string{"使用 pnpm", "## 自动记忆", "[reply-lang](reply-lang.md)", c.Auto.Dir()} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt missing %q", want)
		}
	}
	var nilCtx *Context
	if nilCtx.PromptSection() != "" {
		t.Error("nil context should produce empty section")
	}
}

func TestInstructionPath_ProjectUsesEffectiveFile(t *testing.T) {
	ws := t.TempDir()
	c, _ := NewContext(t.TempDir(), ws)
	p, _ := c.InstructionPath(LevelProject)
	if filepath.Base(p) != InstructionFile {
		t.Errorf("default should be MINICODE.md, got %s", p)
	}
	writeFile(t, filepath.Join(ws, "AGENTS.md"), "x")
	p, _ = c.InstructionPath(LevelProject)
	if filepath.Base(p) != "AGENTS.md" {
		t.Errorf("should target existing fallback file, got %s", p)
	}
}

func TestMigrateLegacy(t *testing.T) {
	home := t.TempDir()
	ws := t.TempDir()
	globalPath, projectPath := LegacyDBPaths(home, ws)

	makeDB := func(path string, rows [][2]string) {
		os.MkdirAll(filepath.Dir(path), 0o755)
		db, err := sql.Open("sqlite", path)
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		if _, err := db.Exec(`CREATE TABLE long_term_memory (id INTEGER PRIMARY KEY AUTOINCREMENT, scope TEXT, workspace TEXT, content TEXT, tags TEXT)`); err != nil {
			t.Fatal(err)
		}
		for _, r := range rows {
			db.Exec(`INSERT INTO long_term_memory (scope, content) VALUES (?, ?)`, r[0], r[1])
		}
	}
	makeDB(globalPath, [][2]string{{"user", "回复用中文"}, {"global", "Go 用 gofmt"}})
	makeDB(projectPath, [][2]string{{"project", "项目使用 pnpm"}})

	c, err := NewContext(home, ws)
	if err != nil {
		t.Fatal(err)
	}
	n, err := c.MigrateLegacy()
	if err != nil || n != 3 {
		t.Fatalf("migrate: n=%d err=%v", n, err)
	}
	entries, _ := c.Auto.List()
	types := map[string]string{}
	for _, e := range entries {
		types[e.Name] = e.Type
	}
	if types["legacy-user-1"] != "user" || types["legacy-global-2"] != "reference" || types["legacy-project-1"] != "project" {
		t.Errorf("unexpected migrated entries: %v", types)
	}
	if isRegularFile(projectPath) || !isRegularFile(projectPath+".bak") {
		t.Error("project db should be renamed to .bak")
	}
	if !isRegularFile(globalPath) {
		t.Error("global db is shared and must be kept")
	}
	if n, _ := c.MigrateLegacy(); n != 0 {
		t.Errorf("second migration should be a no-op, got %d", n)
	}
}
