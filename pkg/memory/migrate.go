package memory

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"mini-code/pkg/textutil"

	_ "modernc.org/sqlite"
)

// 旧版 SQLite 记忆的一次性迁移。迁移完成后在自动记忆目录写入标记文件，
// 项目库 .mini-code/project.db 改名为 .bak；全局库 ~/.mini-code/memory.db
// 被多个工作区共享，保持原样，每个工作区各自迁移其中的 user/global 记忆。
// TODO: 下个版本删除本文件与 modernc.org/sqlite 依赖。
const migratedMarker = ".migrated-from-sqlite"

// LegacyDBPaths 返回旧版全局库与项目库路径。
func LegacyDBPaths(home, workspace string) (global, project string) {
	return filepath.Join(UserDir(home), "memory.db"), filepath.Join(workspace, configDirName, "project.db")
}

// MigrateLegacy 把旧库中的长期记忆导出为自动记忆文件，返回迁移条数。
// 已迁移过或旧库不存在时返回 0, nil。
func (c *Context) MigrateLegacy() (int, error) {
	if c == nil || c.Auto == nil {
		return 0, nil
	}
	marker := filepath.Join(c.Auto.Dir(), migratedMarker)
	if isRegularFile(marker) {
		return 0, nil
	}
	globalPath, projectPath := LegacyDBPaths(c.home, c.workspace)

	total := 0
	for _, p := range []string{globalPath, projectPath} {
		if !isRegularFile(p) {
			continue
		}
		n, err := c.migrateDB(p)
		if err != nil {
			return total, fmt.Errorf("迁移 %s 失败: %w", p, err)
		}
		total += n
	}
	if isRegularFile(projectPath) {
		for _, suffix := range []string{"", "-wal", "-shm"} {
			if isRegularFile(projectPath + suffix) {
				_ = os.Rename(projectPath+suffix, projectPath+suffix+".bak")
			}
		}
	}
	if err := os.WriteFile(marker, []byte("ok\n"), 0o644); err != nil {
		return total, err
	}
	return total, nil
}

func (c *Context) migrateDB(path string) (int, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return 0, err
	}
	defer db.Close()

	rows, err := db.Query(`SELECT id, scope, content FROM long_term_memory ORDER BY id`)
	if err != nil {
		if strings.Contains(err.Error(), "no such table") {
			return 0, nil
		}
		return 0, err
	}
	defer rows.Close()

	n := 0
	for rows.Next() {
		var id int64
		var scope, content string
		if err := rows.Scan(&id, &scope, &content); err != nil {
			return n, err
		}
		content = strings.TrimSpace(content)
		if content == "" {
			continue
		}
		typ := map[string]string{"user": "user", "project": "project"}[scope]
		if typ == "" {
			typ = "reference"
		}
		desc := textutil.TruncateRunes(strings.Join(strings.Fields(content), " "), 80)
		name := fmt.Sprintf("legacy-%s-%d", scope, id)
		if _, _, err := c.Auto.Save(name, desc, typ, content); err != nil {
			return n, err
		}
		n++
	}
	return n, rows.Err()
}
