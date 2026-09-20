/*
 * Copyright (C) 2026 codesafe contributors
 * SPDX-License-Identifier: AGPL-3.0-or-later
 * See COPYING in the project root for the full license.
 */
// config 包负责持久化配置（API key、输出语言）：保存在用户配置目录下的 codesafe/config.json。
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrNoAPIKey 表示尚未配置 API key，需要首次运行时输入或用 --config 设置。
var ErrNoAPIKey = errors.New("api key not configured")

// Config 是持久化到磁盘的配置。
type Config struct {
	APIKey string `json:"api_key"`
	Lang   string `json:"lang"`  // "en"（默认）或 "zh"
	Glyph  string `json:"glyph"` // "nerd"（默认）或 "emoji"
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
	if cfg.Glyph == "" {
		cfg.Glyph = "nerd"
	}
	if cfg.APIKey == "" {
		return cfg, ErrNoAPIKey // 返回已读的 Lang/Glyph，调用方按需取用
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

// Set 解析 "key=value" 并写入配置；支持 api_key、lang。返回更新后的配置。
func Set(kv string) (Config, error) {
	key, value, ok := strings.Cut(kv, "=")
	if !ok {
		return Config{}, fmt.Errorf("expected --config key=value, got %q", kv)
	}
	cfg, _ := Load() // 读旧值以便局部更新；文件不存在也无妨
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
	case "glyph":
		if value != "nerd" && value != "emoji" {
			return Config{}, fmt.Errorf("glyph must be nerd or emoji, got %q", value)
		}
		cfg.Glyph = value
	default:
		return Config{}, fmt.Errorf("unknown config key %q (supported: api_key, lang, glyph)", key)
	}
	if cfg.Lang == "" {
		cfg.Lang = "en"
	}
	if cfg.Glyph == "" {
		cfg.Glyph = "nerd"
	}
	if err := Save(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}
