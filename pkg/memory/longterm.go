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