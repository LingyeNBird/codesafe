/*
 * Copyright (C) 2026 codesafe contributors
 * SPDX-License-Identifier: AGPL-3.0-or-later
 * See COPYING in the project root for the full license.
 */
// render 包把扫描结果渲染成等宽终端行：截断路径 + 每维度"标签:百分比 + 圆环符号"，按维度着色，支持 i18n 标签与 Nerd Font / emoji 两套符号。
package render

import (
	"fmt"
	"strings"

	"codesafe/internal/scan"
)

// ANSI 颜色。
const (
	reset = "\x1b[0m"
	dim   = "\x1b[2m"
)

// 每个维度一个颜色（256 色索引），保证列与列之间可区分。
var dimColor = map[string]string{
	"bug":        "\x1b[38;5;196m", // 红
	"security":   "\x1b[38;5;208m", // 橙
	"data_ops":   "\x1b[38;5;220m", // 黄
	"dependency": "\x1b[38;5;81m",  // 青
	"logic":      "\x1b[38;5;141m", // 紫
	"format":     "\x1b[38;5;70m",  // 绿
}

// i18n：lang -> key -> 文案。新增语言只需加一张表。
var i18n = map[string]map[string]string{
	"en": {
		"bug": "bug", "security": "security", "data_ops": "data",
		"dependency": "deps", "logic": "logic", "format": "format",
		"skip": "skipped", "error": "error",
		"dryrun_header": "Would scan %d files (skips marked):",
		"summary":       "Summary: %d requests, %d errors, %d skipped | %d tokens in | total %v | avg %.1fs/req",
	},
	"zh": {
		"bug": "缺陷", "security": "安全", "data_ops": "数据",
		"dependency": "依赖", "logic": "逻辑", "format": "格式",
		"skip": "跳过", "error": "错误",
		"dryrun_header": "将扫描 %d 个文件（跳过项标注）:",
		"summary":       "汇总: %d 次请求, %d 个失败, %d 个跳过 | 共 %d 输入 token | 总耗时 %v | 平均 %.1fs/次",
	},
}

// T 取 lang 下 key 的文案；缺省回退 en 再回退 key 本身。供包外（如 cli）复用。
func T(lang, key string) string {
	if m, ok := i18n[lang]; ok {
		if s, ok := m[key]; ok {
			return s
		}
	}
	if s, ok := i18n["en"][key]; ok {
		return s
	}
	return key
}

// 两套符号集：nerd 用 Nerd Font 圆环，emoji 用 Unicode 圆/方块表达档位。
var (
	// nerdGlyphs: U+F10D3 空圆 + U+F0A9E..U+F0AA5 填充 1/8..满
	nerdGlyphs = []rune{
		'\U000F10D3',
		'\U000F0A9E', '\U000F0A9F', '\U000F0AA0', '\U000F0AA1',
		'\U000F0AA2', '\U000F0AA3', '\U000F0AA4', '\U000F0AA5',
	}
	// emojiGlyphs: 用色相热度表达档位——绿=低，黄=中，橙=偏高，红=高。
	emojiGlyphs = []rune{
		'⚪',      // 0%   空
		'🟢', '🟢', // ~12% ~25% 低
		'🟡', '🟡', // ~37% 50%  中
		'🟠', '🟠', // ~62% ~75% 偏高
		'🔴', '🔴', // ~87% 100% 高
	}
)

// glyph 把概率 0..1 映射到所选符号集的档位。
func glyph(p float64, mode string) rune {
	set := nerdGlyphs
	if mode == "emoji" {
		set = emojiGlyphs
	}
	if p <= 0 {
		return set[0]
	}
	if p >= 1 {
		return set[8]
	}
	idx := int(p*8 + 0.5)
	if idx < 1 {
		idx = 1
	}
	if idx > 8 {
		idx = 8
	}
	return set[idx]
}

// Row 渲染单行结果：路径左对齐占 width 列，其后每维一段；配置/文档文件只显示格式列。
func Row(r scan.FileResult, width int, lang, glyphMode string) string {
	if r.Skipped {
		return fmt.Sprintf("%s%s %s: %s%s", dim, padPath(r.Path, width), T(lang, "skip"), r.SkipWhy, reset)
	}
	if r.Err != nil {
		return fmt.Sprintf("%s%s %s: %s%s", dim, padPath(r.Path, width), T(lang, "error"), r.Err, reset)
	}
	var b strings.Builder
	b.WriteString(padPath(r.Path, width))
	if r.Kind != scan.KindCode {
		p, ok := r.Probs[scan.FormatDim.ID]
		if !ok {
			return b.String()
		}
		pct := int(p*100 + 0.5)
		fmt.Fprintf(&b, "  %s%s:%3d%% %c%s", dimColor[scan.FormatDim.ID], T(lang, scan.FormatDim.ID), pct, glyph(p, glyphMode), reset)
		return b.String()
	}
	for _, d := range scan.Dims {
		p, ok := r.Probs[d.ID]
		if !ok {
			continue
		}
		pct := int(p*100 + 0.5)
		fmt.Fprintf(&b, "  %s%s:%3d%% %c%s", dimColor[d.ID], T(lang, d.ID), pct, glyph(p, glyphMode), reset)
	}
	return b.String()
}

// padPath 把路径截断/补齐到固定显示宽度；超长时前缀 "…"。
func padPath(path string, width int) string {
	runes := []rune(path)
	if len(runes) > width {
		return "…" + string(runes[len(runes)-width+1:])
	}
	return path + strings.Repeat(" ", width-len(runes))
}

// Table 渲染整个结果集；width 为路径列宽，lang/glyphMode 见 config。
func Table(results []scan.FileResult, width int, lang, glyphMode string) string {
	var b strings.Builder
	for _, r := range results {
		b.WriteString(Row(r, width, lang, glyphMode))
		b.WriteByte('\n')
	}
	return b.String()
}

// Summary 渲染末尾聚合统计行。
func Summary(s scan.Stats, lang string) string {
	var avg float64
	if s.Requests > 0 {
		avg = s.SumReqTime.Seconds() / float64(s.Requests)
	}
	return dim + fmt.Sprintf(T(lang, "summary"),
		s.Requests, s.Errors, s.Skipped, s.TokensIn, s.TotalTime, avg) + reset + "\n"
}
