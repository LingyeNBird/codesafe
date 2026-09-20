/*
 * Copyright (C) 2026 codesafe contributors
 * SPDX-License-Identifier: AGPL-3.0-or-later
 * See COPYING in the project root for the full license.
 */
// config 包管理 codesafe 的用户配置：API key、输出语言、commit scope 选项。
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var ErrNoAPIKey = errors.New("api key not configured")

// Config 是持久化到磁盘的配置。
type Config struct {
	APIKey    string   `json:"api_key"`
	Lang      string   `json:"lang"`                 // "en"（默认）或 "zh"
	Scopes    []string `json:"scopes,omitempty"`     // 用户级自定义 scope 名列表（--config 持久化）
	AllowNone *bool    `json:"allow_none,omitempty"` // 是否允许 scope=none；nil/false→不允许
	// Screened 缓存按项目根路径筛出的形态域 scope 名；目录结构变化时重筛。
	Screened map[string]ScreenedEntry `json:"screened,omitempty"`
}

// ScreenedEntry 是一个项目的筛选缓存：scope 名列表 + 生成时的目录指纹。
type ScreenedEntry struct {
	Scopes  []string `json:"scopes"`   // 筛出的形态域 scope 名
	DirHash string   `json:"dir_hash"` // 目录指纹，变了就重筛
}

// Path 返回配置文件路径：<os.UserConfigDir>/codesafe/config.json。
func Path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("cannot locate user config dir: %w", err)
	}
	return filepath.Join(dir, "codesafe", "config.json"), nil
}

// Load 读取配置文件；文件不存在或缺 key 时返回 ErrNoAPIKey。Lang 缺省为 "en"。
func Load() (Config, error) {
	p, err := Path()
	if err != nil {
		return Config{}, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return Config{Lang: "en"}, ErrNoAPIKey
		}
		return Config{}, fmt.Errorf("cannot read config: %w", err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("config file corrupt: %w", err)
	}
	if cfg.Lang == "" {
		cfg.Lang = "en"
	}
	if cfg.APIKey == "" {
		return cfg, ErrNoAPIKey // 返回已读的 Lang，调用方按需取用
	}
	return cfg, nil
}

// Save 把配置写回磁盘，目录不存在时自动创建，文件权限 0600。
func Save(cfg Config) error {
	p, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return fmt.Errorf("cannot create config dir: %w", err)
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	if err := os.WriteFile(p, data, 0o600); err != nil {
		return fmt.Errorf("cannot write config: %w", err)
	}
	return nil
}

