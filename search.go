package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// M2 搜索：数据源与 Homebrew 官方 GUI (BrewUI) 保持一致。
//   - Homebrew formula/cask: formulae.brew.sh JSON API（本地索引 + 24h 缓存）
//   - npm: registry.npmjs.org 搜索 API（实时）
//   - PyPI: pypi.org JSON API（无搜索接口，仅精确包名查询）
const (
	brewFormulaAPI   = "https://formulae.brew.sh/api/formula.json"
	brewCaskAPI      = "https://formulae.brew.sh/api/cask.json"
	formulaPopularAPI = "https://formulae.brew.sh/api/analytics/install-on-request/30d.json"
	caskPopularAPI    = "https://formulae.brew.sh/api/analytics/cask-install/30d.json"
	brewIndexTTL     = 24 * time.Hour
	brewIndexVersion = 3 // 缓存结构版本，变更时旧缓存自动失效
	httpUA           = "macsync/0.1 (+https://github.com/PEKEW/MacBinSync)"
)

// brewEntry 索引中的一条（已裁剪，避免把原始 12MB JSON 全量驻留内存）。
type brewEntry struct {
	Name       string   `json:"name"`
	Display    string   `json:"display,omitempty"` // cask 的友好名，如 Google Chrome
	Aliases    []string `json:"aliases,omitempty"` // 别名，如 neovim 的 nvim、ripgrep 的 rg
	Version    string   `json:"version"`
	Desc       string   `json:"desc"`
	Homepage   string   `json:"homepage"`
	Kind       string   `json:"kind"` // formula | cask
	Popular    int      `json:"popular,omitempty"` // 近 30 天安装量（用于排序）
	Deprecated bool     `json:"deprecated,omitempty"`
}

// SearchResult 返回给前端的一条搜索结果。
type SearchResult struct {
	Name       string `json:"name"`
	Display    string `json:"display,omitempty"`
	Alias      string `json:"alias,omitempty"` // 命中的别名（用于提示）
	Version    string `json:"version"`
	Desc       string `json:"desc"`
	Homepage   string `json:"homepage"`
	Source     string `json:"source"` // brew-formula | brew-cask | npm | pypi
	Installed  bool   `json:"installed"`
	Popular    int    `json:"popular,omitempty"`
	Deprecated bool   `json:"deprecated,omitempty"`
}

type searchIndex struct {
	mu       sync.Mutex
	entries  []brewEntry
	loadedAt time.Time
}

var brewIdx searchIndex

func cacheDir() string {
	d := filepath.Join(dataDir(), "cache")
	os.MkdirAll(d, 0o755)
	return d
}

