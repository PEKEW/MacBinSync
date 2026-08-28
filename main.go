package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}
	switch os.Args[1] {
	case "scan":
		cmdScan(os.Args[2:])
	case "serve":
		cmdServe(os.Args[2:])
	case "version", "--version", "-v":
		fmt.Println("macsync 0.1.0")
	default:
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Println(`macsync — 本机工具/配置盘点 + Web 可视化

用法:
  macsync scan [--out PATH]   盘点本机并输出 JSON 报告
                              (默认 ~/.macsync/current.json)
  macsync serve [--port N]    启动 Web 界面 (默认端口 8787)
  macsync version             显示版本`)
}

// dataDir 数据目录 ~/.macsync。
func dataDir() string {
	home, _ := os.UserHomeDir()
	d := filepath.Join(home, ".macsync")
	os.MkdirAll(d, 0o755)
	return d
}

func currentReportPath() string {
	return filepath.Join(dataDir(), "current.json")
}

func cmdScan(args []string) {
	out := currentReportPath()
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--out":
			if i+1 < len(args) {
				out = args[i+1]
				i++
			}
		}
	}
	rep, err := runScan(out)
	if err != nil {
		fmt.Fprintln(os.Stderr, "扫描失败:", err)
		os.Exit(1)
	}
	fmt.Printf("扫描完成 → %s\n", out)
	fmt.Printf("  机器: %s · %s %s · %s\n", rep.Machine.Hostname, rep.Machine.OSName, rep.Machine.OSVer, rep.Machine.Chip)
	fmt.Printf("  brew: %d formula / %d cask / %d tap\n", len(rep.Brew.Formulae), len(rep.Brew.Casks), len(rep.Brew.Taps))
	fmt.Printf("  uv:   %d 工具\n", len(rep.Uv.Tools))
	fmt.Printf("  GUI 应用: %d · 配置文件: %d\n", len(rep.Apps), len(rep.Configs))
}
