/*
 * Copyright (C) 2026 codesafe contributors
 * SPDX-License-Identifier: AGPL-3.0-or-later
 * See COPYING in the project root for the full license.
 */
// config 包负责 API key 的持久化存取：保存在用户配置目录下的 codesafe/config.json。
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrNoAPIKey 表示尚未配置 API key，需要首次运行时输入或用 --config 设置。
var ErrNoAPIKey = errors.New("未配置 API key")

// Config 是持久化到磁盘的配置。
type Config struct {
	APIKey string `json:"api_key"`
}

// Path 返回配置文件路径：<os.UserConfigDir>/codesafe/config.json。
func Path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("无法定位用户配置目录: %w", err)
	}
	return filepath.Join(dir, "codesafe", "config.json"), nil
}

// Load 读取配置文件；文件不存在时返回 ErrNoAPIKey。
func Load() (Config, error) {
	p, err := Path()
	if err != nil {
		return Config{}, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return Config{}, ErrNoAPIKey
		}
		return Config{}, fmt.Errorf("读取配置失败: %w", err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("配置文件损坏: %w", err)
	}
	if cfg.APIKey == "" {
		return Config{}, ErrNoAPIKey
	}
	return cfg, nil
}

// SaveAPIKey 将 API key 写入配置文件，目录不存在时自动创建，文件权限 0600。
func SaveAPIKey(key string) error {
	p, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return fmt.Errorf("创建配置目录失败: %w", err)
	}
	data, err := json.Marshal(Config{APIKey: key})
	if err != nil {
		return err
	}
	if err := os.WriteFile(p, data, 0o600); err != nil {
		return fmt.Errorf("写入配置失败: %w", err)
	}
	return nil
}
