package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// cmdTimeout 所有外部命令的默认超时。
const cmdTimeout = 20 * time.Second

// findExec 在 PATH 和常见安装前缀中查找可执行文件。
func findExec(names ...string) string {
	known := []string{
		"/opt/homebrew/bin",
		"/usr/local/bin",
		"/usr/bin",
		"/bin",
	}
	if home, err := os.UserHomeDir(); err == nil {
		known = append(known,
			filepath.Join(home, ".local", "bin"),
			filepath.Join(home, ".cargo", "bin"),
		)
	}
	for _, n := range names {
		if p, err := exec.LookPath(n); err == nil {
			return p
		}
		for _, d := range known {
			p := filepath.Join(d, n)
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				return p
			}
		}
	}
	return ""
}

// runCmd 执行外部命令并返回 stdout，带超时。
func runCmd(ctx context.Context, bin string, args ...string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, cmdTimeout)
	defer cancel()
	out, err := exec.CommandContext(cctx, bin, args...).Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// runCmdPath 类似 runCmd，但把 bin 所在目录加入 PATH（npm 等脚本包装器依赖同目录的 node）。
func runCmdPath(ctx context.Context, bin string, args ...string) (string, error) {
	dir := filepath.Dir(bin)
	env := append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"))
	cctx, cancel := context.WithTimeout(ctx, cmdTimeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, bin, args...)
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// ---------- 系统信息 ----------

func collectMachine() Machine {
	m := Machine{}
	if h, err := os.Hostname(); err == nil {
		m.Hostname = h
	}
	m.Home, _ = os.UserHomeDir()
	if out, err := runCmd(context.Background(), "/usr/bin/sw_vers", "-productName"); err == nil {
		m.OSName = strings.TrimSpace(out)
	}
	if out, err := runCmd(context.Background(), "/usr/bin/sw_vers", "-productVersion"); err == nil {
		m.OSVer = strings.TrimSpace(out)
	}
	if out, err := runCmd(context.Background(), "/usr/bin/uname", "-m"); err == nil {
		m.Chip = strings.TrimSpace(out)
	}
	if s := os.Getenv("SHELL"); s != "" {
		m.Shell = s
	} else if out, err := runCmd(context.Background(), "/bin/sh", "-c", "dscl . -read /Users/$(whoami) UserShell 2>/dev/null | awk '{print $2}'"); err == nil {
		m.Shell = strings.TrimSpace(out)
	}
	if p := findExec("brew"); p != "" {
		m.BrewPrefix = filepath.Dir(p)
	}
	return m
}

// ---------- Homebrew ----------

func collectBrew() Brew {
	var b Brew
	brew := findExec("brew")
	if brew == "" {
		return b
	}
	ctx := context.Background()

	if out, err := runCmd(ctx, brew, "list", "--formula", "--versions"); err == nil {
		for _, line := range strings.Split(out, "\n") {
			f := strings.Fields(line)
			if len(f) >= 2 {
				b.Formulae = append(b.Formulae, BrewItem{Name: f[0], Version: strings.Join(f[1:], ", ")})
			}
		}
	}
	if out, err := runCmd(ctx, brew, "list", "--cask", "--versions"); err == nil {
		for _, line := range strings.Split(out, "\n") {
			f := strings.Fields(line)
			if len(f) >= 2 {
				b.Casks = append(b.Casks, BrewItem{Name: f[0], Version: strings.Join(f[1:], ", ")})
			}
		}
	} else if out2, err2 := runCmd(ctx, brew, "list", "--cask"); err2 == nil {
		// --versions 可能因未信任的 tap 失败，退回纯名称列表
		for _, line := range strings.Split(out2, "\n") {
			if name := strings.TrimSpace(line); name != "" {
				b.Casks = append(b.Casks, BrewItem{Name: name})
			}
		}
	}
	if out, err := runCmd(ctx, brew, "tap"); err == nil {
		for _, line := range strings.Split(out, "\n") {
			if t := strings.TrimSpace(line); t != "" {
				b.Taps = append(b.Taps, t)
			}
		}
	}
	// brew leaves：用户主动安装的 formula
	if out, err := runCmd(ctx, brew, "leaves"); err == nil {
		leaves := map[string]bool{}
		for _, line := range strings.Split(out, "\n") {
			if l := strings.TrimSpace(line); l != "" {
				leaves[l] = true
			}
		}
		for i := range b.Formulae {
			if leaves[b.Formulae[i].Name] {
				b.Formulae[i].TopLevel = true
			}
		}
	}
	// 有更新的 formula（尽力而为，失败不影响整体）
	if out, err := runCmd(ctx, brew, "outdated", "--formula", "--json=v2"); err == nil {
		var outdated []struct {
			Name string `json:"name"`
		}
		if json.Unmarshal([]byte(out), &outdated) == nil {
			set := map[string]bool{}
			for _, f := range outdated {
				set[f.Name] = true
			}
			for i := range b.Formulae {
				b.Formulae[i].Outdated = set[b.Formulae[i].Name]
			}
		}
	}

	sort.Slice(b.Formulae, func(i, j int) bool { return b.Formulae[i].Name < b.Formulae[j].Name })
	sort.Slice(b.Casks, func(i, j int) bool { return b.Casks[i].Name < b.Casks[j].Name })
	sort.Strings(b.Taps)
	return b
}

// ---------- uv ----------

var uvToolRe = regexp.MustCompile(`^(\S+)\s+v([\w.\-+]+)\s*$`)

func collectUv() UvTools {
	var u UvTools
	uv := findExec("uv")
	if uv == "" {
		return u
	}
	out, err := runCmd(context.Background(), uv, "tool", "list")
	if err != nil {
		return u
	}
	var cur *UvTool
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if m := uvToolRe.FindStringSubmatch(line); m != nil {
			if cur != nil {
				u.Tools = append(u.Tools, *cur)
			}
			cur = &UvTool{Name: m[1], Version: m[2]}
		} else if cur != nil && strings.HasPrefix(line, "- ") {
			cur.Binaries = append(cur.Binaries, strings.TrimSpace(strings.TrimPrefix(line, "- ")))
		}
	}
	if cur != nil {
		u.Tools = append(u.Tools, *cur)
	}
	return u
}

