/*
 * Copyright (C) 2026 codesafe contributors
 * SPDX-License-Identifier: AGPL-3.0-or-later
 * See COPYING in the project root for the full license.
 */
// render 包把 commit 分类结果渲染成终端文本。
package render

import (
	"fmt"
	"strings"

	"codesafe/internal/scan"
)

const (
	reset  = "\x1b[0m"
	dim    = "\x1b[2m"
	green  = "\x1b[32m"
	yellow = "\x1b[33m"
	red    = "\x1b[31m"
	cyan   = "\x1b[36m"
	bold   = "\x1b[1m"
)

func confColor(c float64) string {
	switch {
	case c >= 0.8:
		return green
	case c >= 0.5:
		return yellow
	default:
		return red
	}
}

// Result 渲染 commit 分类建议。detail=false 时只输出一行 type(scope)!（可直接进 git commit -m）；
// detail=true 时输出百分比详情 + token/费用统计。
func Result(r scan.Result, lang string, detail bool) string {
	if !detail {
		return r.Suggestion() + "\n"
	}
	return detailBlock(r, lang)
}

// detailBlock 渲染详细结果：type/scope/breaking 百分比 + 建议 + 用量统计。
func detailBlock(r scan.Result, lang string) string {
	var b strings.Builder
	zh := lang == "zh"

	// type
	if zh {
		b.WriteString(bold + "建议的 commit 类型：" + reset + "\n")
	} else {
		b.WriteString(bold + "Suggested commit type:" + reset + "\n")
	}
	fmt.Fprintf(&b, "  %s%s%s%s  %s(%.0f%%)\n",
		confColor(r.TypeConf), bold, r.Type, reset, dim, r.TypeConf*100)
	b.WriteString(altLabel(zh) + formatCands(r.TypeTop) + "\n\n")

	// scope
	if zh {
		b.WriteString(bold + "建议的 scope：" + reset + "\n")
	} else {
		b.WriteString(bold + "Suggested scope:" + reset + "\n")
	}
	fmt.Fprintf(&b, "  %s%s%s%s  %s(%.0f%%)\n",
		confColor(r.ScopeConf), bold, r.Scope, reset, dim, r.ScopeConf*100)
	b.WriteString(altLabel(zh) + formatCands(r.ScopeTop) + "\n\n")

	// breaking
	if zh {
		b.WriteString(bold + "破坏兼容：" + reset)
	} else {
		b.WriteString(bold + "Breaking:" + reset + " ")
	}
	if r.Breaking {
		fmt.Fprintf(&b, "%s%sYES%s  %s(%.0f%%)\n", red, bold, reset, dim, r.BreakingP*100)
	} else {
		fmt.Fprintf(&b, "%sno%s  %s(%.0f%%)\n", green, reset, dim, r.BreakingP*100)
	}
	b.WriteString("\n")

	// final suggestion
	if zh {
		fmt.Fprintf(&b, "%s建议:%s %s%s%s\n", cyan, reset, bold, r.Suggestion(), reset)
	} else {
		fmt.Fprintf(&b, "%sSuggestion:%s %s%s%s\n", cyan, reset, bold, r.Suggestion(), reset)
	}

	// usage stats: tokens + 估算费用（$0.042/Mtok 输入，输出免费）
	cost := float64(r.Usage.InputTokens) / 1e6 * 0.042
	if zh {
		fmt.Fprintf(&b, "%s用量: %d 输入 + %d 输出 tok，约 $%.4f%s\n", dim, r.Usage.InputTokens, r.Usage.OutputTokens, cost, reset)
	} else {
		fmt.Fprintf(&b, "%susage: %d in + %d out tok, ~$%.4f%s\n", dim, r.Usage.InputTokens, r.Usage.OutputTokens, cost, reset)
	}
	return b.String()
}

func altLabel(zh bool) string {
	if zh {
		return "  备选: "
	}
	return "  alternatives: "
}

// formatCands 把候选格式化为 "name:NN%"。
func formatCands(cands []scan.Cand) string {
	parts := make([]string, len(cands))
	for i, c := range cands {
		parts[i] = fmt.Sprintf("%s:%.0f%%", c.Name, c.Prob*100)
	}
	return dim + strings.Join(parts, "  ") + reset
}
