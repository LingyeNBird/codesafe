/*
 * Copyright (C) 2026 codesafe contributors
 * SPDX-License-Identifier: AGPL-3.0-or-later
 * See COPYING in the project root for the full license.
 */
// scan 包：并发扇出逐文件判断并按等权总分排序聚合；配置/文档文件只跑格式维度。
package scan

import (
	"context"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"codesafe/internal/typesafe"
)

// FileResult 是单个文件的扫描结果。
type FileResult struct {
	Path    string             // 相对 root 的 POSIX 路径
	Kind    FileKind           // 决定跑哪组问题
	Probs   map[string]float64 // dim id -> 0..1
	Total   float64            // 等权加总
	Skipped bool               // 二进制/超限/secret/读取失败
	SkipWhy string
	Err     error
	// 统计字段（仅成功请求后填充）
	TokensIn int           // 该文件请求的输入 token 数
	ReqTime  time.Duration // 该文件请求的耗时
	content  string        // 文件内容，仅扫描期间使用
}

// Options 控制扫描行为。
type Options struct {
	Concurrency int               // 并发请求数
	RPS         float64           // 每秒请求上限（全局限速）
	DryRun      bool              // 只列文件不发请求
	Overrides   map[string]string // 绝对路径 -> "code"|"config"|"doc"，覆盖后缀分类
	Force       bool              // 为 true 时跳过 secret 过滤（配合 --files 强扫）
}

// Scanner 执行扫描。
type Scanner struct {
	client *typesafe.Client
	opts   Options
}

// NewScanner 构造扫描器。
func NewScanner(client *typesafe.Client, opts Options) *Scanner {
	if opts.Concurrency <= 0 {
		opts.Concurrency = 16
	}
	if opts.RPS <= 0 {
		opts.RPS = 20
	}
	return &Scanner{client: client, opts: opts}
}

// Run 枚举 root 下的文件并返回按总分降序的结果。
// Subdir 限定扫描子树；ForceList 非空时强制只扫给定文件（绕过 git 跟踪与 secret 过滤）。
func (s *Scanner) Run(ctx context.Context, root, subdir string, forceFiles []string) ([]FileResult, error) {
	files, err := ListFiles(root, subdir, forceFiles)
	if err != nil {
		return nil, err
	}
	if s.opts.DryRun {
		return s.dryRun(root, files), nil
	}
	return s.scanAll(ctx, root, files), nil
}

// classify 读取并分类一个文件，填充 Skipped/Kind/content；跳过时 content 为空。
// 分类顺序：secret 跳过（ForceList 时除外）→ 读取/二进制过滤 → override 覆盖 → 后缀分类。
func (s *Scanner) classify(root, rel string, r *FileResult) {
	r.Path = rel
	if !s.opts.Force && IsSecret(rel) {
		r.Skipped, r.SkipWhy = true, "凭据文件"
		return
	}
	content, ok, err := ReadFile(root, rel)
	if err != nil {
		r.Skipped, r.SkipWhy = true, "读取失败: "+err.Error()
		return
	}
	if !ok {
		r.Skipped, r.SkipWhy = true, "二进制或过大"
		return
	}
	if kind, hit := s.override(root, rel); hit {
		r.Kind = kind
	} else {
		r.Kind = Classify(rel)
	}
	r.content = content
}

// override 查绝对路径是否被 --config override 指定了扫描方式。
func (s *Scanner) override(root, rel string) (FileKind, bool) {
	if s.opts.Overrides == nil {
		return 0, false
	}
	abs := filepath.Join(root, filepath.FromSlash(rel))
	kind, ok := s.opts.Overrides[abs]
	if !ok {
		return 0, false
	}
	switch kind {
	case "config":
		return KindConfig, true
	case "doc":
		return KindDoc, true
	}
	return KindCode, true
}

