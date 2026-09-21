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
		dir     = fs.String("dir", ".", "git repo directory")
		model   = fs.String("model", "jev-latest", "TypeSafe model ID")
		refresh = fs.Bool("refresh", false, "bypass the response cache and re-judge")
		msgs    multiFlag
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
	client := newClient(cfg, *model, *refresh)
	ctx := context.Background()

	// 原始 commit message（第一个 -m 是 subject 行，其余是 body 段）
	raw := strings.Join([]string(msgs), "\n\n")

	// 1) commit_rules + 有无前缀判定：一个请求，state=完整 message
	subject := msgs[0]
	userPrefix := ""
	res, modelHasPrefix, err := scan.CheckCommitRules(ctx, client, raw, pc.CommitRules, pc.PrefixConflict == "keep_user")
	if err != nil {
		return err
	}
	if fail := firstFail(res); fail != nil {
		return fmt.Errorf("commit message 违反规则 %s: %s", fail.Rule.ID, fail.Rule.Fail)
	}

	// 前缀处理：先信模型判定有无前缀；有才用正则提取，正则提不出（格式脏）→ 中断。
	// 这样 "delete 三档判定：xxx" 这类 subject 开头的普通词不会被正则误吃成前缀。
	if modelHasPrefix {
		if m := prefixRe.FindStringSubmatch(subject); m != nil {
			userPrefix = normalizePrefix(m[1])
			subject = strings.TrimSpace(subject[len(m[0]):])
		} else {
			return fmt.Errorf("commit subject 看起来带了 type/scope 前缀，但格式不规范无法解析：%q\n请用 `type(scope): ` 或 `type: ` 格式（英文冒号，冒号后一个空格）", msgs[0])
		}
	}

	// 2) rules + 内建 TODO 判定：查 staged diff（要提交的内容）
	todo := config.TodoOf(cfg, root)
	todoMode := config.TodoModeOf(pc.TodoMode)
	diff, err := scan.StagedDiff(root)
	if strings.TrimSpace(diff) != "" && (len(pc.Rules) > 0 || todo != "") {
		res, err := scan.CheckRules(ctx, client, diff, pc.Rules, todo, pc.TodoMode)
		if err != nil {
			return err
		}
		for _, r := range res {
			if r.Skip {
				continue
			}
			if r.TodoMissing {
				return fmt.Errorf("规则 %s 要求设置 TODO（todo_mode=strict），请先 `codesafe todo <任务>`", r.Rule.ID)
			}
			if r.Rule.ID == "__todo__" && !r.Pass {
				return fmt.Errorf("diff 未实现 TODO：%q\n请更新 TODO（codesafe todo <新任务>）或修正提交内容", todo)
			}
			if r.Pass {
				continue
			}
			if r.Rule.Level == "warn" {
				fmt.Fprintf(os.Stderr, "%swarn%s %s: %s\n", yellow(), reset(), r.Rule.ID, r.Rule.Fail)
				continue
			}
			return fmt.Errorf("diff 违反规则 %s: %s", r.Rule.ID, r.Rule.Fail)
		}
	}
	// strict 模式且 TODO 为空 → 必须先设置任务（diff 为空也拦）
	if todoMode == "strict" && todo == "" {
		return fmt.Errorf("todo_mode=strict：必须先 `codesafe todo <任务>` 设置任务再提交")
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
	if err := cmd.Run(); err != nil {
		return err
	}
	// 提交成功 → 清空 TODO
	if todo != "" {
		config.SetTodo(&cfg, root, "")
	}
	return nil
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
