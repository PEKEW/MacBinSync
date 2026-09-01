package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// SyncResult 同步/测试操作的结果，直接返回给 Web 前端展示。
type SyncResult struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

// syncDir git 克隆的本地工作目录 ~/.macsync/sync。
func syncDir() string {
	return filepath.Join(dataDir(), "sync")
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}

// normalizeRepoURL 把用户输入的仓库名/URL 归一化为 git clone URL + owner/name。
// 支持: "owner/name"、"https://github.com/owner/name(.git)"、"git@github.com:owner/name.git"
func normalizeRepoURL(repo string) (cloneURL, ownerName string, err error) {
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return "", "", fmt.Errorf("仓库为空")
	}
	switch {
	case strings.HasPrefix(repo, "https://") || strings.HasPrefix(repo, "http://"):
		cloneURL = repo
		if !strings.HasSuffix(cloneURL, ".git") {
			cloneURL += ".git"
		}
	case strings.HasPrefix(repo, "git@") || strings.HasPrefix(repo, "ssh://"):
		cloneURL = repo
	default:
		if strings.Count(repo, "/") != 1 {
			return "", "", fmt.Errorf("仓库格式无效，应为 owner/name 或完整 URL")
		}
		cloneURL = "https://github.com/" + repo + ".git"
	}
	re := regexp.MustCompile(`[:/]([^/:]+/[^/:]+?)(?:\.git)?$`)
	if m := re.FindStringSubmatch(cloneURL); m != nil {
		ownerName = m[1]
	}
	return cloneURL, ownerName, nil
}

// setupGitAuth 若存在 gh，则把 gh 注册为 git 凭据助手（幂等、无害）。
func setupGitAuth() {
	if gh := findExec("gh"); gh != "" {
		_, _ = runCmd(context.Background(), gh, "auth", "setup-git")
	}
}

// testSyncConnection 测试 GitHub 同步是否可用：优先 gh 认证，退化 git ls-remote。
func testSyncConnection() SyncResult {
	cfg := loadConfig()
	if cfg.GitHubRepo == "" {
		return SyncResult{OK: false, Message: "尚未配置 GitHub 同步仓库，请在设置中填写"}
	}
	cloneURL, ownerName, err := normalizeRepoURL(cfg.GitHubRepo)
	if err != nil {
		return SyncResult{OK: false, Message: err.Error()}
	}
	if gh := findExec("gh"); gh != "" {
		if out, err := runCmd(context.Background(), gh, "auth", "status"); err != nil {
			return SyncResult{OK: false, Message: "GitHub CLI 未登录: " + firstLine(out) + "。请在终端执行 gh auth login"}
		}
		if ownerName != "" {
			if _, err := runCmd(context.Background(), gh, "repo", "view", ownerName); err != nil {
				return SyncResult{OK: false, Message: fmt.Sprintf("无法访问仓库 %s（不存在或无权访问），请检查仓库名", ownerName)}
			}
		}
		return SyncResult{OK: true, Message: "已连接（GitHub CLI 认证）→ " + cfg.GitHubRepo}
	}
	out, err := runCmdLong(context.Background(), "git", "ls-remote", cloneURL, "HEAD")
	if err != nil {
		return SyncResult{OK: false, Message: "git 无法访问仓库: " + firstLine(out) + "。建议安装 gh 并登录: gh auth login"}
	}
	return SyncResult{OK: true, Message: "已连接（git 凭据认证）→ " + cfg.GitHubRepo}
}

// ensureClone 确保本地有同步仓库克隆（没有则 gh repo clone / git clone）。
func ensureClone(cloneURL string) error {
	dir := syncDir()
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		return nil
	}
	if entries, err := os.ReadDir(dir); err == nil && len(entries) == 0 {
		os.Remove(dir) // 清掉空残留目录
	}
	if gh := findExec("gh"); gh != "" {
		if _, err := runCmdLong(context.Background(), gh, "repo", "clone", cloneURL, dir); err == nil {
			return nil
		}
	}
	out, err := runCmdLong(context.Background(), "git", "clone", cloneURL, dir)
	if err != nil {
		return fmt.Errorf("克隆仓库失败: %s", firstLine(out))
	}
	return nil
}

func reportHostname() string {
	if rep, err := readCurrentReport(); err == nil && rep.Machine.Hostname != "" {
		return rep.Machine.Hostname
	}
	if h, err := os.Hostname(); err == nil {
		return h
	}
	return "unknown"
}

// copyReportsInto 把本机 reports 快照拷入同步仓库。
func copyReportsInto() error {
	dir := syncDir()
	os.MkdirAll(filepath.Join(dir, "reports"), 0o755)
	entries, err := os.ReadDir(filepath.Join(dataDir(), "reports"))
	if err != nil {
		return nil
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dataDir(), "reports", e.Name()))
		if err != nil {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, "reports", e.Name()), data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// gitIn 在同步仓库目录执行 git 命令。
