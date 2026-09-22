/*
 * Copyright (C) 2026 codesafe contributors
 * SPDX-License-Identifier: AGPL-3.0-or-later
 * See COPYING in the project root for the full license.
 */
// scan 包负责 commit 分类：取 diff、调 TypeSafe、返回 type+scope+breaking 建议。
package scan

import (
	"context"

	"codesafe/internal/typesafe"
)

// Result 是一次 commit 分类的结果。
type Result struct {
	Type      string         `json:"type"`
	Scope     string         `json:"scope"`
	Breaking  bool           `json:"breaking"`   // --breaking flag 显式声明（模型不再判定）
	BreakingP float64        `json:"breaking_p"` // 保留字段，恒 0
	TypeConf  float64        `json:"type_confidence"`
	ScopeConf float64        `json:"scope_confidence"`
	TypeTop   []Cand         `json:"type_top"`
	ScopeTop  []Cand         `json:"scope_top"`
	Usage     typesafe.Usage `json:"usage"`
}

// Cand 是候选项及其概率。
type Cand struct {
	Name string  `json:"name"`
	Prob float64 `json:"prob"`
}

// Type 返回带 breaking 标记的 type（如 feat!）。
func (r Result) TypeString() string {
	if r.Breaking {
		return r.Type + "!"
	}
	return r.Type
}

// Suggestion 返回组合建议 "type(scope)" / "type(scope)!" / "type!" / "type"。
// scope 为 "none" 或空时省略括号。
func (r Result) Suggestion() string {
	s := r.Type
	if r.Scope != "" && r.Scope != "none" {
		s += "(" + r.Scope + ")"
	}
	if r.Breaking {
		s += "!"
	}
	return s
}

// Classify 对一段 commit diff 做 type+scope+breaking 分类。allowNone 控制 scope 是否可选 none。
func Classify(ctx context.Context, client *typesafe.Client, diff string, scopes map[string]string, allowNone bool) (Result, error) {
	answers, usage, err := client.Evaluate(ctx, diff, Questions(scopes, allowNone))
	if err != nil {
		return Result{}, err
	}
	t := answers["type"]
	s := answers["scope"]
	return Result{
		Type:      t.Choice,
		Scope:     s.Choice,
		TypeConf:  t.Confidence,
		ScopeConf: s.Confidence,
		TypeTop:   topN(t.Probabilities, 3),
		ScopeTop:  topN(s.Probabilities, 3),
		Usage:     usage,
	}, nil
}

// topN 返回概率最高的 n 个候选。
func topN(probs map[string]float64, n int) []Cand {
	cands := make([]Cand, 0, len(probs))
	for k, v := range probs {
		cands = append(cands, Cand{Name: k, Prob: v})
	}
	for i := 1; i < len(cands); i++ {
		for j := i; j > 0 && cands[j].Prob > cands[j-1].Prob; j-- {
			cands[j], cands[j-1] = cands[j-1], cands[j]
		}
	}
	if len(cands) > n {
		cands = cands[:n]
	}
	return cands
}
