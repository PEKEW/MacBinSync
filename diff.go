package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// 多机对比：读取 ~/.macsync/reports/ 下各机快照，算出"A 有 B 没有"，
// 并判断每一项能否自动安装（GUI 应用会尝试匹配同名 brew cask）。

type MachineInfo struct {
	Hostname    string    `json:"hostname"`
	OS          string    `json:"os"`
	Chip        string    `json:"chip"`
	GeneratedAt time.Time `json:"generated_at"`
	Formulae    int       `json:"formulae"`
	Casks       int       `json:"casks"`
	UvTools     int       `json:"uv_tools"`
	NpmGlobals  int       `json:"npm_globals"`
	Apps        int       `json:"apps"`
	Configs     int       `json:"configs"`
	Current     bool      `json:"current"`
}

type DiffItem struct {
	Source      string `json:"source"` // brew-formula | brew-cask | uv | npm | app | config
	Name        string `json:"name"`
	Version     string `json:"version"`
	Display     string `json:"display,omitempty"`
	Installable bool   `json:"installable"`
	Hint        string `json:"hint,omitempty"`
}

type DiffResult struct {
	Base         MachineInfo `json:"base"`
	Target       MachineInfo `json:"target"`
	OnlyInBase   []DiffItem  `json:"only_in_base"`   // 参照有、目标没有（需要装/入队）
	OnlyInTarget []DiffItem  `json:"only_in_target"` // 目标有、参照没有（反向同步）
}

func machineFromReport(rep Report, current bool) MachineInfo {
	mi := MachineInfo{
		Hostname: rep.Machine.Hostname, OS: strings.TrimSpace(rep.Machine.OSName + " " + rep.Machine.OSVer),
		Chip: rep.Machine.Chip, GeneratedAt: rep.GeneratedAt,
		Formulae: len(rep.Brew.Formulae), Casks: len(rep.Brew.Casks),
		UvTools: len(rep.Uv.Tools), Apps: len(rep.Apps), Configs: len(rep.Configs),
		Current: current,
	}
	if rep.Runtimes.Node != nil {
		mi.NpmGlobals = len(rep.Runtimes.Node.GlobalPkgs)
	}
	return mi
}

// listMachines 汇总本机 + 所有已同步过来的机器快照。
func listMachines() []MachineInfo {
	var out []MachineInfo
	seen := map[string]bool{}

	if rep, err := readCurrentReport(); err == nil && rep.Machine.Hostname != "" {
		out = append(out, machineFromReport(rep, true))
		seen[rep.Machine.Hostname] = true
	}
	entries, err := os.ReadDir(filepath.Join(dataDir(), "reports"))
	if err != nil {
		return out
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		host := strings.TrimSuffix(e.Name(), ".json")
		if seen[host] {
			continue
		}
		rep, err := readReportFile(filepath.Join(dataDir(), "reports", e.Name()))
		if err != nil {
			continue
		}
		out = append(out, machineFromReport(rep, false))
		seen[host] = true
	}
	return out
}

func readReportFile(path string) (Report, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Report{}, err
	}
	var rep Report
	if err := json.Unmarshal(data, &rep); err != nil {
		return Report{}, err
	}
	return rep, nil
}

// loadReportByHost 按主机名加载快照（空字符串或 "current" 表示本机）。
func loadReportByHost(host string) (Report, error) {
	if host == "" || host == "current" {
		return readCurrentReport()
	}
	path := filepath.Join(dataDir(), "reports", host+".json")
	if _, err := os.Stat(path); err != nil {
		return Report{}, fmt.Errorf("找不到主机快照: %s", host)
	}
	return readReportFile(path)
}

// normalizeName 用于跨源名称匹配：小写并去掉非字母数字。
func normalizeName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// caskForApp 尝试为 GUI 应用找到同名 brew cask（如 OmniWM → omniwm）。
func caskForApp(appName string) (token, display, version string, ok bool) {
	brewIdx.mu.Lock()
	entries := brewIdx.entries
	brewIdx.mu.Unlock()
	if len(entries) == 0 {
		// 索引未加载则尽力加载一次（离线/失败时静默跳过）
		if _, _, err := loadBrewIndex(false); err != nil {
			return "", "", "", false
		}
		brewIdx.mu.Lock()
		entries = brewIdx.entries
		brewIdx.mu.Unlock()
	}
	target := normalizeName(appName)
	if target == "" {
		return "", "", "", false
	}
	// 先精确匹配 display，再精确匹配 token，最后做"去掉版本号后缀"的宽松匹配
	for _, e := range entries {
		if e.Kind != "cask" {
			continue
		}
		if normalizeName(e.Display) == target || normalizeName(e.Name) == target {
			return e.Name, e.Display, e.Version, true
		}
	}
	for _, e := range entries {
		if e.Kind != "cask" {
			continue
		}
		if n := normalizeName(e.Display); n != "" && strings.HasPrefix(target, n) && len(n) >= 4 {
			return e.Name, e.Display, e.Version, true
		}
	}
	return "", "", "", false
}

