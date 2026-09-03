package main

import (
	"encoding/json"
	"os"
	"strings"
)

// buildStaticHTML 生成自包含的 HTML 快照：内嵌样式、脚本与报告数据，
// 无需启动服务，双击即可在浏览器查看（app.js 检测到 __MACSYNC_REPORT__ 进入离线模式）。
func buildStaticHTML(rep Report) ([]byte, error) {
	html, err := webFS.ReadFile("web/index.html")
	if err != nil {
		return nil, err
	}
	css, err := webFS.ReadFile("web/style.css")
	if err != nil {
		return nil, err
	}
	js, err := webFS.ReadFile("web/app.js")
	if err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return nil, err
	}
	// 防止报告内容里的 </script> 截断内嵌 JSON
	dataJSON := strings.ReplaceAll(string(data), "</", "<\\/")

	out := strings.Replace(string(html),
		`<link rel="stylesheet" href="/style.css">`,
		"<style>\n"+string(css)+"\n</style>", 1)
	out = strings.Replace(out,
		`<script src="/app.js"></script>`,
		"<script>window.__MACSYNC_REPORT__ = "+dataJSON+";</script>\n<script>\n"+string(js)+"\n</script>", 1)
	return []byte(out), nil
}

// exportStaticHTML 把报告导出为单文件 HTML 快照。
func exportStaticHTML(rep Report, path string) error {
	data, err := buildStaticHTML(rep)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
