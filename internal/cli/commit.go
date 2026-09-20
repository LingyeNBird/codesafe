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

// prefixRe 提取 subject 开头的 conventional 前缀段（到冒号为止）。
// 兼容英文/中文冒号 ：:、英文/中文括号 ()（）、前缀后可有空格、可选 !。
// 捕获组1 = 前缀文本（不含冒号），用于规范化。
var prefixRe = regexp.MustCompile(`^([a-zA-Z]+\s*[（(]?[^:：)）]*[)）]?\s*!?)\s*[:：]\s*`)

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

	// 原始 commit message（第一个 -m 是 subject 行，其余是 body 段）
	raw := strings.Join([]string(msgs), "\n\n")

	// 1) commit_rules + 有无前缀判定：一个请求，state=完整 message
	subject := msgs[0]
	userPrefix := ""
	res, modelHasPrefix, err := scan.CheckCommitRules(ctx, client, raw, pc.CommitRules)
	if err != nil {
		return err
	}
	if fail := firstFail(res); fail != nil {
		return fmt.Errorf("commit message 违反规则 %s: %s", fail.Rule.ID, fail.Rule.Fail)
	}

	// 提取前缀：正则先匹配（兼容全角），匹配不到但模型判有前缀 → 中断要求规范格式
	if m := prefixRe.FindStringSubmatch(subject); m != nil {
		userPrefix = normalizePrefix(m[1])
		subject = strings.TrimSpace(subject[len(m[0]):])
	} else if modelHasPrefix {
		return fmt.Errorf("commit subject 看起来带了 type/scope 前缀，但格式不规范无法解析：%q\n请用 `type(scope): ` 或 `type: ` 格式（英文冒号，冒号后一个空格）", msgs[0])
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

// normalizePrefix 规范化提取到的前缀文本：全角括号→半角、去空格、保留 type(scope)! 结构。
func normalizePrefix(p string) string {
	p = strings.TrimSpace(p)
	p = strings.ReplaceAll(p, "（", "(")
	p = strings.ReplaceAll(p, "）", ")")
	p = strings.ReplaceAll(p, " ", "")
	return p
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