// reportItems 把报告摊平成 分类 → 名称 → 条目。
func reportItems(rep Report) map[string]map[string]DiffItem {
	out := map[string]map[string]DiffItem{}
	add := func(it DiffItem) {
		m, ok := out[it.Source]
		if !ok {
			m = map[string]DiffItem{}
			out[it.Source] = m
		}
		m[it.Name] = it
	}
	for _, f := range rep.Brew.Formulae {
		if f.TopLevel { // 只对比用户主动安装的，忽略依赖
			add(DiffItem{Source: "brew-formula", Name: f.Name, Version: f.Version, Installable: true})
		}
	}
	for _, c := range rep.Brew.Casks {
		add(DiffItem{Source: "brew-cask", Name: c.Name, Version: c.Version, Installable: true})
	}
	for _, t := range rep.Uv.Tools {
		add(DiffItem{Source: "uv", Name: t.Name, Version: t.Version, Installable: true})
	}
	if rep.Runtimes.Node != nil {
		for _, g := range rep.Runtimes.Node.GlobalPkgs {
			name, ver := g, ""
			if i := strings.LastIndex(g, "@"); i > 0 {
				name, ver = g[:i], g[i+1:]
			}
			add(DiffItem{Source: "npm", Name: name, Version: ver, Installable: true})
		}
	}
	for _, a := range rep.Apps {
		// GUI 应用：能匹配到同名 cask 就转为可安装的 brew-cask 条目
		if token, display, ver, ok := caskForApp(a.Name); ok {
			if ver == "" {
				ver = a.Version
			}
			add(DiffItem{Source: "brew-cask", Name: token, Version: ver, Display: display,
				Installable: true, Hint: "来自 GUI 应用 " + a.Name})
			continue
		}
		add(DiffItem{Source: "app", Name: a.Name, Version: a.Version, Installable: false,
			Hint: "Homebrew 无对应 cask，需手动安装或从另一台拷贝"})
	}
	for _, c := range rep.Configs {
		add(DiffItem{Source: "config", Name: c.Path, Version: "", Installable: false, Hint: "配置文件同步见 M4"})
	}
	return out
}

// computeDiff 计算 base（参照机）与 target（目标机）的差异。
func computeDiff(baseHost, targetHost string) (DiffResult, error) {
	baseRep, err := loadReportByHost(baseHost)
	if err != nil {
		return DiffResult{}, err
	}
	targetRep, err := loadReportByHost(targetHost)
	if err != nil {
		return DiffResult{}, err
	}
	baseItems := reportItems(baseRep)
	targetItems := reportItems(targetRep)

	var onlyBase, onlyTarget []DiffItem
	for cat, m := range baseItems {
		for name, it := range m {
			if _, exists := targetItems[cat][name]; !exists {
				onlyBase = append(onlyBase, it)
			}
		}
	}
	for cat, m := range targetItems {
		for name, it := range m {
			if _, exists := baseItems[cat][name]; !exists {
				onlyTarget = append(onlyTarget, it)
			}
		}
	}
	sortItems(onlyBase)
	sortItems(onlyTarget)

	return DiffResult{
		Base:         machineFromReport(baseRep, baseRep.Machine.Hostname == currentHostname()),
		Target:       machineFromReport(targetRep, targetRep.Machine.Hostname == currentHostname()),
		OnlyInBase:   emptyIfNil(onlyBase),
		OnlyInTarget: emptyIfNil(onlyTarget),
	}, nil
}

func currentHostname() string {
	if rep, err := readCurrentReport(); err == nil {
		return rep.Machine.Hostname
	}
	h, _ := os.Hostname()
	return h
}

// sortItems 按分类分组排序：可安装的在前，然后按分类与名称。
func sortItems(items []DiffItem) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Installable != items[j].Installable {
			return items[i].Installable
		}
		if items[i].Source != items[j].Source {
			return items[i].Source < items[j].Source
		}
		return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name)
	})
}

// ---------- 批量应用到本机（后台任务 + 轮询进度） ----------

type ApplyItemResult struct {
	Source  string `json:"source"`
	Name    string `json:"name"`
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

type applyState struct {
	mu         sync.Mutex
	running    bool
	total      int
	index      int
	current    string
	results    []ApplyItemResult
	finishedAt time.Time
}

var applyJob applyState

// startApply 后台顺序安装队列中的项目（同一时间只允许一个任务）。
func startApply(items []QueueItem) bool {
	applyJob.mu.Lock()
	if applyJob.running {
		applyJob.mu.Unlock()
		return false
	}
	applyJob.running = true
	applyJob.total = len(items)
	applyJob.index = 0
	applyJob.current = ""
	applyJob.results = []ApplyItemResult{}
	applyJob.finishedAt = time.Time{}
	applyJob.mu.Unlock()

	go func() {
		for i, it := range items {
			applyJob.mu.Lock()
			applyJob.index = i
			applyJob.current = it.Name
			applyJob.mu.Unlock()

			res := runInstall(it.Source, it.Name)
			msg := "安装成功"
			if !res.OK {
				msg = firstLine(res.Error + " " + res.Output)
				if msg == "" {
					msg = "安装失败"
				}
			}
			applyJob.mu.Lock()
			applyJob.results = append(applyJob.results, ApplyItemResult{it.Source, it.Name, res.OK, msg})
			applyJob.mu.Unlock()
		}
		applyJob.mu.Lock()
		applyJob.running = false
		applyJob.index = len(items)
		applyJob.current = ""
		applyJob.finishedAt = time.Now()
		applyJob.mu.Unlock()

		// 安装完成后重新盘点，刷新已安装状态
		_, _ = runScan(currentReportPath(), nil)
	}()
	return true
}

func applyStatus() map[string]any {
	applyJob.mu.Lock()
	defer applyJob.mu.Unlock()
	return map[string]any{
		"running":     applyJob.running,
		"total":       applyJob.total,
		"index":       applyJob.index,
		"current":     applyJob.current,
		"results":     emptyIfNil(applyJob.results),
		"finished_at": applyJob.finishedAt,
	}
}
