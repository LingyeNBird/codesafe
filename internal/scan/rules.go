/*
 * Copyright (C) 2026 codesafe contributors
 * SPDX-License-Identifier: AGPL-3.0-or-later
 * See COPYING in the project root for the full license.
 */
// scan 包：自定义规则检查——把 codesafe.yaml 的 rules 批量当 noul 问模型，判定 diff 是否违反。
package scan

import (
	"context"
	"fmt"
	"strings"

	"codesafe/internal/config"
	"codesafe/internal/typesafe"
)

// RuleResult 是一条规则的判定结果。
type RuleResult struct {
	Rule config.Rule
	Pass bool    // noul ≥0.5 视为满足
	Prob float64 // 满足的概率
	Skip bool    // files glob 无匹配，跳过未问
}

// DiffFiles 从 unified diff 文本提取改动的文件路径（去重、去 a//b/ 前缀）。
func DiffFiles(diff string) []string {
	set := map[string]bool{}
	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "+++ b/") {
			set[strings.TrimPrefix(line, "+++ b/")] = true
		} else if strings.HasPrefix(line, "diff --git ") {
			// diff --git a/x b/y —— 取 b 路径
			parts := strings.Fields(line)
			if len(parts) >= 4 {
				set[strings.TrimPrefix(parts[3], "b/")] = true
			}
		}
	}
	out := make([]string, 0, len(set))
	for f := range set {
		out = append(out, f)
	}
	return out
}

// matchRule 判 diff 是否含匹配 rule.Files glob 的文件；空 glob 恒匹配。
func matchRule(rule config.Rule, files []string) bool {
	if rule.Files == "" {
		return true
	}
	for _, f := range files {
		if globMatch(rule.Files, f) {
			return true
		}
	}
	return false
}

// globMatch 极简 glob：支持 "*.ext"、"dir/*"、"*.{a,b}"、名字精确匹配。
func globMatch(pattern, name string) bool {
	// 展开 {a,b}
	if i := strings.Index(pattern, "{"); i >= 0 {
		j := strings.Index(pattern[i:], "}")
		if j >= 0 {
			j += i
			for _, alt := range strings.Split(pattern[i+1:j], ",") {
				if globMatch(pattern[:i]+alt+pattern[j+1:], name) {
					return true
				}
			}
			return false
		}
	}
	return matchPat(pattern, name)
}

// matchPat 是经典 * / ? 匹配（按 / 分段不重要，直接全串匹配）。
func matchPat(pat, s string) bool {
	// 简化：* 匹配任意（含 /），? 匹配单字符
	return wildcard(pat, s)
}

// wildcard 是迭代式 * / ? 通配匹配。
func wildcard(pat, s string) bool {
	// 迭代通配匹配
	px, sx := 0, 0
	starP, starS := -1, -1
	for sx < len(s) {
		if px < len(pat) && (pat[px] == '?' || pat[px] == s[px]) {
			px++
			sx++
		} else if px < len(pat) && pat[px] == '*' {
			starP, starS = px, sx
			px++
		} else if starP >= 0 {
			px = starP + 1
			starS++
			sx = starS
		} else {
			return false
		}
	}
	for px < len(pat) && pat[px] == '*' {
		px++
	}
	return px == len(pat)
}

// CheckRules 对 diff 批量检查 rules。每条 rule 生成一个 noul；同一 diff 一次请求。
// 若整批超 max_tokens，把 description 最长的规则拆出单独问（state=该规则说明+匹配文件 diff 段），其余重试。
// 返回每条规则的判定（含 skip）。
func CheckRules(ctx context.Context, client *typesafe.Client, diff string, rules []config.Rule) ([]RuleResult, error) {
	files := DiffFiles(diff)
	var active []config.Rule
	results := map[string]RuleResult{}
	for _, r := range rules {
		if r.ID == "" {
			r.ID = r.Text
		}
		if matchRule(r, files) {
			active = append(active, r)
		} else {
			results[r.ID] = RuleResult{Rule: r, Pass: true, Skip: true}
		}
	}
	if len(active) == 0 {
		return nil, nil
	}
	got, err := askRules(ctx, client, diff, active)
	if err != nil {
		return nil, err
	}
	for _, r := range got {
		results[r.Rule.ID] = r
	}
	out := make([]RuleResult, 0, len(rules))
	for _, r := range rules {
		id := r.ID
		if id == "" {
			id = r.Text
		}
		out = append(out, results[id])
	}
	return out, nil
}