func httpGetJSON(ctx context.Context, rawURL string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", httpUA)
	client := &http.Client{Timeout: 180 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

// ---------- Homebrew 索引 ----------

// fetchPopularity 拉取 Homebrew 近 30 天安装量（用于搜索排序；失败不影响搜索）。
func fetchPopularity(ctx context.Context, api string) map[string]int {
	var resp struct {
		Items []struct {
			Formula string `json:"formula"`
			Cask    string `json:"cask"`
			Count   string `json:"count"`
		} `json:"items"`
	}
	if err := httpGetJSON(ctx, api, &resp); err != nil {
		return nil
	}
	out := make(map[string]int, len(resp.Items))
	for _, it := range resp.Items {
		name := it.Formula
		if name == "" {
			name = it.Cask
		}
		if name == "" {
			continue
		}
		n, err := strconv.Atoi(strings.ReplaceAll(it.Count, ",", ""))
		if err != nil {
			continue
		}
		out[name] = n
	}
	return out
}

// downloadBrewIndex 从 Homebrew API 拉取 formula + cask（含安装量）并裁剪为精简索引。
func downloadBrewIndex() ([]brewEntry, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	popFormula := fetchPopularity(ctx, formulaPopularAPI)
	popCask := fetchPopularity(ctx, caskPopularAPI)

	var formulae []struct {
		Name       string   `json:"name"`
		Aliases    []string `json:"aliases"`
		Desc       string   `json:"desc"`
		Homepage   string   `json:"homepage"`
		Versions   struct {
			Stable string `json:"stable"`
		} `json:"versions"`
		Deprecated bool `json:"deprecated"`
		Disabled   bool `json:"disabled"`
	}
	if err := httpGetJSON(ctx, brewFormulaAPI, &formulae); err != nil {
		return nil, fmt.Errorf("获取 formula 索引失败: %w", err)
	}
	entries := make([]brewEntry, 0, len(formulae))
	for _, f := range formulae {
		if f.Name == "" || f.Disabled {
			continue
		}
		entries = append(entries, brewEntry{
			Name: f.Name, Aliases: f.Aliases, Version: f.Versions.Stable, Desc: f.Desc,
			Homepage: f.Homepage, Kind: "formula", Deprecated: f.Deprecated,
			Popular: popFormula[f.Name],
		})
	}

	var casks []struct {
		Token      string   `json:"token"`
		Name       []string `json:"name"`
		Desc       string   `json:"desc"`
		Homepage   string   `json:"homepage"`
		Version    string   `json:"version"`
		Deprecated bool     `json:"deprecated"`
	}
	if err := httpGetJSON(ctx, brewCaskAPI, &casks); err != nil {
		return entries, fmt.Errorf("获取 cask 索引失败: %w", err)
	}
	for _, c := range casks {
		if c.Token == "" {
			continue
		}
		display := ""
		if len(c.Name) > 0 {
			display = c.Name[0]
		}
		entries = append(entries, brewEntry{
			Name: c.Token, Display: display, Version: c.Version, Desc: c.Desc,
			Homepage: c.Homepage, Kind: "cask", Deprecated: c.Deprecated,
			Popular: popCask[c.Token],
		})
	}
	return entries, nil
}

// loadBrewIndex 载入 Homebrew 索引：内存 → 本地缓存(24h) → 下载。
func loadBrewIndex(force bool) (time.Time, int, error) {
	brewIdx.mu.Lock()
	defer brewIdx.mu.Unlock()

	cacheFile := filepath.Join(cacheDir(), "brew-index.json")

	if !force {
		if len(brewIdx.entries) > 0 && time.Since(brewIdx.loadedAt) < brewIndexTTL {
			return brewIdx.loadedAt, len(brewIdx.entries), nil
		}
		if data, err := os.ReadFile(cacheFile); err == nil {
			var cached struct {
				Version   int         `json:"version"`
				FetchedAt time.Time   `json:"fetched_at"`
				Entries   []brewEntry `json:"entries"`
			}
			if json.Unmarshal(data, &cached) == nil && cached.Version == brewIndexVersion &&
				len(cached.Entries) > 0 && time.Since(cached.FetchedAt) < brewIndexTTL {
				brewIdx.entries = cached.Entries
				brewIdx.loadedAt = cached.FetchedAt
				return cached.FetchedAt, len(cached.Entries), nil
			}
		}
	}

	entries, err := downloadBrewIndex()
	if err != nil && len(entries) == 0 {
		return time.Time{}, 0, err
	}
	brewIdx.entries = entries
	brewIdx.loadedAt = time.Now()
	payload, _ := json.Marshal(struct {
		Version   int         `json:"version"`
		FetchedAt time.Time   `json:"fetched_at"`
		Entries   []brewEntry `json:"entries"`
	}{brewIndexVersion, brewIdx.loadedAt, entries})
	_ = os.WriteFile(cacheFile, payload, 0o644)
	return brewIdx.loadedAt, len(entries), err
}

// ---------- 已安装集合 ----------

type installedSet struct {
	formulae map[string]bool
	casks    map[string]bool
	uvTools  map[string]bool
	npm      map[string]bool
}

func loadInstalledSet() installedSet {
	s := installedSet{
		formulae: map[string]bool{}, casks: map[string]bool{},
		uvTools: map[string]bool{}, npm: map[string]bool{},
	}
	rep, err := readCurrentReport()
	if err != nil {
		return s
	}
	for _, f := range rep.Brew.Formulae {
		s.formulae[f.Name] = true
	}
	for _, c := range rep.Brew.Casks {
		s.casks[c.Name] = true
	}
	for _, t := range rep.Uv.Tools {
		s.uvTools[strings.ToLower(t.Name)] = true
	}
	if rep.Runtimes.Node != nil {
		for _, g := range rep.Runtimes.Node.GlobalPkgs {
			name := g
			if i := strings.LastIndex(g, "@"); i > 0 {
				name = g[:i]
			}
			s.npm[strings.ToLower(name)] = true
		}
	}
	return s
}

// ---------- 各源搜索 ----------

// wordPrefixMatch 判断 s 中是否存在以 q 开头的词（按非字母数字分词），
// 用于把 "Google Chrome" 这类显示名匹配提升到名称包含之前。
func wordPrefixMatch(s, q string) bool {
	if s == "" || q == "" {
		return false
	}
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
	}) {
		if strings.HasPrefix(w, q) {
			return true
		}
	}
	return false
}

