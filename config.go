package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Config 全局配置，存储于 ~/.macsync/config.json。
type Config struct {
	GitHubRepo string `json:"github_repo"` // 同步专用仓库，如 peke/macsync-sync
	LastSync   string `json:"last_sync"`   // 上次成功同步时间 (RFC3339)
}

func configPath() string {
	return filepath.Join(dataDir(), "config.json")
}

func loadConfig() Config {
	var c Config
	data, err := os.ReadFile(configPath())
	if err != nil {
		return c
	}
	_ = json.Unmarshal(data, &c)
	return c
}

func saveConfig(c Config) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath(), data, 0o644)
}
