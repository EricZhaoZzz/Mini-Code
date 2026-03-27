# Phase 2: Memory 层 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 SQLite 三层记忆系统（Session Memory 72h TTL + Long-term Fact Store + FTS5 全文检索），并将 remember/forget/recall 三个工具注册到 Agent 工具集，记忆在每次会话开始时注入 system prompt。

**Architecture:** `pkg/memory/` 包管理两个 SQLite 连接（全局 DB 和项目 DB），Orchestrator 在每次会话开始时调用 `memory.BuildPromptSuffix()` 注入记忆，Agent 通过 remember/forget/recall 工具主动维护 Fact Store，任务完成后 Orchestrator 异步写入 Session Memory。

**Tech Stack:** `modernc.org/sqlite`（纯 Go，无 CGO）、`database/sql`、Go 标准库

**前置条件：** Phase 1 已完成，全部测试通过。

---

## 文件变动清单

| 操作 | 文件 | 说明 |
|------|------|------|
| Create | `pkg/memory/store.go` | DB 连接管理，自动建表 |
| Create | `pkg/memory/session.go` | Session Memory CRUD（72h TTL） |
| Create | `pkg/memory/longterm.go` | Long-term Fact Store CRUD + FTS5 |
| Create | `pkg/memory/prompt.go` | 记忆 → system prompt 注入格式化 |
| Create | `pkg/memory/store_test.go` | 全部 memory 操作测试 |
| Create | `pkg/tools/memory_tools.go` | remember/forget/recall 工具实现 |
| Modify | `pkg/tools/registry.go` | 注册三个新工具 |
| Modify | `pkg/orchestrator/orchestrator.go` | 集成 Memory Store |
| Modify | `go.mod` / `go.sum` | 添加 modernc.org/sqlite 依赖 |

---

## Task 1: 添加 SQLite 依赖

- [ ] **Step 1.1: 安装依赖**

```bash
cd C:/Users/Eric/dev/mini-code
go get modernc.org/sqlite
```
预期：`go.mod` 和 `go.sum` 更新

- [ ] **Step 1.2: 验证依赖可用**

```go
// 临时验证，不需要提交
package main
import _ "modernc.org/sqlite"
```

```bash
go build ./... 2>&1
```
预期：无报错

- [ ] **Step 1.3: Commit 依赖变更**

```bash
git add go.mod go.sum
git commit -m "chore: 添加 modernc.org/sqlite 依赖（纯 Go，无 CGO）"
```

---

## Task 2: Memory Store（DB 连接管理 + 自动建表）

**Files:**
- Create: `pkg/memory/store.go`
- Create: `pkg/memory/store_test.go`

- [ ] **Step 2.1: 写失败测试**

```go
// pkg/memory/store_test.go
package memory_test

import (
	"os"
	"path/filepath"
	"testing"
	"mini-code/pkg/memory"
)

func TestStore_OpenCreatesTablesAutomatically(t *testing.T) {
	dir := t.TempDir()
	globalPath := filepath.Join(dir, "memory.db")
	projectPath := filepath.Join(dir, "project.db")

	store, err := memory.Open(globalPath, projectPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	// 验证表存在：写入一条 session_memory 不报错
	err = store.SaveSessionMemory("test-workspace", "test content", 72)
	if err != nil {
		t.Fatalf("SaveSessionMemory failed: %v", err)
	}
}

func TestStore_RememberAndRecall(t *testing.T) {
	dir := t.TempDir()
	store, err := memory.Open(
		filepath.Join(dir, "memory.db"),
		filepath.Join(dir, "project.db"),
	)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	id, err := store.Remember("这个项目使用 pnpm 而不是 npm", "project", dir, []string{"tooling"})
	if err != nil {
		t.Fatalf("Remember failed: %v", err)
	}
	if id == 0 {
		t.Fatal("expected non-zero id")
	}

	results, err := store.Recall("pnpm", "project", dir, 10)
	if err != nil {
		t.Fatalf("Recall failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one recall result")
	}
	if results[0].Content != "这个项目使用 pnpm 而不是 npm" {
		t.Errorf("unexpected content: %q", results[0].Content)
	}
}

func TestStore_Forget(t *testing.T) {
	dir := t.TempDir()
	store, err := memory.Open(
		filepath.Join(dir, "memory.db"),
		filepath.Join(dir, "project.db"),
	)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	id, _ := store.Remember("临时记忆", "user", "", nil)
	err = store.Forget(id)
	if err != nil {
		t.Fatalf("Forget failed: %v", err)
	}

	results, _ := store.Recall("临时记忆", "", "", 10)
	if len(results) != 0 {
		t.Errorf("expected 0 results after forget, got %d", len(results))
	}
}
```

