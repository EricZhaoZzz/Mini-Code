package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

// ================== Git Status ==================

type GitStatusArguments struct {
	Path string `json:"path" jsonschema_description:"Git 仓库路径，留空表示当前工作区"`
}

type GitStatusResult struct {
	Branch       string          `json:"branch,omitempty"`
	IsClean      bool            `json:"is_clean"`
	Staged       []GitFileStatus `json:"staged,omitempty"`
	Unstaged     []GitFileStatus `json:"unstaged,omitempty"`
	Untracked    []string        `json:"untracked,omitempty"`
	Ahead        int             `json:"ahead,omitempty"`
	Behind       int             `json:"behind,omitempty"`
	TotalChanges int             `json:"total_changes"`
}

type GitFileStatus struct {
	Path     string `json:"path"`
	Staging  string `json:"staging"`
	Worktree string `json:"worktree"`
}

func GitStatus(args interface{}) (interface{}, error) {
	var params GitStatusArguments
	if err := parseArgs(args, &params); err != nil {
		return nil, err
	}

	repoPath, err := resolveWorkspacePath(params.Path)
	if err != nil {
		return nil, err
	}

	// 打开 Git 仓库
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		if err == git.ErrRepositoryNotExists {
			// 尝试向上查找 Git 仓库
			parentRepo, parentErr := findParentGitRepo(repoPath)
			if parentErr != nil {
				return nil, fmt.Errorf("当前目录不是 Git 仓库: %s", displayPath(repoPath))
			}
			repo = parentRepo
		} else {
			return nil, fmt.Errorf("打开 Git 仓库失败: %w", err)
		}
	}

	// 获取工作树
	worktree, err := repo.Worktree()
	if err != nil {
		return nil, fmt.Errorf("获取工作树失败: %w", err)
	}

	// 获取状态
	s, err := worktree.Status()
	if err != nil {
		return nil, fmt.Errorf("获取 Git 状态失败: %w", err)
	}

	result := &GitStatusResult{
		IsClean: s.IsClean(),
	}

	// 获取当前分支
	if head, err := repo.Head(); err == nil {
		result.Branch = head.Name().Short()
	}

	// 解析文件状态
	for file, fs := range s {
		// 跳过未修改的文件
		if fs.Staging == git.Unmodified && fs.Worktree == git.Unmodified {
			continue
		}

		if fs.Staging == git.Untracked && fs.Worktree == git.Untracked {
			result.Untracked = append(result.Untracked, file)
		} else {
			item := GitFileStatus{
				Path:     file,
				Staging:  statusCodeString(fs.Staging),
				Worktree: statusCodeString(fs.Worktree),
			}

			if fs.Staging != git.Unmodified && fs.Staging != git.Untracked {
				result.Staged = append(result.Staged, item)
			}
			if fs.Worktree != git.Unmodified && fs.Worktree != git.Untracked {
				result.Unstaged = append(result.Unstaged, item)
			}
		}
	}

	// 计算总变更数
	result.TotalChanges = len(result.Staged) + len(result.Unstaged) + len(result.Untracked)

	// 获取 Ahead/Behind 信息
	if result.Branch != "" {
		if ahead, behind, err := getAheadBehind(repo, result.Branch); err == nil {
			result.Ahead = ahead
			result.Behind = behind
		}
	}

	return result, nil
}

func statusCodeString(code git.StatusCode) string {
	switch code {
	case git.Added:
		return "A"
	case git.Copied:
		return "C"
	case git.Deleted:
		return "D"
	case git.Modified:
		return "M"
	case git.Renamed:
		return "R"
	case git.UpdatedButUnmerged:
		return "U"
	case git.Unmodified:
		return " "
	case git.Untracked:
		return "?"
	default:
		return string(code)
	}
}