// searchBrew 在本地索引中检索：名称精确 > 前缀 > 名称包含 > 别名/描述包含。
func searchBrew(q string, limit int) []SearchResult {
	brewIdx.mu.Lock()
	entries := brewIdx.entries
	brewIdx.mu.Unlock()
	if len(entries) == 0 {
		return nil
	}
	ql := strings.ToLower(strings.TrimSpace(q))
	inst := loadInstalledSet()

	type scored struct {
		e     brewEntry
		score int
		alias string // 命中的别名
	}
	hits := make([]scored, 0, 64)
	for _, e := range entries {
		name := strings.ToLower(e.Name)
		displayLower := strings.ToLower(e.Display)
		var score int
		var matchedAlias string
		aliasExact, aliasPrefix, aliasHit := false, false, ""
		for _, a := range e.Aliases {
			al := strings.ToLower(a)
			if al == ql {
				aliasExact, aliasHit = true, a
			} else if strings.HasPrefix(al, ql) {
				aliasPrefix = true
				if aliasHit == "" {
					aliasHit = a
				}
			}
		}
		switch {
		case name == ql || aliasExact:
			// 名称或别名完全匹配（如搜 nvim → neovim、rg → ripgrep）
			score = 0
			if aliasExact {
				matchedAlias = aliasHit
			}
		case strings.HasPrefix(name, ql) || wordPrefixMatch(displayLower, ql):
			// 名称前缀，或显示名中某个词以查询开头（如 "Google Chrome" 搜 "chrome"）
			score = 1
		case aliasPrefix:
			score = 2
			matchedAlias = aliasHit
		case wordPrefixMatch(name, ql):
			score = 3
		case strings.Contains(name, ql):
			score = 4
		case strings.Contains(displayLower, ql):
			score = 5
		case strings.Contains(strings.ToLower(e.Desc), ql):
			score = 6
		default:
			continue
		}
		hits = append(hits, scored{e, score, matchedAlias})
	}
	// 先按相关度分层，同层再按 30 天安装量，最后按名称
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].score != hits[j].score {
			return hits[i].score < hits[j].score
		}
		if hits[i].e.Popular != hits[j].e.Popular {
			return hits[i].e.Popular > hits[j].e.Popular
		}
		return hits[i].e.Name < hits[j].e.Name
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}

	results := make([]SearchResult, 0, len(hits))
	for _, h := range hits {
		e := h.e
		source, isInstalled := "brew-formula", inst.formulae[e.Name]
		if e.Kind == "cask" {
			source, isInstalled = "brew-cask", inst.casks[e.Name]
		}
		results = append(results, SearchResult{
			Name: e.Name, Display: e.Display, Alias: h.alias, Version: e.Version, Desc: e.Desc,
			Homepage: e.Homepage, Source: source, Installed: isInstalled,
			Popular: e.Popular, Deprecated: e.Deprecated,
		})
	}
	return results
}