- [ ] **Step 2.2: 运行测试确认失败**

```bash
go test ./pkg/memory/... 2>&1
```
预期：`cannot find package`

- [ ] **Step 2.3: 创建 store.go**

```go
// pkg/memory/store.go
package memory

import (
	"database/sql"
	"fmt"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// MemoryRecord 表示一条长期记忆
type MemoryRecord struct {
	ID        int64
	Scope     string   // "project" | "user" | "global"
	Workspace string
	Content   string
	Tags      []string
	CreatedAt time.Time
}

// SessionRecord 表示一条短期记忆
type SessionRecord struct {
	ID        int64
	Workspace string
	Content   string
	CreatedAt time.Time
	ExpiresAt time.Time
}

// Store 管理两个 SQLite 连接：全局 DB 和项目 DB
type Store struct {
	globalDB  *sql.DB
	projectDB *sql.DB
	mu        sync.Mutex
}

// Open 打开（或创建）两个 SQLite 数据库并自动建表
func Open(globalPath, projectPath string) (*Store, error) {
	gdb, err := openDB(globalPath)
	if err != nil {
		return nil, fmt.Errorf("open global db: %w", err)
	}
	pdb, err := openDB(projectPath)
	if err != nil {
		gdb.Close()
		return nil, fmt.Errorf("open project db: %w", err)
	}
	s := &Store{globalDB: gdb, projectDB: pdb}
	if err := s.migrate(); err != nil {
		s.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

func openDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // SQLite 单写连接
	if _, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON;`); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func (s *Store) migrate() error {
	// 全局 DB：session_memory + user/global long_term_memory + FTS5
	globalSchema := `
CREATE TABLE IF NOT EXISTS session_memory (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    workspace   TEXT NOT NULL,
    content     TEXT NOT NULL,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at  DATETIME NOT NULL
);
CREATE TABLE IF NOT EXISTS long_term_memory (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    scope       TEXT NOT NULL CHECK(scope IN ('project','user','global')),
    workspace   TEXT,
    content     TEXT NOT NULL,
    tags        TEXT,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE VIRTUAL TABLE IF NOT EXISTS memory_fts
    USING fts5(content, tags, content=long_term_memory, content_rowid=id);
CREATE TRIGGER IF NOT EXISTS memory_fts_insert AFTER INSERT ON long_term_memory BEGIN
    INSERT INTO memory_fts(rowid, content, tags) VALUES (new.id, new.content, new.tags);
END;
CREATE TRIGGER IF NOT EXISTS memory_fts_delete AFTER DELETE ON long_term_memory BEGIN
    INSERT INTO memory_fts(memory_fts, rowid, content, tags) VALUES ('delete', old.id, old.content, old.tags);
END;`
	if _, err := s.globalDB.Exec(globalSchema); err != nil {
		return fmt.Errorf("global schema: %w", err)
	}

	// 项目 DB：仅 project scope 的 long_term_memory + FTS5
	projectSchema := `
CREATE TABLE IF NOT EXISTS long_term_memory (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    scope       TEXT NOT NULL DEFAULT 'project',
    workspace   TEXT,
    content     TEXT NOT NULL,
    tags        TEXT,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE VIRTUAL TABLE IF NOT EXISTS memory_fts
    USING fts5(content, tags, content=long_term_memory, content_rowid=id);
CREATE TRIGGER IF NOT EXISTS memory_fts_insert AFTER INSERT ON long_term_memory BEGIN
    INSERT INTO memory_fts(rowid, content, tags) VALUES (new.id, new.content, new.tags);
END;
CREATE TRIGGER IF NOT EXISTS memory_fts_delete AFTER DELETE ON long_term_memory BEGIN
    INSERT INTO memory_fts(memory_fts, rowid, content, tags) VALUES ('delete', old.id, old.content, old.tags);
END;`
	if _, err := s.projectDB.Exec(projectSchema); err != nil {
		return fmt.Errorf("project schema: %w", err)
	}
	return nil
}

