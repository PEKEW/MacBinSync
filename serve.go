package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

//go:embed web
var webFS embed.FS

// ---------- 端口占用检测 ----------

// procInfo 占用端口的进程信息。
type procInfo struct {
	pid     int
	command string
}

// portProcs 通过 lsof 查找监听指定 TCP 端口的进程。
func portProcs(port int) []procInfo {
	lsof := findExec("lsof")
	if lsof == "" {
		lsof = "/usr/sbin/lsof"
	}
	out, err := runCmd(context.Background(), lsof, "-nP", "-iTCP:"+strconv.Itoa(port), "-sTCP:LISTEN")
	if err != nil {
		return nil
	}
	lines := strings.Split(out, "\n")
	var procs []procInfo
	for _, line := range lines[1:] { // 跳过表头
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		if pid, err := strconv.Atoi(f[1]); err == nil {
			// lsof 第一列就是 COMMAND，直接可用；ps 只用来补全完整命令行
			procs = append(procs, procInfo{pid: pid, command: f[0]})
		}
	}
	for i := range procs {
		if out, err := runCmd(context.Background(), "/bin/ps", "-p", strconv.Itoa(procs[i].pid), "-o", "command="); err == nil {
			if cmd := strings.TrimSpace(out); cmd != "" {
				procs[i].command = cmd
			}
		}
	}
	return procs
}

// ensurePortFree 处理端口占用：
//   - 占用的若是旧 macsync 实例 → 自动停止并接管（免去手动找进程）
//   - 其他进程占用 → 返回带进程信息和释放命令的明确错误
func ensurePortFree(port int) error {
	procs := portProcs(port)
	if len(procs) == 0 {
		return nil
	}
	var mine, others []procInfo
	for _, p := range procs {
		if strings.Contains(p.command, "macsync") {
			mine = append(mine, p)
		} else {
			others = append(others, p)
		}
	}
	if len(mine) > 0 {
		pids := make([]string, 0, len(mine))
		for _, p := range mine {
			pids = append(pids, strconv.Itoa(p.pid))
		}
		fmt.Printf("检测到旧的 macsync 实例 (PID %s) 占用端口 %d，正在停止并接管…\n", strings.Join(pids, ", "), port)
		for _, p := range mine {
			_ = syscall.Kill(p.pid, syscall.SIGTERM)
		}
		for i := 0; i < 30; i++ {
			if len(portProcs(port)) == 0 {
				return nil
			}
			time.Sleep(100 * time.Millisecond)
		}
		return errors.New("旧 macsync 实例未能停止，请手动执行: pkill -f \"macsync serve\"")
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("端口 %d 已被其他进程占用:\n", port))
	for _, p := range others {
		b.WriteString(fmt.Sprintf("  PID %d  %s\n", p.pid, p.command))
	}
	b.WriteString("请先停止该进程，或换一个端口启动: macsync serve --port N\n")
	b.WriteString("按端口找进程: lsof -ti:" + strconv.Itoa(port) + " | xargs kill")
	return errors.New(b.String())
}

func cmdServe(args []string) {
	port := 8787
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--port":
			if i+1 < len(args) {
				if p, err := strconv.Atoi(args[i+1]); err == nil {
					port = p
				}
				i++
			}
		}
	}

	// 先解决端口占用，再绑定，最后才打印"已启动"
	addr := "127.0.0.1:" + strconv.Itoa(port)
	if err := ensurePortFree(port); err != nil {
		fmt.Fprintln(os.Stderr, "启动失败:", err)
		os.Exit(1)
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "启动失败:", err)
		// 预检可能因时序漏判，bind 失败时再查一次占用者
		if procs := portProcs(port); len(procs) > 0 {
			fmt.Fprintln(os.Stderr, "占用进程:")
			for _, p := range procs {
				fmt.Fprintf(os.Stderr, "  PID %d  %s\n", p.pid, p.command)
			}
		}
		fmt.Fprintln(os.Stderr, "释放端口: lsof -ti:"+strconv.Itoa(port)+" | xargs kill  或换端口: macsync serve --port N")
		os.Exit(1)
	}
	defer ln.Close()

	// 只读已有报告；绝不自动盘点（盘点耗时，仅在用户显式触发时进行）
	current := currentReportPath()

	sub, _ := fs.Sub(webFS, "web")
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(sub)))
	mux.HandleFunc("/api/report", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if r.Method == http.MethodPost {
			if _, err := runScan(current, nil); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
		}
		data, err := os.ReadFile(current)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "还没有盘点数据：请在页面右上角点「重新盘点」开始首次盘点"})
			return
		}
		w.Write(data)
	})
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"ok": "true", "version": "0.1.0"})
	})

	// 同步队列：GET 查看，POST 加入
	mux.HandleFunc("/api/queue", func(w http.ResponseWriter, r *http.Request) {
		q := loadQueue()
		if r.Method == http.MethodPost {
			var req struct {
				Source string `json:"source"`
				Name   string `json:"name"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求体无效"})
				return
			}
			if req.Source == "" || req.Name == "" {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "source 和 name 不能为空"})
				return
			}
			q.add(req.Source, req.Name)
			_ = saveQueue(q)
		}
		writeJSON(w, http.StatusOK, q)
	})

	// 从队列移除
	mux.HandleFunc("/api/queue/remove", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "仅支持 POST"})
			return
		}
		var req struct {
			Source string `json:"source"`
			Name   string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求体无效"})
			return
		}
		q := loadQueue()
		q.remove(req.Source, req.Name)
		_ = saveQueue(q)
		writeJSON(w, http.StatusOK, q)
	})

	// 卸载工具
	mux.HandleFunc("/api/uninstall", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "仅支持 POST"})
			return
		}
		var req struct {
			Source string `json:"source"`
			Name   string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求体无效"})
			return
		}
		if req.Source == "" || req.Name == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "source 和 name 不能为空"})
			return
		}
		res := runUninstall(req.Source, req.Name)
		writeJSON(w, http.StatusOK, res)
	})

	// 同步配置：GET 查看，POST {github_repo} 保存
	mux.HandleFunc("/api/config", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var req struct {
				GitHubRepo string `json:"github_repo"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求体无效"})
				return
			}
			cfg := loadConfig()
			cfg.GitHubRepo = strings.TrimSpace(req.GitHubRepo)
			if err := saveConfig(cfg); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, http.StatusOK, cfg)
			return
		}
		writeJSON(w, http.StatusOK, loadConfig())
	})

	// 测试 GitHub 连接
	mux.HandleFunc("/api/sync/test", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "仅支持 POST"})
			return
		}
		writeJSON(w, http.StatusOK, testSyncConnection())
	})

	// 同步：POST {direction: "push"|"pull"}
	mux.HandleFunc("/api/sync", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "仅支持 POST"})
			return
		}
		var req struct {
			Direction string `json:"direction"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "请求体无效"})
			return
		}
		var res SyncResult
		switch req.Direction {
		case "push":
			res = syncPush()
		case "pull":
			res = syncPull()
		default:
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "direction 必须是 push 或 pull"})
			return
		}
		writeJSON(w, http.StatusOK, res)
	})

	fmt.Printf("macsync Web 界面已启动 → http://%s\n", addr)
	fmt.Println("按 Ctrl+C 停止。")
	if err := http.Serve(ln, mux); err != nil {
		fmt.Fprintln(os.Stderr, "服务异常退出:", err)
		os.Exit(1)
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
