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

	"gopkg.in/yaml.v3"
)

var ErrNoAPIKey = errors.New("api key not configured")

// Config 是持久化到磁盘的配置。
type Config struct {
	APIKey    string   `json:"api_key"`
	Lang      string   `json:"lang"`                 // "en"（默认）或 "zh"
	Scopes    []string `json:"scopes,omitempty"`     // 用户级自定义 scope 名列表（--config 持久化）
	AllowNone *bool    `json:"allow_none,omitempty"` // 是否允许 scope=none；nil/false→不允许
	Cache     *bool    `json:"cache,omitempty"`      // 响应缓存；nil/true→开（默认），false→关
	// Screened 缓存按项目根路径筛出的形态域 scope 名；目录结构变化时重筛。
	Screened map[string]ScreenedEntry `json:"screened,omitempty"`
	// Todo 按项目根路径存当前任务描述（codesafe todo 设置，commit 成功后清空）。
	Todo map[string]string `json:"todo,omitempty"`
}

// CacheOn 返回响应缓存是否开启：默认开（nil→true），显式 false 关。
func CacheOn(cfg Config) bool {
	return cfg.Cache == nil || *cfg.Cache
}

// ScreenedEntry 是一个项目的筛选缓存：scope 名列表 + 生成时的目录指纹。
type ScreenedEntry struct {
	Scopes  []string `json:"scopes"`   // 筛出的形态域 scope 名
	DirHash string   `json:"dir_hash"` // 目录指纹，变了就重筛
}

// Path 返回配置文件路径：CODESAFE_CONFIG 环境变量优先，否则 <os.UserConfigDir>/codesafe/config.json。
func Path() (string, error) {
	// 显式覆盖优先——用户/CI 可指向指定配置文件。
	if env := os.Getenv("CODESAFE_CONFIG"); env != "" {
		return env, nil
	}
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
	case "cache":
		// 响应缓存开关；默认开
		b, err := parseBool(value)
		if err != nil {
			return Config{}, fmt.Errorf("cache must be true or false, got %q", value)
		}
		cfg.Cache = &b
	default:
		return Config{}, fmt.Errorf("unknown config key %q (supported: api_key, lang, scopes, nonescope, cache)", key)
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

// TodoOf 返回项目 root 当前的 TODO 任务描述；无则空串。
func TodoOf(cfg Config, root string) string {
	if cfg.Todo == nil {
		return ""
	}
	return cfg.Todo[root]
}

// SetTodo 写入/清空项目 root 的 TODO 并持久化。text 空=清空。
func SetTodo(cfg *Config, root, text string) error {
	if cfg.Todo == nil {
		cfg.Todo = map[string]string{}
	}
	if text == "" {
		delete(cfg.Todo, root)
	} else {
		cfg.Todo[root] = text
	}
	return Save(*cfg)
}

// TodoModeOf 解析 todo_mode 字符串为规范值：off|loose|strict；空/非法→默认 loose。
func TodoModeOf(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "off", "strict":
		return strings.ToLower(strings.TrimSpace(s))
	}
	return "loose"
}

type ProjectConfig struct {
	Scopes         map[string]string `yaml:"scopes"`
	AllowNone      *bool             `yaml:"allow_none"`
	Rules          []Rule            `yaml:"rules"`           // 代码规则：diff 检查
	CommitRules    []Rule            `yaml:"commit_rules"`    // 提交规则：commit message 检查
	PrefixConflict string            `yaml:"prefix_conflict"` // keep_user | override（默认）
	TodoMode       string            `yaml:"todo_mode"`       // off | loose | strict（默认 loose）
}

// Rule.TodoMode 规则级覆盖全局 todo_mode（仅含 {{TODO}} 插值的规则有意义）。
type Rule struct {
	ID       string `yaml:"id"`
	Level    string `yaml:"level"`     // error | warn
	Files    string `yaml:"files"`     // glob，如 "*.vue"；空=对所有 diff 生效
	Lines    string `yaml:"lines"`     // 行段，如 "1-6"、"1-4,-10--1"（负数=倒数）；设后该规则不走 diff，改喂匹配文件的指定行内容
	Text     string `yaml:"text"`      // 判定说明（instructions），可含 {{TODO}} 插值
	Pass     string `yaml:"pass"`      // 满足时的描述
	Fail     string `yaml:"fail"`      // 违反时的描述
	On       string `yaml:"on"`        // commit_rules 用：subject|body|prefix|all
	TodoMode string `yaml:"todo_mode"` // off|loose|strict，覆盖全局（对含 {{TODO}} 的规则）
}

// LoadProject 读取 dir 下的 codesafe.yaml / codesafe.yml；不存在返回空配置。
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
	if err := yaml.Unmarshal(data, &pc); err != nil {
		return pc, fmt.Errorf("cannot parse %s: %w", path, err)
	}
	return pc, nil
}