func (s *Store) Close() {
	if s.globalDB != nil {
		s.globalDB.Close()
	}
	if s.projectDB != nil {
		s.projectDB.Close()
	}
}

// dbForScope 根据 scope 返回对应的 DB
func (s *Store) dbForScope(scope string) *sql.DB {
	if scope == "project" {
		return s.projectDB
	}
	return s.globalDB
}
```

- [ ] **Step 2.4: 运行测试（此时还缺少 SaveSessionMemory/Remember/Recall/Forget）**

```bash
go test ./pkg/memory/... 2>&1
```
预期：编译失败，提示缺少方法

---

## Task 3: Session Memory + Long-term CRUD

**Files:**
- Create: `pkg/memory/session.go`
- Create: `pkg/memory/longterm.go`

- [ ] **Step 3.1: 创建 session.go**

```go
// pkg/memory/session.go
package memory

import "time"

// SaveSessionMemory 保存短期记忆，ttlHours 为过期小时数
func (s *Store) SaveSessionMemory(workspace, content string, ttlHours int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	expires := time.Now().Add(time.Duration(ttlHours) * time.Hour)
	_, err := s.globalDB.Exec(
		`INSERT INTO session_memory (workspace, content, expires_at) VALUES (?, ?, ?)`,
		workspace, content, expires.Format(time.RFC3339),
	)
	return err
}

// LoadSessionMemory 加载指定工作区未过期的 Session 记忆，按创建时间倒序
func (s *Store) LoadSessionMemory(workspace string, limit int) ([]SessionRecord, error) {
	rows, err := s.globalDB.Query(
		`SELECT id, workspace, content, created_at, expires_at
		 FROM session_memory
		 WHERE workspace = ? AND expires_at > datetime('now')
		 ORDER BY created_at DESC LIMIT ?`,
		workspace, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []SessionRecord
	for rows.Next() {
		var r SessionRecord
		var createdStr, expiresStr string
		if err := rows.Scan(&r.ID, &r.Workspace, &r.Content, &createdStr, &expiresStr); err != nil {
			return nil, err
		}
		r.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
		r.ExpiresAt, _ = time.Parse(time.RFC3339, expiresStr)
		records = append(records, r)
	}
	return records, rows.Err()
}

// PurgeExpiredSessionMemory 删除过期的 Session 记忆（可定期调用）
func (s *Store) PurgeExpiredSessionMemory() error {
	_, err := s.globalDB.Exec(`DELETE FROM session_memory WHERE expires_at <= datetime('now')`)
	return err
}
```

- [ ] **Step 3.2: 创建 longterm.go**

```go
// pkg/memory/longterm.go
package memory

import (
	"database/sql"
	"encoding/json"
	"fmt"
)

// Remember 向 Fact Store 写入一条长期记忆，返回记忆 ID
func (s *Store) Remember(content, scope, workspace string, tags []string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if scope != "project" && scope != "user" && scope != "global" {
		return 0, fmt.Errorf("invalid scope %q: must be project/user/global", scope)
	}

	tagsJSON := "[]"
	if len(tags) > 0 {
		b, _ := json.Marshal(tags)
		tagsJSON = string(b)
	}

	db := s.dbForScope(scope)
	result, err := db.Exec(
		`INSERT INTO long_term_memory (scope, workspace, content, tags) VALUES (?, ?, ?, ?)`,
		scope, workspace, content, tagsJSON,
	)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

// Forget 删除指定 ID 的长期记忆（先在两个 DB 中查找）
func (s *Store) Forget(id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, db := range []*sql.DB{s.globalDB, s.projectDB} {
		res, err := db.Exec(`DELETE FROM long_term_memory WHERE id = ?`, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			return nil
		}
	}
	return fmt.Errorf("memory id %d not found", id)
}

// RecallResult 表示 Recall 的单条结果
type RecallResult struct {
	ID        int64
	Scope     string
	Workspace string
	Content   string
	Tags      []string
}

// Recall 用 FTS5 全文检索相关记忆
// scope 为空时搜索所有 scope；workspace 用于 project scope 过滤
func (s *Store) Recall(query, scope, workspace string, limit int) ([]RecallResult, error) {
	var results []RecallResult

	dbs := []*sql.DB{s.globalDB, s.projectDB}
	if scope == "project" {
		dbs = []*sql.DB{s.projectDB}
	} else if scope == "user" || scope == "global" {
		dbs = []*sql.DB{s.globalDB}
	}

	for _, db := range dbs {
		rows, err := db.Query(
			`SELECT m.id, m.scope, m.workspace, m.content, m.tags
			 FROM memory_fts f
			 JOIN long_term_memory m ON m.id = f.rowid
			 WHERE memory_fts MATCH ?
			 ORDER BY rank LIMIT ?`,
			query, limit,
		)
		if err != nil {
			continue // FTS5 查询失败时跳过，不中断
		}
		for rows.Next() {
			var r RecallResult
			var tagsJSON string
			if err := rows.Scan(&r.ID, &r.Scope, &r.Workspace, &r.Content, &tagsJSON); err != nil {
				continue
			}
			json.Unmarshal([]byte(tagsJSON), &r.Tags)
			results = append(results, r)
		}
		rows.Close()
	}
	return results, nil
}

// LoadLongTermMemory 加载指定 scope/workspace 的所有长期记忆（用于 system prompt 注入）
func (s *Store) LoadLongTermMemory(scope, workspace string, limit int) ([]RecallResult, error) {
	db := s.dbForScope(scope)
	q := `SELECT id, scope, workspace, content, tags FROM long_term_memory WHERE scope = ?`
	args := []interface{}{scope}
	if scope == "project" && workspace != "" {
		q += ` AND workspace = ?`
		args = append(args, workspace)
	}
	q += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)

	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []RecallResult
	for rows.Next() {
		var r RecallResult
		var tagsJSON string
		if err := rows.Scan(&r.ID, &r.Scope, &r.Workspace, &r.Content, &tagsJSON); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(tagsJSON), &r.Tags)
		results = append(results, r)
	}
	return results, rows.Err()
}
```

- [ ] **Step 3.3: 运行测试**

```bash
go test ./pkg/memory/... -v
```
预期：全部 PASS

- [ ] **Step 3.4: Commit**

```bash
git add pkg/memory/
git commit -m "feat(memory): SQLite 三层记忆存储（Session/LongTerm/FTS5）"
```

---

## Task 4: Memory → System Prompt 注入

**Files:**
- Create: `pkg/memory/prompt.go`
- Create: `pkg/memory/prompt_test.go`

- [ ] **Step 4.1: 写测试**

```go
// pkg/memory/prompt_test.go
package memory_test

