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