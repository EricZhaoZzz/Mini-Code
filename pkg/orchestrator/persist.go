package orchestrator

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"mini-code/pkg/ui"

	"github.com/sashabaranov/go-openai"
)

// DefaultSessionPath 返回工作区内的会话持久化文件路径。
// 位于 .mini-code/（已 gitignore）：会话可能包含读取过的文件内容，不应进入版本库。
func DefaultSessionPath(workspaceRoot string) string {
	return filepath.Join(workspaceRoot, ".mini-code", "sessions", "last.json")
}

// SaveSnapshot 原子地写入会话快照：先写临时文件再 rename，
// 进程在写入中途退出也不会留下半截 JSON 导致下次无法恢复。
func SaveSnapshot(path string, snapshot SessionSnapshot) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("创建会话目录失败: %w", err)
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("序列化会话快照失败: %w", err)
	}
	tmp := path + ".tmp"
	// 0600：会话中可能含有读取过的敏感文件内容
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("写入会话快照失败: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("保存会话快照失败: %w", err)
	}
	return nil
}

// LoadSnapshot 读取会话快照。
func LoadSnapshot(path string) (SessionSnapshot, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return SessionSnapshot{}, fmt.Errorf("读取会话快照失败: %w", err)
	}
	var snapshot SessionSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return SessionSnapshot{}, fmt.Errorf("解析会话快照失败: %w", err)
	}
	return snapshot, nil
}

// SavedSessionInfo 描述可恢复的会话，用于启动提示。
type SavedSessionInfo struct {
	Turns    int // 用户消息条数
	LastSeen time.Time
}

// InspectSavedSession 返回已保存会话的概要；不存在或无对话内容时 ok=false。
func InspectSavedSession(path string) (info SavedSessionInfo, ok bool) {
	snapshot, err := LoadSnapshot(path)
	if err != nil {
		return SavedSessionInfo{}, false
	}
	info.Turns = countUserMessages(snapshot.Messages)
	info.LastSeen = snapshot.LastSeen
	return info, info.Turns > 0
}

func countUserMessages(msgs []openai.ChatCompletionMessage) int {
	n := 0
	for _, m := range msgs {
		if m.Role == openai.ChatMessageRoleUser {
			n++
		}
	}
	return n
}

// SetPersistPath 启用会话持久化：每次任务结束后把会话写入 path。空串表示关闭。
func (o *Orchestrator) SetPersistPath(path string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.persistPath = path
}

func (o *Orchestrator) getPersistPath() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.persistPath
}

// persist 保存会话。失败只提示不中断：持久化是附加能力，不能影响当前任务结果。
// 没有任何用户消息的会话不保存，避免空会话覆盖掉上一次有价值的记录。
func (o *Orchestrator) persist(session *Session) {
	path := o.getPersistPath()
	if path == "" {
		return
	}
	snapshot := session.ExportSnapshot()
	if countUserMessages(snapshot.Messages) == 0 {
		return
	}
	if err := SaveSnapshot(path, snapshot); err != nil {
		ui.PrintWarning("会话保存失败（不影响当前任务）: %v", err)
	}
}

// ResumeSession 用已保存的会话替换当前会话，返回恢复的用户消息条数。
//
// 系统提示会按当前状态重建：保存时的提示可能已过期（记忆、沙箱状态变化），
// 而对话内容本身才是需要恢复的部分。
func (o *Orchestrator) ResumeSession(channelID, userID string) (int, error) {
	path := o.getPersistPath()
	if path == "" {
		return 0, fmt.Errorf("会话持久化未启用")
	}
	snapshot, err := LoadSnapshot(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return 0, fmt.Errorf("没有可恢复的会话")
		}
		return 0, err
	}
	turns := countUserMessages(snapshot.Messages)
	if turns == 0 {
		return 0, fmt.Errorf("没有可恢复的会话")
	}

	// 快照可能来自不同的 channel/user 标识，统一挂到当前会话上
	snapshot.ChannelID = channelID
	snapshot.UserID = userID
	snapshot.ID = channelID + ":" + userID
	o.RestoreSession(snapshot)
	o.RefreshSessionPrompt(channelID, userID)
	return turns, nil
}