// searchNpm 使用 npm registry 搜索 API（实时）。
func searchNpm(ctx context.Context, q string, limit int) ([]SearchResult, error) {
	var resp struct {
		Objects []struct {
			Package struct {
				Name        string `json:"name"`
				Version     string `json:"version"`
				Description string `json:"description"`
				Links       struct {
					NPM      string `json:"npm"`
					Homepage string `json:"homepage"`
				} `json:"links"`
			} `json:"package"`
		} `json:"objects"`
	}
	api := "https://registry.npmjs.org/-/v1/search?text=" + url.QueryEscape(q) + "&size=" + strconv.Itoa(limit)
	if err := httpGetJSON(ctx, api, &resp); err != nil {
		return nil, err
	}
	inst := loadInstalledSet()
	out := make([]SearchResult, 0, len(resp.Objects))
	for _, o := range resp.Objects {
		home := o.Package.Links.Homepage
		if home == "" {
			home = o.Package.Links.NPM
		}
		out = append(out, SearchResult{
			Name: o.Package.Name, Version: o.Package.Version, Desc: o.Package.Description,
			Homepage: home, Source: "npm", Installed: inst.npm[strings.ToLower(o.Package.Name)],
		})
	}
	return out, nil
}

// lookupPyPI PyPI 无搜索 API，仅支持精确包名查询。
func lookupPyPI(ctx context.Context, name string) (*SearchResult, error) {
	var resp struct {
		Info struct {
			Name       string `json:"name"`
			Version    string `json:"version"`
			Summary    string `json:"summary"`
			ProjectURL string `json:"project_url"`
			HomePage   string `json:"home_page"`
		} `json:"info"`
	}
	api := "https://pypi.org/pypi/" + url.PathEscape(name) + "/json"
	if err := httpGetJSON(ctx, api, &resp); err != nil {
		return nil, err
	}
	home := resp.Info.ProjectURL
	if home == "" {
		home = resp.Info.HomePage
	}
	inst := loadInstalledSet()
	return &SearchResult{
		Name: resp.Info.Name, Version: resp.Info.Version, Desc: resp.Info.Summary,
		Homepage: home, Source: "pypi", Installed: inst.uvTools[strings.ToLower(resp.Info.Name)],
	}, nil
}

// searchAllSources 汇总各源结果 + 说明信息。
func searchAllSources(q string, limit int, refresh bool) ([]SearchResult, []string, time.Time, int) {
	notes := []string{}

	loadedAt, count, err := loadBrewIndex(refresh)
	if err != nil {
		notes = append(notes, err.Error())
	}
	results := searchBrew(q, limit)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if npmRes, err := searchNpm(ctx, q, 15); err == nil {
		results = append(results, npmRes...)
	} else {
		notes = append(notes, "npm 搜索失败: "+err.Error())
	}

	// PyPI 只支持精确包名（无搜索接口），带空格的查询跳过
	if !strings.ContainsAny(q, " \t") {
		if p, err := lookupPyPI(ctx, q); err == nil {
			results = append(results, *p)
		} else if !strings.Contains(err.Error(), "HTTP 404") {
			notes = append(notes, "PyPI 查询失败: "+err.Error())
		}
	}
	return results, notes, loadedAt, count
}

// ---------- 安装动作 ----------

// installable 支持一键安装的来源。
var installable = map[string]bool{
	"brew-formula": true,
	"brew-cask":    true,
	"npm":          true,
	"pypi":         true,
}

func runInstall(source, name string) ActionResult {
	switch source {
	case "brew-formula":
		return runAction("brew", "install", name)
	case "brew-cask":
		return runAction("brew", "install", "--cask", name)
	case "npm":
		bin := findNpmBin()
		if bin == "" {
			return ActionResult{OK: false, Error: "未找到 npm"}
		}
		out, err := runCmdLong(context.Background(), bin, "install", "-g", name)
		if err != nil {
			return ActionResult{OK: false, Output: out, Error: err.Error()}
		}
		return ActionResult{OK: true, Output: out}
	case "pypi":
		return runAction("uv", "tool", "install", name)
	default:
		return ActionResult{OK: false, Error: fmt.Sprintf("不支持的来源: %s", source)}
	}
}