func findParentGitRepo(path string) (*git.Repository, error) {
	current := path
	for {
		gitDir := filepath.Join(current, ".git")
		if _, err := os.Stat(gitDir); err == nil {
			return git.PlainOpen(current)
		}

		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return nil, git.ErrRepositoryNotExists
}

func getAheadBehind(repo *git.Repository, branchName string) (int, int, error) {
	// 获取本地分支引用
	localRef, err := repo.Reference(plumbing.ReferenceName("refs/heads/"+branchName), true)
	if err != nil {
		return 0, 0, err
	}

	// 获取远程分支引用
	remoteRef, err := repo.Reference(plumbing.ReferenceName("refs/remotes/origin/"+branchName), true)
	if err != nil {
		return 0, 0, err
	}

	localHash := localRef.Hash()
	remoteHash := remoteRef.Hash()

	// 获取提交历史
	localCommits := make(map[plumbing.Hash]bool)
	remoteCommits := make(map[plumbing.Hash]bool)

	// 遍历本地分支的提交
	localIter, err := repo.Log(&git.LogOptions{From: localHash})
	if err == nil {
		for {
			commit, err := localIter.Next()
			if err != nil {
				break
			}
			localCommits[commit.Hash] = true
		}
		localIter.Close()
	}

	// 遍历远程分支的提交
	remoteIter, err := repo.Log(&git.LogOptions{From: remoteHash})
	if err == nil {
		for {
			commit, err := remoteIter.Next()
			if err != nil {
				break
			}
			remoteCommits[commit.Hash] = true
		}
		remoteIter.Close()
	}

	// 计算 ahead（本地有但远程没有）
	ahead := 0
	for hash := range localCommits {
		if !remoteCommits[hash] {
			ahead++
		}
	}

	// 计算 behind（远程有但本地没有）
	behind := 0
	for hash := range remoteCommits {
		if !localCommits[hash] {
			behind++
		}
	}

	return ahead, behind, nil
}

// ================== Git Diff ==================

type GitDiffArguments struct {
	Path        string `json:"path" jsonschema_description:"Git 仓库路径，留空表示当前工作区"`
	Staged      bool   `json:"staged" jsonschema_description:"显示已暂存的变更（类似 git diff --staged）"`
	Unstaged    bool   `json:"unstaged" jsonschema_description:"显示未暂存的变更"`
	File        string `json:"file" jsonschema_description:"指定文件路径，留空显示所有变更"`
}

type GitDiffResult struct {
	Files    []GitDiffFile `json:"files"`
	TotalAdd int           `json:"total_additions"`
	TotalDel int           `json:"total_deletions"`
	IsStaged bool          `json:"is_staged"`
}

type GitDiffFile struct {
	Path      string `json:"path"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	IsBinary  bool   `json:"is_binary,omitempty"`
	IsNew     bool   `json:"is_new,omitempty"`
	IsDeleted bool   `json:"is_deleted,omitempty"`
	IsRenamed bool   `json:"is_renamed,omitempty"`
	OldPath   string `json:"old_path,omitempty"`
	// 注意：Hunks 字段已被移除，因为我们使用 RunShell 来获取更详细的 diff
}

func GitDiff(args interface{}) (interface{}, error) {
	var params GitDiffArguments
	if err := parseArgs(args, &params); err != nil {
		return nil, err
	}

	// 默认显示未暂存的变更
	if !params.Staged && !params.Unstaged {
		params.Unstaged = true
	}

	repoPath, err := resolveWorkspacePath(params.Path)
	if err != nil {
		return nil, err
	}

	// 打开 Git 仓库
	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		if err == git.ErrRepositoryNotExists {
			parentRepo, parentErr := findParentGitRepo(repoPath)
			if parentErr != nil {
				return nil, fmt.Errorf("当前目录不是 Git 仓库: %s", displayPath(repoPath))
			}
			repo = parentRepo
		} else {
			return nil, fmt.Errorf("打开 Git 仓库失败: %w", err)
		}
	}

	// 获取工作树
	worktree, err := repo.Worktree()
	if err != nil {
		return nil, fmt.Errorf("获取工作树失败: %w", err)
	}

	// 获取状态
	s, err := worktree.Status()
	if err != nil {
		return nil, fmt.Errorf("获取状态失败: %w", err)
	}

	result := &GitDiffResult{
		IsStaged: params.Staged,
		Files:    []GitDiffFile{},
	}

	// 解析文件状态
	for file, fs := range s {
		// 如果指定了文件，过滤其他文件
		if params.File != "" && file != params.File {
			continue
		}

		diffFile := GitDiffFile{Path: file}

		// 根据状态判断变更类型
		if params.Staged {
			// 暂存区的变更
			switch fs.Staging {
			case git.Added:
				diffFile.IsNew = true
				diffFile.Additions = 1 // 简化估计
			case git.Deleted:
				diffFile.IsDeleted = true
				diffFile.Deletions = 1
			case git.Modified:
				diffFile.Additions = 1
				diffFile.Deletions = 1
			case git.Renamed:
				diffFile.IsRenamed = true
				// 从 Extra 字段获取旧路径
				if fs.Extra != "" {
					diffFile.OldPath = fs.Extra
				}
			default:
				continue
			}
		} else {
			// 工作区的变更
			switch fs.Worktree {
			case git.Untracked:
				diffFile.IsNew = true
				diffFile.Additions = 1
			case git.Modified:
				diffFile.Additions = 1
				diffFile.Deletions = 1
			case git.Deleted:
				diffFile.IsDeleted = true
				diffFile.Deletions = 1
			default:
				// 检查暂存区是否有变更（相对于 HEAD）
				if fs.Staging == git.Added {
					diffFile.IsNew = true
					diffFile.Additions = 1
				} else if fs.Staging == git.Modified {
					diffFile.Additions = 1
					diffFile.Deletions = 1
				} else {
					continue
				}
			}
		}

		result.Files = append(result.Files, diffFile)
		result.TotalAdd += diffFile.Additions
		result.TotalDel += diffFile.Deletions
	}

	if len(result.Files) == 0 {
		return "没有发现变更", nil
	}

	return result, nil
}

// ================== Git Log ==================

type GitLogArguments struct {
	Path    string `json:"path" jsonschema_description:"Git 仓库路径，留空表示当前工作区"`
	Count   int    `json:"count" jsonschema_description:"显示的提交数量，默认 10"`
}

type GitLogResult struct {
	Commits []GitCommit `json:"commits"`
}

type GitCommit struct {
	Hash         string   `json:"hash"`
	ShortHash    string   `json:"short_hash"`
	Author       string   `json:"author"`
	AuthorEmail  string   `json:"author_email,omitempty"`
	Date         string   `json:"date"`
	RelativeDate string   `json:"relative_date,omitempty"`
	Message      string   `json:"message"`
	Parents      []string `json:"parents,omitempty"`
}

func GitLog(args interface{}) (interface{}, error) {
	var params GitLogArguments
	if err := parseArgs(args, &params); err != nil {
		return nil, err
	}

	if params.Count <= 0 {
		params.Count = 10
	}

	repoPath, err := resolveWorkspacePath(params.Path)
	if err != nil {
		return nil, err
	}

	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		if err == git.ErrRepositoryNotExists {
			parentRepo, parentErr := findParentGitRepo(repoPath)
			if parentErr != nil {
				return nil, fmt.Errorf("当前目录不是 Git 仓库: %s", displayPath(repoPath))
			}
			repo = parentRepo
		} else {
			return nil, fmt.Errorf("打开 Git 仓库失败: %w", err)
		}
	}

	// 获取日志迭代器
	logOpts := &git.LogOptions{
		Order: git.LogOrderCommitterTime,
	}

	iterator, err := repo.Log(logOpts)
	if err != nil {
		return nil, fmt.Errorf("获取提交日志失败: %w", err)
	}
	defer iterator.Close()

	result := &GitLogResult{
		Commits: []GitCommit{},
	}

	count := 0
	for {
		commit, err := iterator.Next()
		if err != nil {
			break
		}

		hash := commit.Hash.String()
		shortHash := hash[:7]

		var parents []string
		for _, p := range commit.ParentHashes {
			parents = append(parents, p.String()[:7])
		}

		commitTime := commit.Author.When
		commitInfo := GitCommit{
			Hash:         hash,
			ShortHash:    shortHash,
			Author:       commit.Author.Name,
			AuthorEmail:  commit.Author.Email,
			Date:         commitTime.Format(time.RFC3339),
			RelativeDate: humanize.Time(commitTime),
			Message:      strings.TrimSpace(commit.Message),
			Parents:      parents,
		}

		result.Commits = append(result.Commits, commitInfo)
		count++
		if count >= params.Count {
			break
		}
	}

	if len(result.Commits) == 0 {
		return "暂无提交记录", nil
	}

	return result, nil
}