// Set 解析 "key=value" 并写入配置；支持 api_key、lang、scopes。返回更新后的配置。
func Set(kv string) (Config, error) {
	cfg, _ := Load() // 读旧值以便局部更新；文件不存在也无妨
	cfg, err := Apply(cfg, kv)
	if err != nil {
		return Config{}, err
	}
	if err := Save(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Apply 同 Set 的解析逻辑，但只改内存值不落盘。供 --set 临时覆盖用。
func Apply(cfg Config, kv string) (Config, error) {
	key, value, ok := strings.Cut(kv, "=")
	if !ok {
		return Config{}, fmt.Errorf("expected key=value, got %q", kv)
	}
	switch key {
	case "api_key":
		if value == "" {
			return Config{}, errors.New("api_key cannot be empty")
		}
		cfg.APIKey = value
	case "lang":
		if value != "en" && value != "zh" {
			return Config{}, fmt.Errorf("lang must be en or zh, got %q", value)
		}
		cfg.Lang = value
	case "scopes":
		// | 分隔的 scope 名列表，覆盖内置集；空值 = 用内置
		if value == "" {
			cfg.Scopes = nil
			break
		}
		var scopes []string
		for _, s := range strings.Split(value, "|") {
			if s = strings.TrimSpace(s); s != "" {
				scopes = append(scopes, s)
			}
		}
		cfg.Scopes = scopes
	case "nonescope":
		// 是否允许 scope=none（跨模块改动无单一 scope）；默认允许
		b, err := parseBool(value)
		if err != nil {
			return Config{}, fmt.Errorf("nonescope must be true or false, got %q", value)
		}
		cfg.AllowNone = &b
	default:
		return Config{}, fmt.Errorf("unknown config key %q (supported: api_key, lang, scopes, nonescope)", key)
	}
	if cfg.Lang == "" {
		cfg.Lang = "en"
	}
	return cfg, nil
}

// parseBool 解析 "true"/"false"（也接受 yes/no/1/0）。
func parseBool(v string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "yes", "1", "on":
		return true, nil
	case "false", "no", "0", "off":
		return false, nil
	}
	return false, fmt.Errorf("not a bool: %q", v)
}

// AllowNoneOf 返回是否允许 scope=none：默认 true，显式 false 关闭。
func AllowNoneOf(cfg Config) bool {
	return cfg.AllowNone == nil || *cfg.AllowNone
}

// ProjectConfig 是项目根 codesafe.yaml 的内容：项目自定义 scope 与 allow_none 开关。
type ProjectConfig struct {
	Scopes    map[string]string `yaml:"scopes"`
	AllowNone *bool             `yaml:"allow_none"` // 显式 false 时禁止 scope=none
}

// LoadProject 读取 dir 下的 codesafe.yaml / codesafe.yml；不存在返回空配置。
// 手写极简解析：只认顶层 "scopes:" 后跟 "- name" 或 "- name: desc" 行。
func LoadProject(dir string) (ProjectConfig, error) {
	var pc ProjectConfig
	var path string
	for _, name := range []string{"codesafe.yaml", "codesafe.yml"} {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); err == nil {
			path = p
			break
		}
	}
	if path == "" {
		return pc, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return pc, fmt.Errorf("cannot read %s: %w", path, err)
	}
	pc.Scopes = parseScopes(string(data))
	pc.AllowNone = parseAllowNone(string(data))
	return pc, nil
}

// parseAllowNone 解析顶层 "allow_none: false"；缺省返回 nil（默认允许）。
func parseAllowNone(y string) *bool {
	for _, raw := range strings.Split(y, "\n") {
		trim := strings.TrimSpace(raw)
		if strings.HasPrefix(trim, "allow_none:") {
			v := strings.TrimSpace(strings.TrimPrefix(trim, "allow_none:"))
			if b, err := parseBool(strings.Trim(v, `"'`)); err == nil {
				return &b
			}
		}
	}
	return nil
}

// parseScopes 解析 codesafe.yaml 的 scopes 段。支持两种写法：
//
//	scopes:
//	  - server
//	  - web: frontend UI
//
// 或行内 map：scopes: {server: "", web: frontend UI}
func parseScopes(y string) map[string]string {
	out := map[string]string{}
	lines := strings.Split(y, "\n")
	inScopes := false
	for _, raw := range lines {
		line := strings.TrimRight(raw, " \t")
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, "#") {
			continue
		}
		if !inScopes {
			if strings.HasPrefix(trim, "scopes:") {
				rest := strings.TrimSpace(strings.TrimPrefix(trim, "scopes:"))
				if rest != "" { // 行内 map
					rest = strings.Trim(rest, "{}")
					for _, kv := range strings.Split(rest, ",") {
						k, v, _ := strings.Cut(kv, ":")
						k = strings.TrimSpace(strings.Trim(k, `"'`))
						v = strings.TrimSpace(strings.Trim(v, `"'`))
						if k != "" {
							out[k] = v
						}
					}
					return out
				}
				inScopes = true
			}
			continue
		}
		// 在 scopes 段内：只收 "- name[: desc]" 行；遇到新的顶层键退出
		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "\t") {
			break
		}
		if strings.HasPrefix(trim, "-") {
			entry := strings.TrimSpace(strings.TrimPrefix(trim, "-"))
			name, desc, _ := strings.Cut(entry, ":")
			name = strings.TrimSpace(strings.Trim(name, `"'`))
			desc = strings.TrimSpace(strings.Trim(desc, `"'`))
			if name != "" {
				out[name] = desc
			}
		}
	}
	return out
}
