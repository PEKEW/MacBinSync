package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// emptyIfNil 保证切片序列化为 [] 而非 null。
func emptyIfNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// scanStep 一次进度通知：某采集步骤开始(done=false)或完成(done=true)。
type scanStep struct {
	step  int           // 从 0 开始
	total int           // 总步骤数
	name  string        // 步骤名
	done  bool          // 是否已完成
	ms    time.Duration // 本步骤耗时（done=true 时有效）
}

// collectReport 汇总所有采集器，生成一份完整盘点报告。
// progress 可为 nil（如服务端后台盘点时不输出）。
func collectReport(progress func(scanStep)) Report {
	r := Report{}
	steps := []struct {
		name string
		fn   func()
	}{
		{"系统信息", func() { r.Machine = collectMachine() }},
		{"Homebrew", func() { r.Brew = collectBrew() }},
		{"uv 工具", func() { r.Uv = collectUv() }},
		{"运行时", func() { r.Runtimes = collectRuntimes() }},
		{"Cargo 二进制", func() { r.Cargo = collectCargo() }},
		{"~/.local/bin", func() { r.LocalBin = collectLocalBin() }},
		{"GUI 应用", func() { r.Apps = collectApps() }},
		{"配置文件", func() { r.Configs = collectConfigs() }},
	}
	for i, s := range steps {
		if progress != nil {
			progress(scanStep{step: i, total: len(steps), name: s.name})
		}
		start := time.Now()
		s.fn()
		if progress != nil {
			progress(scanStep{step: i, total: len(steps), name: s.name, done: true, ms: time.Since(start)})
		}
	}
	r.GeneratedAt = time.Now()
	r.Brew.Formulae = emptyIfNil(r.Brew.Formulae)
	r.Brew.Casks = emptyIfNil(r.Brew.Casks)
	r.Brew.Taps = emptyIfNil(r.Brew.Taps)
	r.Uv.Tools = emptyIfNil(r.Uv.Tools)
	r.Runtimes.Rustup = emptyIfNil(r.Runtimes.Rustup)
	r.Cargo.Bins = emptyIfNil(r.Cargo.Bins)
	r.LocalBin.Bins = emptyIfNil(r.LocalBin.Bins)
	r.Apps = emptyIfNil(r.Apps)
	r.Configs = emptyIfNil(r.Configs)
	if r.Runtimes.Node != nil {
		r.Runtimes.Node.NvmVersions = emptyIfNil(r.Runtimes.Node.NvmVersions)
		r.Runtimes.Node.GlobalPkgs = emptyIfNil(r.Runtimes.Node.GlobalPkgs)
	}
	return r
}

// readCurrentReport 读取当前盘点到内存（不存在时返回错误）。
func readCurrentReport() (Report, error) {
	data, err := os.ReadFile(currentReportPath())
	if err != nil {
		return Report{}, err
	}
	var rep Report
	if err := json.Unmarshal(data, &rep); err != nil {
		return Report{}, err
	}
	return rep, nil
}

// runScan 执行盘点并写入 out（同时写入 dataDir/reports/<hostname>.json 快照）。
// progress 透传给 collectReport。
func runScan(out string, progress func(scanStep)) (Report, error) {
	rep := collectReport(progress)
	data, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return rep, err
	}
	if dir := filepath.Dir(out); dir != "" {
		os.MkdirAll(dir, 0o755)
	}
	if err := os.WriteFile(out, data, 0o644); err != nil {
		return rep, err
	}
	host := rep.Machine.Hostname
	if host == "" {
		host = "unknown"
	}
	reportsDir := filepath.Join(dataDir(), "reports")
	os.MkdirAll(reportsDir, 0o755)
	if err := os.WriteFile(filepath.Join(reportsDir, host+".json"), data, 0o644); err != nil {
		return rep, err
	}
	return rep, nil
}
