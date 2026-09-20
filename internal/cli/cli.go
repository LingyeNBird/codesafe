/*
 * Copyright (C) 2026 codesafe contributors
 * SPDX-License-Identifier: AGPL-3.0-or-later
 * See COPYING in the project root for the full license.
 */
// cli 包解析命令行参数、处理首次运行的 API key 输入、--config 设置以及 --dry-run 模拟。
package cli

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"codesafe/internal/config"
	"codesafe/internal/render"
	"codesafe/internal/scan"
	"codesafe/internal/typesafe"
)

// Run 是 CLI 主流程，返回非 nil error 时由调用方退出非零。
func Run(args []string) error {
	fs := flag.NewFlagSet("codesafe", flag.ContinueOnError)
	var (
		dir    = fs.String("dir", ".", "要扫描的目录（默认当前目录）")
		setKey = fs.String("config", "", "设置 API key 并退出")
		dryRun = fs.Bool("dry-run", false, "只列出将被扫描的文件，不调用 API")
		model  = fs.String("model", "jev-latest", "TypeSafe 模型 ID 或别名")
		width  = fs.Int("width", 40, "路径列显示宽度")
		concur = fs.Int("concurrency", 16, "并发请求数")
		rps    = fs.Float64("rps", 20, "每秒请求上限")
	)
	fs.Usage = usage
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *setKey != "" {
		if err := config.SaveAPIKey(*setKey); err != nil {
			return err
		}
		fmt.Println("API key 已保存到", mustConfigPath())
		return nil
	}

	root, err := scan.RepoRoot(*dir)
	if err != nil {
		return err
	}
	if root == "" {
		root, err = filepath.Abs(*dir)
		if err != nil {
			return fmt.Errorf("无法解析目录: %w", err)
		}
	}

	var apiKey string
	if *dryRun {
		apiKey = "dry-run"
	} else {
		cfg, err := config.Load()
		if err != nil {
			if !errors.Is(err, config.ErrNoAPIKey) {
				return err
			}
			key, perr := promptKey()
			if perr != nil {
				return perr
			}
			if err := config.SaveAPIKey(key); err != nil {
				return err
			}
			cfg.APIKey = key
			fmt.Println("API key 已保存到", mustConfigPath())
		}
		apiKey = cfg.APIKey
	}

	client := typesafe.NewClient(apiKey, *model)
	s := scan.NewScanner(client, scan.Options{
		Concurrency: *concur,
		RPS:         *rps,
		DryRun:      *dryRun,
	})

	results, err := s.Run(context.Background(), root)
	if err != nil {
		return err
	}
	if *dryRun {
		fmt.Printf("将扫描 %d 个文件（跳过项标注）:\n", countScannable(results))
	}
	fmt.Print(render.Table(results, *width))
	return nil
}

// promptKey 首次运行时从 stdin 读取 API key。
func promptKey() (string, error) {
	fmt.Fprint(os.Stderr, "首次运行需要 TypeSafe API key（可从 https://console.typesafe.ai/ 获取）: ")
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("读取 API key 失败: %w", err)
	}
	key := strings.TrimSpace(line)
	if key == "" {
		return "", errors.New("API key 不能为空")
	}
	return key, nil
}

// mustConfigPath 返回配置文件路径，失败时返回占位文本（仅用于展示）。
func mustConfigPath() string {
	p, err := config.Path()
	if err != nil {
		return "<config>"
	}
	return p
}

// countScannable 统计 dry-run 中未跳过的文件数。
func countScannable(rs []scan.FileResult) int {
	n := 0
	for _, r := range rs {
		if !r.Skipped {
			n++
		}
	}
	return n
}

// usage 打印帮助。
func usage() {
	fmt.Fprintf(os.Stderr, `codesafe — 用 TypeSafe System One 对 git 跟踪文件做逐文件安全/缺陷粗筛

用法:
  codesafe [flags]

Flags:
`)
	flag.PrintDefaults()
	fmt.Fprintf(os.Stderr, `
首次运行会提示输入 API key 并保存到用户配置目录；之后直接运行。
使用 --config <key> 可随时更新。
`)
}