// askRules 批量问 rules；超限时拆最长的单独问。递归直到全部判出。
func askRules(ctx context.Context, client *typesafe.Client, diff string, rules []config.Rule) ([]RuleResult, error) {
	qs := map[string]typesafe.Question{}
	for _, r := range rules {
		qs[r.ID] = ruleNoul(r)
	}
	answers, _, err := client.Evaluate(ctx, diff, qs)
	if err != nil {
		if isTokenLimit(err) && len(rules) > 1 {
			// 拆出 description 最长的单独问（最小上下文：规则说明+匹配文件段）
			i := longestRule(rules)
			head := rules[i]
			rest := append(append([]config.Rule{}, rules[:i]...), rules[i+1:]...)
			single, err1 := askRules(ctx, client, narrowDiff(diff, head), []config.Rule{head})
			if err1 != nil {
				return nil, err1
			}
			restRes, err2 := askRules(ctx, client, diff, rest)
			if err2 != nil {
				return nil, err2
			}
			return append(single, restRes...), nil
		}
		return nil, err
	}
	out := make([]RuleResult, 0, len(rules))
	for _, r := range rules {
		a := answers[r.ID]
		out = append(out, RuleResult{Rule: r, Pass: a.Noul >= 0.5, Prob: a.Noul})
	}
	return out, nil
}

// ruleNoul 把一条规则转成 noul。带 files 限定时在 instructions 里声明"只评估匹配 glob 的文件"——
// 因为复用的是全量 diff 上下文，模型需知道该规则只管哪部分文件。
func ruleNoul(r config.Rule) typesafe.Question {
	pass := r.Pass
	if pass == "" {
		pass = "the diff satisfies this rule"
	}
	fail := r.Fail
	if fail == "" {
		fail = "the diff violates this rule"
	}
	text := r.Text
	if r.Files != "" {
		text = fmt.Sprintf("%s\n\nScope: this rule applies ONLY to files matching %q. Ignore all other files in the diff when judging.", text, r.Files)
	}
	return typesafe.Noul(text, map[string]string{"true": pass, "false": fail})
}

// longestRule 返回 text 最长的规则下标。
func longestRule(rules []config.Rule) int {
	best, bl := 0, -1
	for i, r := range rules {
		if len(r.Text) > bl {
			best, bl = i, len(r.Text)
		}
	}
	return best
}

// narrowDiff 从完整 diff 提取匹配 rule.Files 的文件段；无匹配返回原 diff。
func narrowDiff(diff string, r config.Rule) string {
	if r.Files == "" {
		return diff
	}
	var segs []string
	var cur strings.Builder
	keep := false
	flush := func() {
		if keep && cur.Len() > 0 {
			segs = append(segs, cur.String())
		}
		cur.Reset()
	}
	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "diff --git ") {
			flush()
			parts := strings.Fields(line)
			keep = len(parts) >= 4 && globMatch(r.Files, strings.TrimPrefix(parts[3], "b/"))
		}
		cur.WriteString(line + "\n")
	}
	flush()
	if len(segs) == 0 {
		return diff
	}
	return strings.Join(segs, "\n")
}

// isTokenLimit 判断是否为上下文超限错误（400 max_tokens_exceeded）。
func isTokenLimit(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "max_tokens") ||
		strings.Contains(err.Error(), "tokens_exceeded") ||
		strings.Contains(err.Error(), "context"))
}

// CheckCommitRules 检查 commit message 各部分规则。on=subject|body|prefix|all。
// prefix 规则查的是用户 -m 自带的前缀（若用户没带则不查该规则）。
func CheckCommitRules(ctx context.Context, client *typesafe.Client, subject, body, userPrefix string, rules []config.Rule) ([]RuleResult, error) {
	var out []RuleResult
	qs := map[string]typesafe.Question{}
	var applicable []config.Rule
	for _, r := range rules {
		var target string
		switch r.On {
		case "subject", "":
			target = subject
		case "body":
			target = body
		case "prefix":
			target = userPrefix
			if target == "" {
				out = append(out, RuleResult{Rule: r, Pass: true, Skip: true})
				continue
			}
		case "all":
			target = subject + "\n" + body
		}
		if r.ID == "" {
			r.ID = r.Text
		}
		if strings.TrimSpace(target) == "" {
			out = append(out, RuleResult{Rule: r, Pass: true, Skip: true})
			continue
		}
		qs[r.ID] = typesafe.Noul(
			fmt.Sprintf("Given this commit-message %s text, check the rule. %s", orEmpty(r.On, "subject"), r.Text),
			map[string]string{"true": orStr(r.Pass, "the text satisfies the rule"), "false": orStr(r.Fail, "the text violates the rule")})
		applicable = append(applicable, r)
	}
	if len(applicable) == 0 {
		return out, nil
	}
	answers, _, err := client.Evaluate(ctx, commitMsgState(subject, body, userPrefix), qs)
	if err != nil {
		return nil, err
	}
	for _, r := range applicable {
		a := answers[r.ID]
		out = append(out, RuleResult{Rule: r, Pass: a.Noul >= 0.5, Prob: a.Noul})
	}
	return out, nil
}

// commitMsgState 把 commit message 各部分组装成 state。
func commitMsgState(subject, body, prefix string) string {
	var b strings.Builder
	if prefix != "" {
		b.WriteString("prefix: " + prefix + "\n")
	}
	b.WriteString("subject: " + subject + "\n")
	if body != "" {
		b.WriteString("body:\n" + body)
	}
	return b.String()
}

// orEmpty 返回 s 或空时的默认值。
func orEmpty(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// orStr 返回 s 或空时的默认值。
func orStr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
