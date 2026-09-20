/*
 * Copyright (C) 2026 codesafe contributors
 * SPDX-License-Identifier: AGPL-3.0-or-later
 * See COPYING in the project root for the full license.
 */
// cli 包解析命令行参数、读取 diff、调用 TypeSafe 做 commit 分类并输出建议。
package cli

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"codesafe/internal/config"
	"codesafe/internal/render"
	"codesafe/internal/scan"
	"codesafe/internal/typesafe"
)

// Run 是 CLI 主流程：分发子命令（diff/commit）或默认的 classify。

func Run(args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "diff":
			return runDiff(args[1:])
		case "commit":
			return runCommit(args[1:])
		case "init":
			return runInit(args[1:])
		}
	}
	return runClassify(args)
}

// runClassify 是默认的 commit 分类流程。
func runClassify(args []string) error {
	fs := flag.NewFlagSet("codesafe", flag.ContinueOnError)
	var (
		dir     = fs.String("dir", ".", "git repo directory (default: current)")
		setConf = fs.String("config", "", "set a config value and exit: api_key=<key> | lang=en|zh | scopes=a|b|c")
		tmpSet  = fs.String("set", "", "temporary config for this run only (same keys as --config, not saved)")
		model   = fs.String("model", "jev-latest", "TypeSafe model ID or alias")
		source  = fs.String("source", "auto", "diff source: staged | worktree | <commit-sha> | auto (staged, else worktree)")
		detail  = fs.Bool("detail", false, "show percentage detail + token/cost stats (default prints just type(scope)!)")
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

	// 仓库根
	root, err := scan.RepoRoot(*dir)
	if err != nil {
		return err
	}
	if root == "" {
		return fmt.Errorf("%s is not inside a git repository", *dir)
	}

	// 加载配置 + --set 临时覆盖
	var cfg config.Config
	if c, err := config.Load(); err == nil || errors.Is(err, config.ErrNoAPIKey) {
		cfg = c
	}
	if cfg.Lang == "" {
		cfg.Lang = "en"
	}
	if *tmpSet != "" {
		for _, kv := range strings.Split(*tmpSet, ",") {
			var err error
			cfg, err = config.Apply(cfg, strings.TrimSpace(kv))
			if err != nil {
				return err
			}
		}
	}
	if cfg.APIKey == "" {
		key, perr := promptKey()
		if perr != nil {
			return perr
		}
		if err := config.Save(config.Config{APIKey: key, Lang: cfg.Lang, Scopes: cfg.Scopes}); err != nil {
			return err
		}
		cfg.APIKey = key
		fmt.Println("API key saved to", mustConfigPath())
	}

	client := typesafe.NewClient(cfg.APIKey, *model)

	// scope 来源优先级：项目 codesafe.yaml > 用户 --config > 筛选缓存/重筛 > 内置默认
	pc, _ := config.LoadProject(root)
	scopes := resolveScopes(context.Background(), client, pc, &cfg, root)
	allowNone := resolveAllowNone(pc, cfg)

	// 取 diff
	diff, err := getDiff(root, *source)
	if err != nil {
		return err
	}
	if strings.TrimSpace(diff) == "" {
		return fmt.Errorf("no diff found (source=%s): stage changes or pass a commit", *source)
	}

	res, err := scan.Classify(context.Background(), client, diff, scopes, allowNone)
	if err != nil {
		return err
	}
	fmt.Print(render.Result(res, cfg.Lang, *detail))
	return nil
}

// resolveScopes 合成 scope 集：项目 codesafe.yaml > 用户配置 > 筛选(缓存或重筛) > 内置。
// 筛选写回 cfg.Screened 并持久化。yaml/--config 优先时不筛选。
func resolveScopes(ctx context.Context, client *typesafe.Client, pc config.ProjectConfig, cfg *config.Config, root string) map[string]string {
	if len(pc.Scopes) > 0 {
		return pc.Scopes
	}
	if len(cfg.Scopes) > 0 {
		return scan.ScopesFromNames(cfg.Scopes)
	}
	hash := scan.DirFingerprint(root)
	if cfg.Screened != nil {
		if e, ok := cfg.Screened[root]; ok && e.DirHash == hash {
			return scan.ScopesFromNames(e.Scopes)
		}
	}
	screened, err := scan.ScreenScopes(ctx, client, root, 0.5)
	if err != nil {
		return nil // 筛选失败退回内置全集
	}
	if cfg.Screened == nil {
		cfg.Screened = map[string]config.ScreenedEntry{}
	}
	cfg.Screened[root] = config.ScreenedEntry{Scopes: sortedKeys(screened), DirHash: hash}
	config.Save(*cfg)
	return screened
}

// sortedKeys 返回 map 键的排序切片。
func sortedKeys(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// resolveAllowNone 合成是否允许 scope=none：项目 yaml allow_none > --config nonescope > 默认 true。
func resolveAllowNone(pc config.ProjectConfig, cfg config.Config) bool {
	if pc.AllowNone != nil {
		return *pc.AllowNone
	}
	return config.AllowNoneOf(cfg)
}

// getDiff 按 source 取 diff：staged → worktree → commit。
func getDiff(root, source string) (string, error) {
	switch source {
	case "staged":
		return scan.StagedDiff(root)
	case "worktree":
		return scan.WorktreeDiff(root)
	case "auto":
		if d, err := scan.StagedDiff(root); err == nil && strings.TrimSpace(d) != "" {
			return d, nil
		}
		return scan.WorktreeDiff(root)
	default:
		// 当作 commit sha / rev
		return scan.CommitDiff(root, source)
	}
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

// usage 打印帮助；fs 为已注册全部 flag 的集合。
func usage(fs *flag.FlagSet) func() {
	return func() {
		fmt.Fprintf(os.Stderr, `codesafe — suggest a conventional-commit type(scope) for your diff via the TypeSafe API

Usage:
  codesafe [flags]            classify the current staged (or worktree) diff
  codesafe --source <sha>     classify a specific commit

Flags:
`)
		fs.PrintDefaults()
		fmt.Fprintf(os.Stderr, `
Diff source (--source):
  staged     git diff --cached        (what you're about to commit)
  worktree   git diff HEAD            (all uncommitted changes)
  <sha>      a specific commit
  auto       staged if present, else worktree   (default)

Persistent config (saved):
  codesafe --config api_key=<key>
  codesafe --config lang=zh                 (output in Chinese; default en)
  codesafe --config scopes=a|b|c            (your scope names, | separated)

Temporary overrides for one run only (not saved):
  codesafe --set api_key=<key>
  codesafe --set lang=en

Project scopes (committed with the repo):
  create codesafe.yaml in the repo root:
    allow_none: false        # forbid scope=none (require a concrete scope)
    scopes:
      - server
      - web: frontend UI
      - installer
  Project scopes override --config scopes, which override the built-in set.
  none (cross-cutting change, no parens) is offered unless disabled via
  allow_none: false or --config nonescope=false.
`)
	}
}
