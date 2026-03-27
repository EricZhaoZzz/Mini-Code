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