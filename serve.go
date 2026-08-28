package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"strconv"
)

//go:embed web
var webFS embed.FS

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

	current := currentReportPath()
	if _, err := os.Stat(current); os.IsNotExist(err) {
		fmt.Println("首次运行，正在盘点本机…")
		if _, err := runScan(current); err != nil {
			fmt.Fprintln(os.Stderr, "盘点失败:", err)
			os.Exit(1)
		}
	}

	sub, _ := fs.Sub(webFS, "web")
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(sub)))
	mux.HandleFunc("/api/report", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if r.Method == http.MethodPost {
			if _, err := runScan(current); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
		}
		data, err := os.ReadFile(current)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "暂无报告，请先 POST /api/scan 盘点"})
			return
		}
		w.Write(data)
	})
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"ok": "true", "version": "0.1.0"})
	})

	addr := "127.0.0.1:" + strconv.Itoa(port)
	fmt.Printf("macsync Web 界面已启动 → http://%s\n", addr)
	fmt.Println("按 Ctrl+C 停止。")
	if err := http.ListenAndServe(addr, mux); err != nil {
		fmt.Fprintln(os.Stderr, "服务启动失败:", err)
		os.Exit(1)
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
