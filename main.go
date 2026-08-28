package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
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

// isTTY 判断 stdout 是否终端（决定是否用 \r 原地刷新进度）。
func isTTY() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// newProgressPrinter 返回一个 scan 进度回调：
// 终端下逐行原地刷新（"…" → "✓ 完成 (耗时)"），非终端退化为普通行。
func newProgressPrinter() func(scanStep) {
	tty := isTTY()
	var lastLen int
	return func(s scanStep) {
		prefix := fmt.Sprintf("  [%d/%d]", s.step+1, s.total)
		if s.done {
			line := fmt.Sprintf("%s ✓ %s (%v)", prefix, s.name, s.ms.Round(time.Millisecond))
			if tty {
				if lastLen > 0 {
					fmt.Print("\r\033[K")
				}
				fmt.Println(line)
				lastLen = 0
			} else {
				fmt.Println(line)
			}
			return
		}
		if tty {
			line := fmt.Sprintf("%s %s …", prefix, s.name)
			if lastLen > 0 {
				fmt.Print("\r\033[K")
			}
			fmt.Print(line)
			lastLen = len(line)
		}
	}
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
	host, _ := os.Hostname()
	fmt.Printf("正在盘点 %s …\n", host)
	start := time.Now()
	rep, err := runScan(out, newProgressPrinter())
	if err != nil {
		fmt.Fprintln(os.Stderr, "扫描失败:", err)
		os.Exit(1)
	}
	fmt.Printf("\n扫描完成 → %s（总耗时 %v）\n", out, time.Since(start).Round(time.Millisecond))
	fmt.Printf("  机器: %s · %s %s · %s\n", rep.Machine.Hostname, rep.Machine.OSName, rep.Machine.OSVer, rep.Machine.Chip)
	fmt.Printf("  brew: %d formula / %d cask / %d tap\n", len(rep.Brew.Formulae), len(rep.Brew.Casks), len(rep.Brew.Taps))
	fmt.Printf("  uv:   %d 工具\n", len(rep.Uv.Tools))
	fmt.Printf("  GUI 应用: %d · 配置文件: %d\n", len(rep.Apps), len(rep.Configs))
}
