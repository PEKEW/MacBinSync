package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// ---------- 同步队列 ----------

// QueueItem 队列中的一条：某台机器上的某个工具/配置，等待同步到其他机器。
type QueueItem struct {
	Source  string    `json:"source"`
	Name    string    `json:"name"`
	AddedAt time.Time `json:"added_at"`
}

// Queue 同步队列的持久化结构，存储于 ~/.macsync/queue.json。
type Queue struct {
	Items []QueueItem `json:"items"`
}

func queuePath() string {
	return filepath.Join(dataDir(), "queue.json")
}

func loadQueue() Queue {
	var q Queue
	data, err := os.ReadFile(queuePath())
	if err != nil {
		q.Items = []QueueItem{}
		return q
	}
	_ = json.Unmarshal(data, &q)
	if q.Items == nil {
		q.Items = []QueueItem{}
	}
	return q
}

func saveQueue(q Queue) error {
	if q.Items == nil {
		q.Items = []QueueItem{}
	}
	data, err := json.MarshalIndent(q, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(queuePath(), data, 0o644)
}

func (q *Queue) contains(source, name string) bool {
	for _, it := range q.Items {
		if it.Source == source && it.Name == name {
			return true
		}
	}
	return false
}

func (q *Queue) add(source, name string) {
	if q.contains(source, name) {
		return
	}
	q.Items = append(q.Items, QueueItem{Source: source, Name: name, AddedAt: time.Now()})
}

func (q *Queue) remove(source, name string) {
	out := q.Items[:0]
	for _, it := range q.Items {
		if it.Source != source || it.Name != name {
			out = append(out, it)
		}
	}
	q.Items = out
}

// ---------- 卸载动作 ----------

type ActionResult struct {
	OK     bool   `json:"ok"`
	Output string `json:"output"`
	Error  string `json:"error,omitempty"`
}

// uninstallable 支持真正卸载动作的来源。
var uninstallable = map[string]bool{
	"brew-formula": true,
	"brew-cask":    true,
	"uv":           true,
	"npm":          true,
	"app":          true,
}

// actionTimeout 卸载类命令可能较慢（brew 卸载有清理步骤）。
const actionTimeout = 180 * time.Second

func runCmdLong(ctx context.Context, bin string, args ...string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, actionTimeout)
	defer cancel()
	out, err := exec.CommandContext(cctx, bin, args...).CombinedOutput()
	return string(out), err
}

func runAction(binName string, args ...string) ActionResult {
	bin := findExec(binName)
	if bin == "" {
		return ActionResult{OK: false, Error: "未找到 " + binName}
	}
	out, err := runCmdLong(context.Background(), bin, args...)
	if err != nil {
		return ActionResult{OK: false, Output: out, Error: err.Error()}
	}
	return ActionResult{OK: true, Output: out}
}

func findNpmBin() string {
	for _, d := range findNodeDirs() {
		if p := filepath.Join(d, "npm"); fileExists(p) {
			return p
		}
	}
	return ""
}

// runUninstall 根据来源执行真正的卸载。
func runUninstall(source, name string) ActionResult {
	switch source {
	case "brew-formula":
		return runAction("brew", "uninstall", name)
	case "brew-cask":
		return runAction("brew", "uninstall", "--cask", name)
	case "uv":
		return runAction("uv", "tool", "uninstall", name)
	case "npm":
		bin := findNpmBin()
		if bin == "" {
			return ActionResult{OK: false, Error: "未找到 npm"}
		}
		out, err := runCmdLong(context.Background(), bin, "uninstall", "-g", name)
		if err != nil {
			return ActionResult{OK: false, Output: out, Error: err.Error()}
		}
		return ActionResult{OK: true, Output: out}
	case "app":
		return trashApp(name)
	default:
		return ActionResult{OK: false, Error: fmt.Sprintf("不支持的来源: %s", source)}
	}
}

// trashApp 把 /Applications 下的应用移动到废纸篓（不物理删除）。
func trashApp(name string) ActionResult {
	home, _ := os.UserHomeDir()
	src := filepath.Join("/Applications", name+".app")
	if _, err := os.Stat(src); err != nil {
		return ActionResult{OK: false, Error: "应用不存在: " + src}
	}
	dst := filepath.Join(home, ".Trash", name+".app")
	if _, err := os.Stat(dst); err == nil {
		dst = filepath.Join(home, ".Trash", name+"-"+time.Now().Format("20060102-150405")+".app")
	}
	if err := os.Rename(src, dst); err != nil {
		return ActionResult{OK: false, Error: err.Error()}
	}
	return ActionResult{OK: true, Output: "已移动到废纸篓: " + dst}
}