import (
	"path/filepath"
	"strings"
	"testing"
	"mini-code/pkg/memory"
)

func TestBuildPromptSuffix_ContainsRememberedFacts(t *testing.T) {
	dir := t.TempDir()
	store, _ := memory.Open(
		filepath.Join(dir, "memory.db"),
		filepath.Join(dir, "project.db"),
	)
	defer store.Close()

	store.Remember("使用 pnpm 而不是 npm", "project", dir, nil)
	store.Remember("回复语言：中文", "user", "", nil)
	store.SaveSessionMemory(dir, "昨天完成了认证模块", 72)

	suffix := store.BuildPromptSuffix(dir)

	if !strings.Contains(suffix, "pnpm") {
		t.Error("expected project memory in suffix")
	}
	if !strings.Contains(suffix, "中文") {
		t.Error("expected user preference in suffix")
	}
	if !strings.Contains(suffix, "认证模块") {
		t.Error("expected session memory in suffix")
	}
	if len(suffix) > 2000 {
		t.Errorf("suffix too long: %d chars (max 2000)", len(suffix))
	}
}
```

- [ ] **Step 4.2: 运行测试确认失败**

```bash
go test ./pkg/memory/... -run TestBuildPromptSuffix -v
```
预期：`undefined: store.BuildPromptSuffix`

- [ ] **Step 4.3: 创建 prompt.go**

```go
// pkg/memory/prompt.go
package memory

import (
	"fmt"
	"strings"
)

const maxPromptSuffixChars = 2000

