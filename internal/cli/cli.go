/*
 * Copyright (C) 2026 codesafe contributors
 * SPDX-License-Identifier: AGPL-3.0-or-later
 * See COPYING in the project root for the full license.
 */
// cli 包解析命令行参数、处理首次运行的 API key 输入、--config key=value 设置以及 --dry-run 模拟。
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
	"time"

	"codesafe/internal/config"
	"codesafe/internal/render"
	"codesafe/internal/scan"
	"codesafe/internal/typesafe"
)

// Run 是 CLI 主流程，返回非 nil error 时由调用方退出非零。
func Run(args []string) error {
	fs := flag.NewFlagSet("codesafe", flag.ContinueOnError)
	var (
		dir     = fs.String("dir", ".", "directory to scan (default: current)")
		subdir  = fs.String("subdir", "", "only scan this subdirectory within --dir")
		files   = fs.String("files", "", "comma-separated file list to force-scan (bypasses git/credential filters)")
		setConf = fs.String("config", "", "set a config value and exit: api_key=<key> | lang=en|zh | glyph=nerd|emoji | override=<path>:<code|config|doc>")
		dryRun  = fs.Bool("dry-run", false, "list files that would be scanned without calling the API")
		model   = fs.String("model", "jev-latest", "TypeSafe model ID or alias")
		width   = fs.Int("width", 40, "path column display width")
		concur  = fs.Int("concurrency", 16, "concurrent request count")
		rps     = fs.Float64("rps", 20, "max requests per second")
	)
	fs.Usage = usage(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *setConf != "" {
		cfg, err := config.Set(*setConf)
		if err != nil {
			return err
		}
		fmt.Println("config saved to", mustConfigPath(), "(lang="+cfg.Lang+")")
		return nil
	}

	root, err := scan.RepoRoot(*dir)
	if err != nil {
		return err
	}
	if root == "" {
		root, err = filepath.Abs(*dir)
		if err != nil {
			return fmt.Errorf("cannot resolve directory: %w", err)
		}
	}

	var cfg config.Config
	if *dryRun {
		cfg = config.Config{APIKey: "dry-run", Lang: "en", Glyph: "nerd"}
		if c, err := config.Load(); err == nil || errors.Is(err, config.ErrNoAPIKey) {
			if c.Lang != "" {
				cfg.Lang = c.Lang
			}
			if c.Glyph != "" {
				cfg.Glyph = c.Glyph
			}
			cfg.Overrides = c.Overrides // 覆盖规则在 dry-run 也生效
		}
	} else {
		cfg, err = config.Load()
		if err != nil {
			if !errors.Is(err, config.ErrNoAPIKey) {
				return err
			}
			key, perr := promptKey()
			if perr != nil {
				return perr
			}
			if err := config.Save(config.Config{APIKey: key, Lang: cfg.Lang, Glyph: cfg.Glyph}); err != nil {
				return err
			}
			cfg.APIKey = key
			fmt.Println("API key saved to", mustConfigPath())
		}
	}
	if cfg.Glyph == "" {
		cfg.Glyph = "nerd"
	}
	if cfg.Lang == "" {
		cfg.Lang = "en"
	}

	var forceFiles []string
	if *files != "" {
		for _, f := range strings.Split(*files, ",") {
			if f = strings.TrimSpace(f); f != "" {
				forceFiles = append(forceFiles, f)
			}
		}
	}

	client := typesafe.NewClient(cfg.APIKey, *model)
	s := scan.NewScanner(client, scan.Options{
		Concurrency: *concur,
		RPS:         *rps,
		DryRun:      *dryRun,
		Overrides:   cfg.Overrides,
		Force:       len(forceFiles) > 0,
	})

	start := time.Now()
	results, err := s.Run(context.Background(), root, *subdir, forceFiles)
	if err != nil {
		return err
	}
	totalTime := time.Since(start)
	if *dryRun {
		fmt.Printf(render.T(cfg.Lang, "dryrun_header")+"\n", countScannable(results))
	}
	fmt.Print(render.Table(results, *width, cfg.Lang, cfg.Glyph))
	if !*dryRun {
		fmt.Print(render.Legend(cfg.Lang))
		fmt.Print(render.Summary(scan.Summarize(results, totalTime), cfg.Lang))
	}
	return nil
}

// promptKey 首次运行时从 stdin 读取 API key。
func promptKey() (string, error) {
	fmt.Fprint(os.Stderr, "TypeSafe API key required (get one at https://console.typesafe.ai/): ")
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("cannot read API key: %w", err)
	}
	key := strings.TrimSpace(line)
	if key == "" {
		return "", errors.New("API key cannot be empty")
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

// usage 打印帮助；fs 为已注册全部 flag 的集合。
func usage(fs *flag.FlagSet) func() {
	return func() {
		fmt.Fprintf(os.Stderr, `codesafe — fast per-file safety/bug triage via the TypeSafe System One API

Usage:
  codesafe [flags]

Flags:
`)
		fs.PrintDefaults()
		fmt.Fprintf(os.Stderr, `
On first run you will be prompted for your API key, saved to the user config dir.
Update values any time with:
  codesafe --config api_key=<key>
  codesafe --config lang=zh                     (output in Chinese; default en)
  codesafe --config glyph=emoji                 (emoji gauge instead of Nerd Font)
  codesafe --config override=<path>:<kind>      (force a file's scan mode: code|config|doc)
`)
	}
}
