/*
 * Copyright (C) 2026 codesafe contributors
 * SPDX-License-Identifier: AGPL-3.0-or-later
 * See COPYING in the project root for the full license.
 */
// codesafe commit：校验 commit message 规则 + 代码规则，生成 type(scope) 前缀并执行 git commit。
package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"codesafe/internal/config"
	"codesafe/internal/scan"
	"codesafe/internal/typesafe"
)

// multiFlag 收集可重复的 -m 参数。
type multiFlag []string

// String 返回 -m 值拼接（flag.Value 接口）。
func (m *multiFlag) String() string { return strings.Join(*m, "\n") }

// Set 追加一个 -m 值（flag.Value 接口）。
func (m *multiFlag) Set(v string) error {
	*m = append(*m, v)
	return nil
}

// prefixRe 匹配 commit subject 开头的 conventional 前缀 "type(scope)!:" 或 "type:"。
var prefixRe = regexp.MustCompile(`^[a-zA-Z]+(\([^)]*\))?!?:\s*`)

// runCommit 校验规则、生成前缀、执行 git commit。
func runCommit(args []string) error {
	fs := flag.NewFlagSet("codesafe commit", flag.ContinueOnError)
	var (
		dir   = fs.String("dir", ".", "git repo directory")
		model = fs.String("model", "jev-latest", "TypeSafe model ID")
		msgs  multiFlag
	)
	fs.Var(&msgs, "m", "commit message (repeatable; first is subject, rest are body)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(msgs) == 0 {
		return fmt.Errorf("commit needs at least one -m")
	}
	cfg, pc, root, err := setup(*dir)
	if err != nil {
		return err
	}
	if cfg.APIKey == "" {
		return config.ErrNoAPIKey
	}
	client := typesafe.NewClient(cfg.APIKey, *model)
	ctx := context.Background()

	// 第一个 -m 可能自带 type(scope): 前缀 → 拆出
	subject := msgs[0]
	userPrefix := ""
	if m := prefixRe.FindString(subject); m != "" {
		userPrefix = strings.TrimSpace(strings.TrimSuffix(m, ":"))
		subject = strings.TrimSpace(subject[len(m):])
	}
	body := strings.Join(msgs[1:], "\n\n")

	// 1) commit_rules：查 subject/body/用户前缀
	if len(pc.CommitRules) > 0 {
		res, err := scan.CheckCommitRules(ctx, client, subject, body, userPrefix, pc.CommitRules)
		if err != nil {
			return err
		}
		if fail := firstFail(res); fail != nil {
			return fmt.Errorf("commit message 违反规则 %s: %s", fail.Rule.ID, fail.Rule.Fail)
		}
	}

	// 2) rules：查 staged diff（要提交的内容）的代码规则
	diff, err := scan.StagedDiff(root)
	if strings.TrimSpace(diff) != "" && len(pc.Rules) > 0 {
		res, err := scan.CheckRules(ctx, client, diff, pc.Rules)
		if err != nil {
			return err
		}
		for _, r := range res {
			if r.Skip || r.Pass {
				continue
			}
			if r.Rule.Level == "warn" {
				fmt.Fprintf(os.Stderr, "%swarn%s %s: %s\n", yellow(), reset(), r.Rule.ID, r.Rule.Fail)
				continue
			}
			return fmt.Errorf("diff 违反规则 %s: %s", r.Rule.ID, r.Rule.Fail)
		}
	}

	// 3) 生成 type(scope) 前缀（若用户已带前缀且 keep_user，则直接用用户的）
	var prefix string
	if userPrefix != "" && pc.PrefixConflict == "keep_user" {
		prefix = userPrefix
	} else {
		scopes := resolveScopes(ctx, client, pc, &cfg, root)
		allowNone := resolveAllowNone(pc, cfg)
		res, err := scan.Classify(ctx, client, diff, scopes, allowNone)
		if err != nil {
			return err
		}
		prefix = res.Suggestion()
	}

	// 4) 拼接并执行 git commit
	full := prefix + ": " + subject
	commitArgs := []string{"-C", root, "commit", "-m", full}
	for _, m := range msgs[1:] {
		commitArgs = append(commitArgs, "-m", m)
	}
	cmd := exec.Command("git", commitArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	fmt.Fprintf(os.Stderr, "%scommit:%s %s\n", dim(), reset(), full)
	return cmd.Run()
}

// firstFail 返回第一个 error 级违反的规则；无则 nil。
func firstFail(res []scan.RuleResult) *scan.RuleResult {
	for i := range res {
		if !res[i].Skip && !res[i].Pass && res[i].Rule.Level != "warn" {
			return &res[i]
		}
	}
	return nil
}