// BuildPromptSuffix 构建记忆注入片段，追加在 system prompt 末尾
// 截断优先级：Session Memory > project LongTerm > user LongTerm
func (s *Store) BuildPromptSuffix(workspace string) string {
	var sb strings.Builder

	// 1. 用户偏好（user scope）
	userMems, _ := s.LoadLongTermMemory("user", "", 20)
	if len(userMems) > 0 {
		sb.WriteString("\n\n## 用户偏好\n")
		for _, m := range userMems {
			sb.WriteString("- ")
			sb.WriteString(m.Content)
			sb.WriteString("\n")
		}
	}

	// 2. 项目记忆（project scope）
	projMems, _ := s.LoadLongTermMemory("project", workspace, 20)
	if len(projMems) > 0 {
		sb.WriteString("\n## 项目记忆\n")
		for _, m := range projMems {
			sb.WriteString("- ")
			sb.WriteString(m.Content)
			sb.WriteString("\n")
		}
	}

	// 3. Session Memory（近 72h 摘要）
	sessions, _ := s.LoadSessionMemory(workspace, 5)
	if len(sessions) > 0 {
		sb.WriteString("\n## 近期上下文（过去 72 小时）\n")
		for _, m := range sessions {
			sb.WriteString(fmt.Sprintf("- %s: %s\n",
				m.CreatedAt.Format("2006-01-02"), m.Content))
		}
	}

	result := sb.String()

	// 截断到最大长度
	if len(result) > maxPromptSuffixChars {
		result = result[:maxPromptSuffixChars]
		// 确保不截断在汉字中间
		result = strings.TrimRight(result, "\n-: ")
		result += "\n... (记忆已截断)"
	}

	return result
}
```

- [ ] **Step 4.4: 运行测试**

```bash
go test ./pkg/memory/... -v
```
预期：全部 PASS

- [ ] **Step 4.5: Commit**

```bash
git add pkg/memory/prompt.go pkg/memory/prompt_test.go
git commit -m "feat(memory): 实现记忆注入 system prompt 格式化（2000字符上限）"
```

---

## Task 5: remember / forget / recall 工具

**Files:**
- Create: `pkg/tools/memory_tools.go`
- Modify: `pkg/tools/registry.go`

- [ ] **Step 5.1: 写工具测试**

```go
// pkg/tools/memory_tools_test.go
package tools_test

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"mini-code/pkg/memory"
	"mini-code/pkg/tools"
)