// ---------- 运行时 ----------

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// findNodeDirs 返回候选 node/npm 所在目录：PATH → nvm 各版本(最新优先) → brew opt。
func findNodeDirs() []string {
	var dirs []string
	if p := findExec("node"); p != "" {
		dirs = append(dirs, filepath.Dir(p))
	}
	if home, err := os.UserHomeDir(); err == nil {
		if versions, err := os.ReadDir(filepath.Join(home, ".nvm", "versions", "node")); err == nil {
			sort.Slice(versions, func(i, j int) bool { return versions[i].Name() < versions[j].Name() })
			for i := len(versions) - 1; i >= 0; i-- {
				dirs = append(dirs, filepath.Join(home, ".nvm", "versions", "node", versions[i].Name(), "bin"))
			}
		}
	}
	dirs = append(dirs, "/opt/homebrew/opt/node/bin", "/usr/local/opt/node/bin")
	return dirs
}

func collectRuntimes() Runtimes {
	var r Runtimes
	ctx := context.Background()

	if rustup := findExec("rustup"); rustup != "" {
		if out, err := runCmd(ctx, rustup, "toolchain", "list"); err == nil {
			for _, line := range strings.Split(out, "\n") {
				if l := strings.TrimSpace(line); l != "" {
					r.Rustup = append(r.Rustup, l)
				}
			}
		}
	}

	// node 可能装在 nvm / brew opt / 标准前缀，逐个候选目录探测
	var nodeBin, npmBin string
	for _, d := range findNodeDirs() {
		if nodeBin == "" && fileExists(filepath.Join(d, "node")) {
			nodeBin = filepath.Join(d, "node")
		}
		if npmBin == "" && fileExists(filepath.Join(d, "npm")) {
			npmBin = filepath.Join(d, "npm")
		}
		if nodeBin != "" && npmBin != "" {
			break
		}
	}

	ni := &NodeInfo{}
	hasNode := false
	if nodeBin != "" {
		if out, err := runCmd(ctx, nodeBin, "--version"); err == nil {
			ni.Version = strings.TrimSpace(out)
			hasNode = true
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		if versions, err := os.ReadDir(filepath.Join(home, ".nvm", "versions", "node")); err == nil {
			for _, v := range versions {
				ni.NvmVersions = append(ni.NvmVersions, v.Name())
				hasNode = true
			}
		}
	}
	if npmBin != "" {
		if out, err := runCmdPath(ctx, npmBin, "ls", "-g", "--depth=0", "--json"); err == nil {
			var parsed struct {
				Dependencies map[string]struct {
					Version string `json:"version"`
				} `json:"dependencies"`
			}
			if json.Unmarshal([]byte(out), &parsed) == nil {
				for name, dep := range parsed.Dependencies {
					ni.GlobalPkgs = append(ni.GlobalPkgs, fmt.Sprintf("%s@%s", name, dep.Version))
					hasNode = true
				}
				sort.Strings(ni.GlobalPkgs)
			}
		}
	}
	if hasNode {
		r.Node = ni
	}
	return r
}

// ---------- Cargo ----------

func collectCargo() Cargo {
	var c Cargo
	home, err := os.UserHomeDir()
	if err != nil {
		return c
	}
	entries, err := os.ReadDir(filepath.Join(home, ".cargo", "bin"))
	if err != nil {
		return c
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		c.Bins = append(c.Bins, e.Name())
	}
	sort.Strings(c.Bins)
	return c
}

// ---------- ~/.local/bin ----------

func collectLocalBin() LocalBin {
	var l LocalBin
	home, err := os.UserHomeDir()
	if err != nil {
		return l
	}
	entries, err := os.ReadDir(filepath.Join(home, ".local", "bin"))
	if err != nil {
		return l
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		l.Bins = append(l.Bins, e.Name())
	}
	sort.Strings(l.Bins)
	return l
}

// ---------- GUI 应用 ----------

func collectApps() []App {
	var apps []App
	entries, err := os.ReadDir("/Applications")
	if err != nil {
		return apps
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".app") {
			continue
		}
		path := filepath.Join("/Applications", e.Name())
		apps = append(apps, App{
			Name:    strings.TrimSuffix(e.Name(), ".app"),
			Path:    path,
			Version: appVersion(path),
		})
	}
	sort.Slice(apps, func(i, j int) bool { return apps[i].Name < apps[j].Name })
	return apps
}