func gitIn(args ...string) (string, error) {
	full := append([]string{"-C", syncDir()}, args...)
	return runCmdLong(context.Background(), "git", full...)
}

// syncPush 把本机 queue + reports 提交并推送到 GitHub。
func syncPush() SyncResult {
	cfg := loadConfig()
	if cfg.GitHubRepo == "" {
		return SyncResult{OK: false, Message: "尚未配置 GitHub 同步仓库"}
	}
	cloneURL, _, err := normalizeRepoURL(cfg.GitHubRepo)
	if err != nil {
		return SyncResult{OK: false, Message: err.Error()}
	}
	setupGitAuth()
	if err := ensureClone(cloneURL); err != nil {
		return SyncResult{OK: false, Message: err.Error()}
	}
	// 先拉取，减少冲突（首次推送/空远端时 pull 失败属正常，忽略）
	if out, err := gitIn("pull", "--ff-only", "--no-rebase"); err != nil {
		low := strings.ToLower(out)
		if !strings.Contains(low, "couldn't find remote ref") &&
			!strings.Contains(low, "no remote ref") &&
			!strings.Contains(low, "no matching refs") {
			return SyncResult{OK: false, Message: "拉取远端失败: " + firstLine(out)}
		}
	}

	// 拷贝 queue.json 与 reports 快照
	if _, err := os.Stat(queuePath()); err == nil {
		if data, err := os.ReadFile(queuePath()); err == nil {
			os.WriteFile(filepath.Join(syncDir(), "queue.json"), data, 0o644)
		}
	}
	if err := copyReportsInto(); err != nil {
		return SyncResult{OK: false, Message: "拷贝报告失败: " + err.Error()}
	}

	// commit + push（专用仓库，统一用 macsync 身份提交）
	host := reportHostname()
	msg := fmt.Sprintf("sync: %s push %s", host, time.Now().Format("2006-01-02 15:04:05"))
	if _, err := gitIn("add", "-A"); err != nil {
		return SyncResult{OK: false, Message: "git add 失败: " + err.Error()}
	}
	if _, err := gitIn("-c", "user.name=macsync", "-c", "user.email=macsync@local", "commit", "-m", msg); err != nil {
		// 无改动时 commit 会失败，属正常，继续 push
	}
	if out, err := gitIn("push"); err != nil {
		return SyncResult{OK: false, Message: "推送失败: " + firstLine(out) + "。请检查 gh auth login 状态"}
	}

	cfg.LastSync = time.Now().Format(time.RFC3339)
	_ = saveConfig(cfg)
	return SyncResult{OK: true, Message: "推送成功: " + msg}
}

// syncPull 拉取远端并合并 queue（并集）与 reports 快照到本机。
func syncPull() SyncResult {
	cfg := loadConfig()
	if cfg.GitHubRepo == "" {
		return SyncResult{OK: false, Message: "尚未配置 GitHub 同步仓库"}
	}
	cloneURL, _, err := normalizeRepoURL(cfg.GitHubRepo)
	if err != nil {
		return SyncResult{OK: false, Message: err.Error()}
	}
	setupGitAuth()
	if err := ensureClone(cloneURL); err != nil {
		return SyncResult{OK: false, Message: err.Error()}
	}
	if out, err := gitIn("pull", "--ff-only", "--no-rebase"); err != nil {
		return SyncResult{OK: false, Message: "拉取失败: " + firstLine(out)}
	}

	// 合并远端 queue（并集，不丢本机已选条目）
	remoteQueue := filepath.Join(syncDir(), "queue.json")
	if _, err := os.Stat(remoteQueue); err == nil {
		mergeQueueFrom(remoteQueue)
	}
	// 回拷 reports 快照
	remoteReports := filepath.Join(syncDir(), "reports")
	if entries, err := os.ReadDir(remoteReports); err == nil {
		dst := filepath.Join(dataDir(), "reports")
		os.MkdirAll(dst, 0o755)
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			if data, err := os.ReadFile(filepath.Join(remoteReports, e.Name())); err == nil {
				os.WriteFile(filepath.Join(dst, e.Name()), data, 0o644)
			}
		}
	}

	cfg.LastSync = time.Now().Format(time.RFC3339)
	_ = saveConfig(cfg)
	return SyncResult{OK: true, Message: "拉取成功，已合并远端队列与各机报告快照"}
}

// mergeQueueFrom 把 remote 队列与本地队列按 (source,name) 并集合并，保存到本地。
func mergeQueueFrom(remotePath string) {
	data, err := os.ReadFile(remotePath)
	if err != nil {
		return
	}
	var remote Queue
	if json.Unmarshal(data, &remote) != nil {
		return
	}
	local := loadQueue()
	for _, it := range remote.Items {
		if !local.contains(it.Source, it.Name) {
			local.Items = append(local.Items, it)
		}
	}
	_ = saveQueue(local)
}
