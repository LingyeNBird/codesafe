/*
 * Copyright (C) 2026 codesafe contributors
 * SPDX-License-Identifier: AGPL-3.0-or-later
 * See COPYING in the project root for the full license.
 */
// typesafe 包是 TypeSafe System One HTTP API 的客户端：构造 questions、发送请求、解析按问题 id 返回的概率答案。
package typesafe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"codesafe/internal/cache"
)

const endpoint = "https://api.typesafe.ai/v1/systemone"

// Client 调用 TypeSafe evaluation 端点。
type Client struct {
	apiKey  string
	model   string
	http    *http.Client
	retries int
	// Cache 开关响应缓存（bbolt，按请求 payload 的 SHA-256）；默认开。
	Cache bool
	// Refresh 强制跳过缓存重判（--refresh）。
	Refresh bool
}

// NewClient 返回使用给定 API key 的客户端，model 传空时使用 jev-latest。默认开缓存。
func NewClient(apiKey, model string) *Client {
	if model == "" {
		model = "jev-latest"
	}
	return &Client{
		apiKey:  apiKey,
		model:   model,
		http:    &http.Client{Timeout: 120 * time.Second},
		retries: 4,
		Cache:   true,
	}
}

// Question 是单个问题（noul 或 choice）。
type Question struct {
	Type         string            `json:"type"`
	Instructions string            `json:"instructions"`
	Criteria     map[string]string `json:"criteria,omitempty"`
}

// Noul 构造一个 yes/no 问题，criteria 描述 true/false 各自的含义。
func Noul(instructions string, criteria map[string]string) Question {
	return Question{Type: "noul", Instructions: instructions, Criteria: criteria}
}

// Choice 构造一个选项问题，criteria 为 选项名 -> 说明。
func Choice(instructions string, criteria map[string]string) Question {
	return Question{Type: "choice", Instructions: instructions, Criteria: criteria}
}

// request 是 POST /v1/systemone 的请求体。
type request struct {
	State     any                 `json:"state"`
	Model     string              `json:"model"`
	Questions map[string]Question `json:"questions"`
}

// Response 是 evaluation 端点的响应；每个问题的答案按其 id 键控。
type Response struct {
	Model   string                     `json:"model"`
	Answers map[string]json.RawMessage `json:"answers"`
	Usage   Usage                      `json:"usage"`
}

// Usage 是一次请求的 token 用量。
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// apiError 是端点返回的错误体。
type apiError struct {
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

// rateLimitError 表示 429，携带 Retry-After 秒数（可为 0）。
type rateLimitError struct{ retryAfter time.Duration }

func (e *rateLimitError) Error() string { return "rate limited" }

// Answer 是一个问题的原始答案（按类型字段填充）。
type Answer struct {
	// noul
	Noul float64
	// choice
	Choice        string
	Probabilities map[string]float64
	Confidence    float64
}

// Evaluate 对同一 state 求值所有 questions，返回每个问题 id 的 Answer 及 token 用量。
func (c *Client) Evaluate(ctx context.Context, state any, questions map[string]Question) (map[string]Answer, Usage, error) {
	body := request{State: state, Model: c.model, Questions: questions}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, Usage{}, err
	}
	// 缓存键 = 真实发往上游的请求字节 SHA-256；命中且未过期则直接复用。
	key := cache.Key(payload)
	if c.Cache && !c.Refresh {
		if e, ok := cache.Get(key); ok {
			var out Response
			if json.Unmarshal(e.Answers, &out.Answers) == nil {
				if ans, err := decodeAnswers(out.Answers); err == nil {
					var u Usage
					json.Unmarshal(e.Usage, &u)
					return ans, u, nil
				}
			}
		}
	}
	var lastErr error
	for attempt := 0; attempt <= c.retries; attempt++ {
		if attempt > 0 {
			if !sleep(ctx, backoff(attempt, lastErr)) {
				return nil, Usage{}, ctx.Err()
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
		if err != nil {
			return nil, Usage{}, err
		}
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		raw, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}

		if resp.StatusCode == http.StatusOK {
			var out Response
			if err := json.Unmarshal(raw, &out); err != nil {
				return nil, Usage{}, fmt.Errorf("解析响应失败: %w", err)
			}
			ans, err := decodeAnswers(out.Answers)
			if err != nil {
				return nil, Usage{}, err
			}
			if c.Cache {
				rawAns, _ := json.Marshal(out.Answers)
				rawUse, _ := json.Marshal(out.Usage)
				cache.Set(key, cache.Entry{Answers: rawAns, Usage: rawUse})
			}
			return ans, out.Usage, nil
		}
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			lastErr = retryable(resp, raw)
			continue
		}
		return nil, Usage{}, fatal(resp, raw)
	}
	return nil, Usage{}, fmt.Errorf("请求失败（重试 %d 次后）: %w", c.retries, lastErr)
}

// decodeAnswers 把 answers map 里每个答案按字段解出（noul 概率、choice 选中项/概率/置信度）。
func decodeAnswers(answers map[string]json.RawMessage) (map[string]Answer, error) {
	out := make(map[string]Answer, len(answers))
	for id, raw := range answers {
		var a struct {
			Noul          *float64           `json:"noul"`
			Choice        string             `json:"choice"`
			Probabilities map[string]float64 `json:"probabilities"`
			Confidence    float64            `json:"confidence"`
		}
		if err := json.Unmarshal(raw, &a); err != nil {
			return nil, fmt.Errorf("解析问题 %q 的答案失败: %w", id, err)
		}
		an := Answer{Choice: a.Choice, Probabilities: a.Probabilities, Confidence: a.Confidence}
		if a.Noul != nil {
			an.Noul = *a.Noul
		}
		out[id] = an
	}
	return out, nil
}

// retryable 把可重试的响应转成 error，429 时解析 Retry-After。
func retryable(resp *http.Response, raw []byte) error {
	if resp.StatusCode == http.StatusTooManyRequests {
		d := 0.0
		if s := resp.Header.Get("Retry-After"); s != "" {
			fmt.Sscanf(s, "%f", &d)
		}
		return &rateLimitError{retryAfter: time.Duration(d * float64(time.Second))}
	}
	return fmt.Errorf("上游错误 %d: %s", resp.StatusCode, truncate(raw))
}

// fatal 把不可重试的响应转成带上游消息的错误。
func fatal(resp *http.Response, raw []byte) error {
	var ae apiError
	if json.Unmarshal(raw, &ae) == nil && ae.Error.Message != "" {
		return fmt.Errorf("API 错误 %d: %s", resp.StatusCode, ae.Error.Message)
	}
	return fmt.Errorf("API 错误 %d: %s", resp.StatusCode, truncate(raw))
}

// backoff 计算第 n 次重试的等待时长；429 优先用 Retry-After。
func backoff(attempt int, lastErr error) time.Duration {
	var rl *rateLimitError
	if asRateLimit(lastErr, &rl) && rl.retryAfter > 0 {
		return rl.retryAfter
	}
	return time.Duration(attempt*attempt) * time.Second
}

// asRateLimit 判断 err 是否为 rateLimitError。
func asRateLimit(err error, out **rateLimitError) bool {
	for err != nil {
		if rl, ok := err.(*rateLimitError); ok {
			*out = rl
			return true
		}
		return false
	}
	return false
}

// sleep 等待 d 或 ctx 取消；返回 false 表示 ctx 已取消。
func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// truncate 截断错误响应体用于日志。
func truncate(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 300 {
		return s[:300] + "…"
	}
	return s
}
