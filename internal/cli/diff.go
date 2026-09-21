/*
 * Copyright (C) 2026 codesafe contributors
 * SPDX-License-Identifier: AGPL-3.0-or-later
 * See COPYING in the project root for the full license.
 */
// codesafe diff：检查当前工作区 diff 是否满足 codesafe.yaml 的 rules。
package cli

import (
	"context"
	"flag"
	"fmt"
	"strings"

	"codesafe/internal/config"
	"codesafe/internal/scan"
	"codesafe/internal/typesafe"
)

// ANSI 颜色码（与 render 包一致）。
// dim 返回暗色。
func dim() string { return "\x1b[2m" }

// reset 返回重置。
func reset() string { return "\x1b[0m" }

// green 返回绿色。
func green() string { return "\x1b[32m" }

// yellow 返回黄色。
func yellow() string { return "\x1b[33m" }

// red 返回红色。
func red() string { return "\x1b[31m" }

// runDiff 检查工作区/staged diff 是否满足项目 codesafe.yaml 的代码规则。
// 默认查整个工作区（git diff HEAD）；--staged 只查暂存，--worktree 只查未暂存。
func runDiff(args []string) error {
	fs := flag.NewFlagSet("codesafe diff", flag.ContinueOnError)
	var (
		dir      = fs.String("dir", ".", "git repo directory")
		model    = fs.String("model", "jev-latest", "TypeSafe model ID")
		staged   = fs.Bool("staged", false, "check only staged changes")
		worktree = fs.Bool("worktree", false, "check only unstaged (worktree) changes")
		detail   = fs.Bool("detail", false, "show per-rule probabilities")
		refresh  = fs.Bool("refresh", false, "bypass the response cache and re-judge")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, pc, root, err := setup(*dir)
	if err != nil {
		return err
	}
	todo := config.TodoOf(cfg, root)
	if len(pc.Rules) == 0 && todo == "" {
		fmt.Println(dim() + "no rules in codesafe.yaml" + reset())
		return nil
	}
	diff, err := pickDiff(root, *staged, *worktree)
	if err != nil {
		return err
	}
	if strings.TrimSpace(diff) == "" {
		return fmt.Errorf("no diff to check")
	}
	client := newClient(cfg, *model, *refresh)
	results, err := scan.CheckRules(context.Background(), client, diff, pc.Rules, todo, pc.TodoMode)
	if err != nil {
		return err
	}
	printRuleResults(results, cfg.Lang, *detail)
	return nil
}

// pickDiff 按 staged/worktree 标志取 diff；都不给=整个工作区(git diff HEAD)。
func pickDiff(root string, staged, worktree bool) (string, error) {
	switch {
	case staged && !worktree:
		return scan.StagedDiff(root)
	case worktree && !staged:
		return scan.UnstagedDiff(root)
	default:
		return scan.WorktreeDiff(root) // git diff HEAD = 全部工作区改动
	}
}

// printRuleResults 打印规则判定：违反的列 id+fail 描述，warn 标注。
func printRuleResults(results []scan.RuleResult, lang string, detail bool) {
	zh := lang == "zh"
	var fails, warns []scan.RuleResult
	for _, r := range results {
		if r.Skip {
			continue
		}
		if !r.Pass {
			if r.Rule.Level == "warn" {
				warns = append(warns, r)
			} else {
				fails = append(fails, r)
			}
		} else if detail {
			if zh {
				fmt.Printf("  %s✓%s %s  %s(%.0f%%)\n", green(), reset(), r.Rule.ID, dim(), r.Prob*100)
			} else {
				fmt.Printf("  %s✓%s %s  %s(%.0f%%)\n", green(), reset(), r.Rule.ID, dim(), r.Prob*100)
			}
		}
	}
	for _, r := range warns {
		lbl := "WARN"
		if zh {
			lbl = "警告"
		}
		fmt.Printf("  %s%s%s %s  %s%s%s\n", yellow(), lbl, reset(), r.Rule.ID, dim(), r.Rule.Fail, reset())
	}
	if len(fails) == 0 && len(warns) == 0 {
		if zh {
			fmt.Println(green() + "满足所有规则" + reset())
		} else {
			fmt.Println(green() + "all rules satisfied" + reset())
		}
		return
	}
	for _, r := range fails {
		lbl := "FAIL"
		desc := r.Rule.Fail
		if zh {
			lbl = "违反"
		}
		if r.TodoMissing {
			desc = "要求设置 TODO（todo_mode=strict），先 `codesafe todo <任务>`"
		} else if r.Rule.ID == "__todo__" {
			desc = "diff 未实现 TODO：" + r.Rule.Fail
		}
		fmt.Printf("  %s%s%s %s  %s%s%s\n", red(), lbl, reset(), r.Rule.ID, dim(), desc, reset())
	}
}

// setup 加载用户配置 + 项目配置 + 仓库根。
func setup(dir string) (config.Config, config.ProjectConfig, string, error) {
	root, err := scan.RepoRoot(dir)
	if err != nil {
		return config.Config{}, config.ProjectConfig{}, "", err
	}
	if root == "" {
		return config.Config{}, config.ProjectConfig{}, "", fmt.Errorf("%s is not inside a git repository", dir)
	}
	cfg, _ := config.Load()
	pc, _ := config.LoadProject(root)
	return cfg, pc, root, nil
}

// newClient 建 typesafe client 并按配置应用缓存开关与 --refresh。
func newClient(cfg config.Config, model string, refresh bool) *typesafe.Client {
	c := typesafe.NewClient(cfg.APIKey, model)
	c.Cache = config.CacheOn(cfg)
	c.Refresh = refresh
	return c
}