func setupMemoryTools(t *testing.T) *memory.Store {
	dir := t.TempDir()
	store, err := memory.Open(
		filepath.Join(dir, "memory.db"),
		filepath.Join(dir, "project.db"),
	)
	if err != nil {
		t.Fatalf("Open store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	tools.SetMemoryStore(store) // 注入 store 到工具层
	return store
}

func TestRememberTool_StoresAndReturnsID(t *testing.T) {
	setupMemoryTools(t)

	args, _ := json.Marshal(map[string]interface{}{
		"content": "使用 Go 1.22",
		"scope":   "project",
	})

	result, err := tools.Executors["remember"](string(args))
	if err != nil {
		t.Fatalf("remember failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestForgetTool_DeletesMemory(t *testing.T) {
	store := setupMemoryTools(t)

	id, _ := store.Remember("临时记忆", "user", "", nil)
	args, _ := json.Marshal(map[string]interface{}{"memory_id": id})

	_, err := tools.Executors["forget"](string(args))
	if err != nil {
		t.Fatalf("forget failed: %v", err)
	}

	results, _ := store.Recall("临时记忆", "", "", 10)
	if len(results) != 0 {
		t.Error("expected memory to be deleted")
	}
}

func TestRecallTool_FindsRelevantMemory(t *testing.T) {
	store := setupMemoryTools(t)
	store.Remember("项目使用 PostgreSQL 数据库", "project", ".", nil)

	args, _ := json.Marshal(map[string]interface{}{"query": "数据库"})
	result, err := tools.Executors["recall"](string(args))
	if err != nil {
		t.Fatalf("recall failed: %v", err)
	}
	// Executors 返回 interface{}，转为字符串比较
	resultStr, ok := result.(string)
	if !ok || resultStr == "" || resultStr == "未找到相关记忆" {
		t.Errorf("expected non-empty recall result, got: %v", result)
	}
}
```

- [ ] **Step 5.2: 运行测试确认失败**

```bash
go test ./pkg/tools/... -run TestRememberTool -v
```
预期：`undefined: tools.SetMemoryStore`

- [ ] **Step 5.3: 创建 memory_tools.go**

// 注：`parseArgs()` 已在 `pkg/tools/file.go:279` 定义，`workspaceRoot()` 已在
// `pkg/tools/workspace.go:10` 定义，同包内可直接调用，无需重新实现。

```go
// pkg/tools/memory_tools.go
package tools

import (
	"fmt"
	"mini-code/pkg/memory"
	"strings"
)

var globalMemoryStore *memory.Store

// SetMemoryStore 注入 Memory Store（由 main.go 在启动时调用）
func SetMemoryStore(store *memory.Store) {
	globalMemoryStore = store
}

// RememberArguments remember 工具参数
type RememberArguments struct {
	Content string   `json:"content" validate:"required" jsonschema:"required" jsonschema_description:"要记住的事实，自然语言描述"`
	Scope   string   `json:"scope" validate:"required" jsonschema:"required" jsonschema_description:"记忆范围：project（当前项目）| user（用户偏好）| global（通用知识）"`
	Tags    []string `json:"tags" jsonschema_description:"可选标签，便于检索"`
}

func Remember(args interface{}) (interface{}, error) {
	var params RememberArguments
	if err := parseArgs(args, &params); err != nil {
		return nil, err
	}
	if globalMemoryStore == nil {
		return nil, fmt.Errorf("memory store 未初始化")
	}

	workspace, _ := workspaceRoot()
	id, err := globalMemoryStore.Remember(params.Content, params.Scope, workspace, params.Tags)
	if err != nil {
		return nil, fmt.Errorf("保存记忆失败: %w", err)
	}
	return fmt.Sprintf("已记住（ID: %d）: %s", id, params.Content), nil
}

// ForgetArguments forget 工具参数
type ForgetArguments struct {
	MemoryID int64 `json:"memory_id" validate:"required" jsonschema:"required" jsonschema_description:"要删除的记忆 ID（由 remember 工具返回）"`
}

func Forget(args interface{}) (interface{}, error) {
	var params ForgetArguments
	if err := parseArgs(args, &params); err != nil {
		return nil, err
	}
	if globalMemoryStore == nil {
		return nil, fmt.Errorf("memory store 未初始化")
	}
	if err := globalMemoryStore.Forget(params.MemoryID); err != nil {
		return nil, fmt.Errorf("删除记忆失败: %w", err)
	}
	return fmt.Sprintf("已删除记忆 ID: %d", params.MemoryID), nil
}

// RecallArguments recall 工具参数
type RecallArguments struct {
	Query string `json:"query" validate:"required" jsonschema:"required" jsonschema_description:"自然语言检索词"`
	Scope string `json:"scope" jsonschema_description:"限定检索范围：project | user | global，不填则全局搜索"`
}

func Recall(args interface{}) (interface{}, error) {
	var params RecallArguments
	if err := parseArgs(args, &params); err != nil {
		return nil, err
	}
	if globalMemoryStore == nil {
		return nil, fmt.Errorf("memory store 未初始化")
	}

	workspace, _ := workspaceRoot()
	results, err := globalMemoryStore.Recall(params.Query, params.Scope, workspace, 10)
	if err != nil {
		return nil, fmt.Errorf("检索记忆失败: %w", err)
	}
	if len(results) == 0 {
		return "未找到相关记忆", nil
	}

	var sb strings.Builder
	for _, r := range results {
		sb.WriteString(fmt.Sprintf("[ID:%d][%s] %s\n", r.ID, r.Scope, r.Content))
	}
	return strings.TrimSpace(sb.String()), nil
}
```

- [ ] **Step 5.4: 在 registry.go 注册三个工具**

在 `pkg/tools/registry.go` 的 `init()` 函数中追加：

```go
// 记忆工具
register("remember", "记住一个事实到长期记忆（project/user/global 三种范围）", RememberArguments{}, Remember)
register("forget", "删除一条长期记忆（通过 remember 返回的 ID）", ForgetArguments{}, Forget)
register("recall", "从长期记忆中检索相关信息", RecallArguments{}, Recall)
```

- [ ] **Step 5.5: 运行测试**

```bash
go test ./pkg/tools/... -run TestRememberTool -v
go test ./pkg/tools/... -run TestForgetTool -v
go test ./pkg/tools/... -run TestRecallTool -v
```
预期：全部 PASS

- [ ] **Step 5.6: Commit**

```bash
git add pkg/tools/memory_tools.go pkg/tools/memory_tools_test.go pkg/tools/registry.go
git commit -m "feat(tools): 注册 remember/forget/recall 三个记忆工具"
```

---

## Task 6: Orchestrator 集成 Memory

**Files:**
- Modify: `pkg/orchestrator/orchestrator.go`
- Modify: `cmd/agent/main.go`

- [ ] **Step 6.0: 更新 Orchestrator 结构体，添加 memStore 字段**

将 `pkg/orchestrator/orchestrator.go` 中的 `Orchestrator` 结构体和 `New()` 函数修改为：

```go
import "mini-code/pkg/memory"

type Orchestrator struct {
	sessions  map[string]*Session
	mu        sync.Mutex
	memStore  *memory.Store // 替换原来的 interface{}
}

func New(memStore *memory.Store) *Orchestrator {
	o := &Orchestrator{
		sessions: make(map[string]*Session),
		memStore: memStore,
	}
	go o.evictLoop()
	return o
}
```

同时在文件顶部的 `workspaceRoot()` 调用处添加说明：
> `workspaceRoot()` 来自 `pkg/tools/workspace.go`，需要导入：`"mini-code/pkg/tools"` 并调用 `tools.WorkspaceRoot()`，或者在 orchestrator.go 中直接使用 `os.Getwd()`。建议用 `os.Getwd()` 避免循环依赖。

将所有 `workspaceRoot()` 调用替换为：
```go
workspace, _ := os.Getwd()
```

- [ ] **Step 6.1: 更新 Orchestrator 注入记忆到 system prompt**

在 `orchestrator.go` 的 `buildSystemPrompt()` 函数中，当 `memStore != nil` 时追加记忆片段：

```go
func (o *Orchestrator) buildSystemPromptWithMemory(workspace string) string {
	base := buildBaseSystemPrompt() // Phase 1 中已定义的 buildBaseSystemPrompt()
	if o.memStore == nil {
		return base
	}
	suffix := o.memStore.BuildPromptSuffix(workspace)
	return base + suffix
}
```

在 `GetOrCreateSession` 中使用新函数：

```go
func (o *Orchestrator) GetOrCreateSession(channelID, userID string) *Session {
	key := channelID + ":" + userID
	o.mu.Lock()
	defer o.mu.Unlock()
	if s, ok := o.sessions[key]; ok {
		return s
	}
	workspace, _ := os.Getwd() // 不使用 tools.workspaceRoot() 避免循环依赖
	prompt := o.buildSystemPromptWithMemory(workspace)
	s := newSession(channelID, userID, prompt)
	o.sessions[key] = s
	return s
}
```

- [ ] **Step 6.2: 在 Handle 完成后异步写 Session Memory**

在 `Handle()` 方法末尾添加异步写入：

```go
// 任务完成后异步更新 Session Memory（不阻塞响应）
if o.memStore != nil && reply != "" {
	go func() {
		workspace, _ := os.Getwd()
		// 取响应前 200 字作为摘要
		summary := reply
		if len([]rune(summary)) > 200 {
			runes := []rune(summary)
			summary = string(runes[:200])
		}
		_ = o.memStore.SaveSessionMemory(workspace, summary, 72)
	}()
}
```

- [ ] **Step 6.3: 更新 main.go 初始化 Memory Store**

```go
// cmd/agent/main.go 中，在创建 Orchestrator 前初始化 Memory Store
import "mini-code/pkg/memory"

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

orch := orchestrator.New(memStore)
```

- [ ] **Step 6.4: 添加 .gitignore 条目**

```bash
echo ".mini-code/" >> .gitignore
```

- [ ] **Step 6.5: 运行全部测试**

```bash
go test ./... -count=1
```
预期：全部 PASS

- [ ] **Step 6.6: 编译验证**

```bash
go build ./cmd/agent && echo "BUILD OK"
```

- [ ] **Step 6.7: 手动验收测试**

启动程序，测试 remember 工具：
```
> 记住：这个项目使用 modernc.org/sqlite 作为 SQLite 驱动
（Agent 应调用 remember 工具并返回 ID）
> exit
（重启程序）
> 我们之前用什么 SQLite 驱动？
（Agent 应在 system prompt 中看到记忆并正确回答）
```

- [ ] **Step 6.8: 最终 Commit**

```bash
git add .
git commit -m "feat(phase2): 完成 Memory 层集成，支持三层记忆和 remember/forget/recall 工具"
```

---

## Phase 2 验收标准

```bash
# 1. 全部测试通过
go test ./... -count=1

# 2. 编译成功
go build ./cmd/agent

# 3. 手动验证：remember 后重开会话能感知记忆
```
