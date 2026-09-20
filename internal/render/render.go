/*
 * Copyright (C) 2026 codesafe contributors
 * SPDX-License-Identifier: AGPL-3.0-or-later
 * See COPYING in the project root for the full license.
 */
// render 包把扫描结果渲染成等宽终端行：截断路径 + 每维度"标签:百分比 + Nerd Font 圆环"，按维度着色。
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

// Nerd Font md-circle-slice：U+F10D3 空圆，U+F0A9E..U+F0AA5 填充从 1/8 到满。
var sliceGlyphs = []rune{
	'\U000F10D3', // 0%   空圆
	'\U000F0A9E', // ~12% slice_1
	'\U000F0A9F', // ~25% slice_2
	'\U000F0AA0', // ~37% slice_3
	'\U000F0AA1', // 50%  slice_4
	'\U000F0AA2', // ~62% slice_5
	'\U000F0AA3', // ~75% slice_6
	'\U000F0AA4', // ~87% slice_7
	'\U000F0AA5', // 100% slice_8 满
}

// glyph 把概率 0..1 映射到八档圆环。
func glyph(p float64) rune {
	if p <= 0 {
		return sliceGlyphs[0]
	}
	if p >= 1 {
		return sliceGlyphs[8]
	}
	idx := int(p*8 + 0.5) // 四舍五入到最近档
	if idx < 1 {
		idx = 1
	}
	if idx > 8 {
		idx = 8
	}
	return sliceGlyphs[idx]
}

// Row 渲染单行结果：路径左对齐占 width 列，其后每维一段；配置/文档文件只显示格式列。
func Row(r scan.FileResult, width int) string {
	if r.Skipped {
		return fmt.Sprintf("%s%s 跳过: %s%s", dim, padPath(r.Path, width), r.SkipWhy, reset)
	}
	if r.Err != nil {
		return fmt.Sprintf("%s%s 错误: %s%s", dim, padPath(r.Path, width), r.Err, reset)
	}
	var b strings.Builder
	b.WriteString(padPath(r.Path, width))
	if r.Kind != scan.KindCode {
		p, ok := r.Probs[scan.FormatDim.ID]
		if !ok {
			return b.String()
		}
		pct := int(p*100 + 0.5)
		fmt.Fprintf(&b, "  %s%s:%3d%% %c%s", dimColor[scan.FormatDim.ID], scan.FormatDim.Label, pct, glyph(p), reset)
		return b.String()
	}
	for _, d := range scan.Dims {
		p, ok := r.Probs[d.ID]
		if !ok {
			continue
		}
		pct := int(p*100 + 0.5)
		color := dimColor[d.ID]
		fmt.Fprintf(&b, "  %s%s:%3d%% %c%s", color, d.Label, pct, glyph(p), reset)
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

// Table 渲染整个结果集；width 为路径列宽。
func Table(results []scan.FileResult, width int) string {
	var b strings.Builder
	for _, r := range results {
		b.WriteString(Row(r, width))
		b.WriteByte('\n')
	}
	return b.String()
}
