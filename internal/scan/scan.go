/*
 * Copyright (C) 2026 codesafe contributors
 * SPDX-License-Identifier: AGPL-3.0-or-later
 * See COPYING in the project root for the full license.
 */
// scan 包：并发扇出逐文件判断并按等权总分排序聚合；配置/文档文件只跑格式维度。
package scan

import (
	"context"
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
	content string // 文件内容，仅扫描期间使用
}

// Options 控制扫描行为。
type Options struct {
	Concurrency int     // 并发请求数
	RPS         float64 // 每秒请求上限（全局限速）
	DryRun      bool    // 只列文件不发请求
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
func (s *Scanner) Run(ctx context.Context, root string) ([]FileResult, error) {
	files, err := ListFiles(root)
	if err != nil {
		return nil, err
	}
	if s.opts.DryRun {
		return s.dryRun(root, files), nil
	}
	return s.scanAll(ctx, root, files), nil
}

// classify 读取并分类一个文件，填充 Skipped/Kind/content；跳过时 content 为空。
func classify(root, rel string, r *FileResult) {
	r.Path = rel
	if IsSecret(rel) {
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
	r.Kind = Classify(rel)
	r.content = content
}

// dryRun 对每个文件做同样的过滤和分类，但不发请求。
func (s *Scanner) dryRun(root string, files []string) []FileResult {
	results := make([]FileResult, 0, len(files))
	for _, rel := range files {
		var r FileResult
		classify(root, rel, &r)
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
		classify(root, rel, r)
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
			probs, err := s.client.Evaluate(ctx, state, questions)
			if err != nil {
				r.Err = err
				return
			}
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