// appVersion 通过 plutil 读取 app 的 CFBundleShortVersionString。
func appVersion(appPath string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/usr/bin/plutil",
		"-extract", "CFBundleShortVersionString", "raw", "-o", "-",
		filepath.Join(appPath, "Contents", "Info.plist")).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// ---------- 配置文件 ----------

// configExcludes HOME 下不作为配置扫描的条目（缓存、大数据目录等）。
var configExcludes = map[string]bool{
	".cache": true, ".DS_Store": true, ".Trash": true, ".local": true,
	".npm": true, ".gradle": true, ".bun": true, ".nvm": true,
	".android": true, ".matplotlib": true, ".modelscope": true,
	".bash_history": true, ".zsh_history": true, ".Xauthority": true,
	".emulator_console_auth_token": true, ".CFUserTextEncoding": true,
	".cups": true, ".bytertc": true, ".go": true, ".rustup": true,
	".cargo": true, ".m2": true,
}

func collectConfigs() []ConfigItem {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	var items []ConfigItem

	if entries, err := os.ReadDir(home); err == nil {
		for _, e := range entries {
			name := e.Name()
			if !strings.HasPrefix(name, ".") || configExcludes[name] {
				continue
			}
			items = append(items, statConfig(filepath.Join(home, name)))
		}
	}
	if entries, err := os.ReadDir(filepath.Join(home, ".config")); err == nil {
		for _, e := range entries {
			items = append(items, statConfig(filepath.Join(home, ".config", e.Name())))
		}
	}

	sort.Slice(items, func(i, j int) bool { return items[i].Path < items[j].Path })
	return items
}

func statConfig(p string) ConfigItem {
	ci := ConfigItem{Path: p}
	st, err := os.Stat(p)
	if err != nil {
		return ci
	}
	ci.ModTime = st.ModTime()
	if st.IsDir() {
		ci.Type = "dir"
		ci.Size = st.Size()
		if _, err := os.Stat(filepath.Join(p, ".git")); err == nil {
			ci.Type = "git-repo"
		}
		ci.FileCount = countFiles(p)
	} else {
		ci.Type = "file"
		ci.Size = st.Size()
		ci.Hash = hashFile(p)
	}
	return ci
}

func countFiles(dir string) int {
	n := 0
	_ = filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			n++
		}
		if n > 100000 {
			return filepath.SkipAll
		}
		return nil
	})
	return n
}

// hashFile 对小于 5MB 的普通文件计算短 sha256（前 8 字节）。
func hashFile(p string) string {
	st, err := os.Stat(p)
	if err != nil || st.Size() > 5<<20 || st.IsDir() {
		return ""
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:8])
}