// dryRun 对每个文件做同样的过滤和分类，但不发请求；Probs 填 0 占位让列渲染出来。
func (s *Scanner) dryRun(root string, files []string) []FileResult {
	results := make([]FileResult, 0, len(files))
	for _, rel := range files {
		var r FileResult
		s.classify(root, rel, &r)
		if !r.Skipped {
			r.Probs = map[string]float64{}
			if r.Kind == KindCode {
				for _, d := range Dims {
					r.Probs[d.ID] = 0
				}
			} else {
				r.Probs[FormatDim.ID] = 0
			}
		}
		results = append(results, r)
	}
	sortResults(results)
	return results
}

// scanAll 并发对每个文件调用 Evaluate；rps 限速器控制全局速率。
func (s *Scanner) scanAll(ctx context.Context, root string, files []string) []FileResult {
	results := make([]FileResult, len(files))
	sem := make(chan struct{}, s.opts.Concurrency)
	tick := time.NewTicker(time.Duration(float64(time.Second) / s.opts.RPS))
	defer tick.Stop()
	codeQuestions := Questions()

	var wg sync.WaitGroup
	for i, rel := range files {
		r := &results[i]
		s.classify(root, rel, r)
		if r.Skipped {
			continue
		}
		questions := questionsFor(r.Kind, codeQuestions)
		wg.Add(1)
		sem <- struct{}{}
		go func(r *FileResult, questions map[string]typesafe.Question) {
			defer wg.Done()
			defer func() { <-sem }()
			select {
			case <-ctx.Done():
				r.Err = ctx.Err()
				return
			case <-tick.C:
			}
			state := map[string]string{"path": r.Path, "content": r.content}
			start := time.Now()
			probs, usage, err := s.client.Evaluate(ctx, state, questions)
			r.ReqTime = time.Since(start)
			if err != nil {
				r.Err = err
				return
			}
			r.TokensIn = usage.InputTokens
			r.Probs = probs
			r.Total = total(r.Kind, probs)
		}(r, questions)
	}
	wg.Wait()
	sortResults(results)
	return results
}

// questionsFor 返回该文件类型要问的问题集。
func questionsFor(kind FileKind, codeQuestions map[string]typesafe.Question) map[string]typesafe.Question {
	if kind == KindCode {
		return codeQuestions
	}
	return map[string]typesafe.Question{FormatDim.ID: FormatQuestion(kind)}
}

// total 按维度权重加总各维概率；非代码文件只有 format 一维。
func total(kind FileKind, probs map[string]float64) float64 {
	if kind != KindCode {
		return probs[FormatDim.ID] * FormatDim.Weight
	}
	var t float64
	for _, d := range Dims {
		t += probs[d.ID] * d.Weight
	}
	return t
}

// sortResults 按总分降序、路径升序排序；跳过的文件排最后。
func sortResults(rs []FileResult) {
	sort.SliceStable(rs, func(i, j int) bool {
		a, b := rs[i], rs[j]
		if a.Skipped != b.Skipped {
			return b.Skipped
		}
		if a.Total != b.Total {
			return a.Total > b.Total
		}
		return a.Path < b.Path
	})
}

// Stats 是一次扫描的聚合统计。
type Stats struct {
	Requests   int           // 成功请求数（含重试后成功的文件）
	Errors     int           // 请求失败的文件数
	Skipped    int           // 跳过的文件数
	TokensIn   int           // 输入 token 总量
	TokensOut  int           // 输出 token 总量（按 usage 累加，通常为 0 成本）
	TotalTime  time.Duration // 端到端耗时（由调用方计时）
	SumReqTime time.Duration // 各文件请求耗时之和（用于看平均）
}

// Summarize 聚合所有文件结果的统计字段；totalTime 为整次扫描的端到端耗时。
func Summarize(rs []FileResult, totalTime time.Duration) Stats {
	var s Stats
	s.TotalTime = totalTime
	for _, r := range rs {
		switch {
		case r.Skipped:
			s.Skipped++
		case r.Err != nil:
			s.Errors++
		default:
			s.Requests++
			s.TokensIn += r.TokensIn
			s.SumReqTime += r.ReqTime
		}
	}
	return s
}
