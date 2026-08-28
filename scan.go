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

// collectReport 汇总所有采集器，生成一份完整盘点报告。
func collectReport() Report {
	r := Report{
		Machine:     collectMachine(),
		Brew:        collectBrew(),
		Uv:          collectUv(),
		Runtimes:    collectRuntimes(),
		Cargo:       collectCargo(),
		LocalBin:    collectLocalBin(),
		Apps:        collectApps(),
		Configs:     collectConfigs(),
		GeneratedAt: time.Now(),
	}
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

// runScan 执行盘点并写入 out（同时写入 dataDir/reports/<hostname>.json 快照）。
func runScan(out string) (Report, error) {
	rep := collectReport()
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
