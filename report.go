package main

import "time"

// Report 是一台机器完整盘点快照的顶层结构。
type Report struct {
	Machine     Machine      `json:"machine"`
	Brew        Brew         `json:"brew"`
	Uv          UvTools      `json:"uv"`
	Runtimes    Runtimes     `json:"runtimes"`
	Cargo       Cargo        `json:"cargo"`
	LocalBin    LocalBin     `json:"local_bin"`
	Apps        []App        `json:"apps"`
	Configs     []ConfigItem `json:"configs"`
	GeneratedAt time.Time    `json:"generated_at"`
}

// Machine 系统与机器基本信息。
type Machine struct {
	Hostname   string `json:"hostname"`
	OSName     string `json:"os_name"`
	OSVer      string `json:"os_version"`
	Chip       string `json:"chip"`
	Home       string `json:"home"`
	Shell      string `json:"shell"`
	BrewPrefix string `json:"brew_prefix,omitempty"`
}

// Brew Homebrew 安装情况。
type Brew struct {
	Formulae []BrewItem `json:"formulae"`
	Casks    []BrewItem `json:"casks"`
	Taps     []string   `json:"taps"`
}

// BrewItem 一个 formula 或 cask。
type BrewItem struct {
	Name     string `json:"name"`
	Version  string `json:"version"`
	TopLevel bool   `json:"top_level"` // 是否为 brew leaves（用户主动安装，非依赖）
	Outdated bool   `json:"outdated"`
}

// UvTools uv 管理的 Python CLI 工具。
type UvTools struct {
	Tools []UvTool `json:"tools"`
}

// UvTool 一个 uv tool。
type UvTool struct {
	Name     string   `json:"name"`
	Version  string   `json:"version"`
	Binaries []string `json:"binaries"`
}

// Runtimes 各语言运行时。
type Runtimes struct {
	Rustup []string  `json:"rustup_toolchains"`
	Node   *NodeInfo `json:"node,omitempty"`
}

// NodeInfo Node 相关。
type NodeInfo struct {
	Version     string   `json:"version"`
	NvmVersions []string `json:"nvm_versions"`
	GlobalPkgs  []string `json:"global_packages"`
}

// Cargo ~/.cargo/bin 下的二进制。
type Cargo struct {
	Bins []string `json:"bins"`
}

// LocalBin ~/.local/bin 下的散装二进制（uv tool、独立安装的 CLI 等）。
type LocalBin struct {
	Bins []string `json:"bins"`
}

// App /Applications 下的 GUI 应用。
type App struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Version string `json:"version"`
}

// ConfigItem 一个配置文件或配置目录。
type ConfigItem struct {
	Path      string    `json:"path"`
	Type      string    `json:"type"` // file | dir | git-repo
	Size      int64     `json:"size"`
	FileCount int       `json:"file_count,omitempty"`
	Hash      string    `json:"hash,omitempty"` // 文件为短 sha256，目录为空
	ModTime   time.Time `json:"mod_time"`
}
