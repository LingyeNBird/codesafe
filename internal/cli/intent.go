/*
 * Copyright (C) 2026 codesafe contributors
 * SPDX-License-Identifier: AGPL-3.0-or-later
 * See COPYING in the project root for the full license.
 */
// codesafe intent：识别用户输入的意图——要不要动手改代码、想要哪几类回应（解释/方案/审查/执行）。
// 多标签 noul 判定：每个意图独立给概率，组合意图（"先解释再给方案"）能同时命中多个。
package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"strings"

	"codesafe/internal/config"
	"codesafe/internal/typesafe"
)

// IntentResult 是一次意图判定的输出：action 四选一（modify/execute/answer/unclear）+ answer 时的软意图概率。
type IntentResult struct {
	Action       string             `json:"action"`        // modify|execute|answer|unclear
	ActionP      float64            `json:"action_p"`      // 该 action 的概率
	ShouldModify bool               `json:"should_modify"` // action==modify
	Intents      map[string]float64 `json:"intents"`       // action==answer 时：answer/plan/review 概率
	Primary      string             `json:"primary"`       // 概率最高的软意图（answer 时）
}

// intentQuestions：第一阶段 action 四选一（modify/execute/answer/unclear）；
// 第二阶段：action=answer 时再判想要的答复类型（answer/plan/review，可多命中）。
// actionQuestion 第一阶段：把用户输入归到四大类之一。
// 重点是分清"要动手改东西/跑操作"和"要答复"——征询句（"是不是改成X好""你觉得""要不要"）是要答复不是改。
func actionQuestion() typesafe.Question {
	return typesafe.Choice(
		"The user wrote the latest message in a conversation with an AI coding assistant. What is the user asking the assistant to do? Choose the ONE best fit. "+
			"KEY DISTINCTION: 'modify'/'execute' mean the user wants the assistant to DO something to the project or system now. 'answer' means the user wants a REPLY — an explanation, an opinion, a plan to read, a review, or a yes/no judgement — without the assistant changing or running anything. "+
			"Phrases like 'is it better to change X', 'should we add Y', '你觉得…比较好吗', '是否要新建', '打算改哪些' are ASKING for an opinion/plan → answer, not modify. A bug report or problem statement without an explicit 'fix it' is also answer (investigate/propose, don't change).",
		map[string]string{
			"modify":  "Change the CONTENT of something — edit/write/create/delete code, files, docs, images, config. Any clear directive to modify/adjust/redo a target, however phrased.",
			"execute": "Run a command or operational action WITHOUT changing source content — git ops (commit/tag/push), build, test, run, restart, deploy, generate a one-off result.",
			"answer":  "Wants a reply only — an explanation, an opinion, a plan to read, a review/inspection, or a yes/no judgement. Includes proposing/discussing a change, asking 'should we', reporting a problem. Nothing gets changed or executed.",
			"unclear": "Cannot tell what the user wants — ambiguous, missing context, or no actionable intent.",
		})
}

// intentQuestions 第二阶段：当 action=answer 时，判用户想要哪几类答复（可多命中）。
func intentQuestions() map[string]typesafe.Question {
	noul := func(text, yes, no string) typesafe.Question {
		return typesafe.Noul(text, map[string]string{"true": yes, "false": no})
	}
	return map[string]typesafe.Question{
		"want_answer": noul(
			"The user wants an explanation or a direct answer to a question — e.g. 'what does this do', 'why is it like this', 'how does X work'.",
			"The user wants an explanation or an answer to a question.",
			"The user is not asking for an explanation or answer."),
		"want_plan": noul(
			"The user wants a plan, a design, an approach, or options to consider — e.g. 'how should we do this', 'give me a proposal', 'what's the best way', 'is it better to change X'. They want to see and discuss the approach before any change.",
			"The user wants a plan/proposal/approach to review.",
			"The user is not asking for a plan or approach."),
		"want_review": noul(
			"The user wants the assistant to review, check, or critique existing code or a change — e.g. 'is this correct', 'review this', 'any problems here', 'find the bug'. They want findings/opinions, not edits.",
			"The user wants a review, critique, or inspection of existing code.",
			"The user is not asking for a review or inspection."),
	}
}

// runIntent 判定用户输入的意图并输出。`intent "<text>"`。
func runIntent(args []string) error {
	fs := flag.NewFlagSet("codesafe intent", flag.ContinueOnError)
	var (
		dir     = fs.String("dir", ".", "git repo directory (for config lookup)")
		model   = fs.String("model", "jev-latest", "TypeSafe model ID")
		ctxStr  = fs.String("context", "", "surrounding context (e.g. recent conversation) to resolve references like 'change it'")
		hist    = multiFlag{}
		jsonOut = fs.Bool("json", false, "print full probability vector as JSON")
		refresh = fs.Bool("refresh", false, "bypass the response cache")
	)
	fs.Var(&hist, "history", "recent prior user messages, oldest first (repeatable; used to disambiguate when unclear)")
	if err := fs.Parse(reorderFlags(args)); err != nil {
		return err
	}
	text := strings.TrimSpace(strings.Join(fs.Args(), " "))
	if text == "" {
		return fmt.Errorf("intent needs the user's text, e.g. codesafe intent \"帮我看下这段代码\"")
	}
	cfg, _, _, err := setup(*dir)
	if err != nil {
		return err
	}
	if cfg.APIKey == "" {
		return config.ErrNoAPIKey
	}
	client := newClient(cfg, *model, *refresh)
	// 阶段 1：action 四选一。state = 上下文（可选）+ 用户输入。
	state := text
	if *ctxStr != "" {
		state = "Context:\n" + *ctxStr + "\n\nUser input:\n" + text
	}
	ctx := context.Background()
	q := map[string]typesafe.Question{"action": actionQuestion()}

	actAns, _, err := client.Evaluate(ctx, state, q)
	if err != nil {
		return err
	}
	action := actAns["action"]

	// unclear 且提供了 --history：带历史重判一次（只一次，仍 unclear 就停）
	if action.Choice == "unclear" && len(hist) > 0 {
		h := strings.Join([]string(hist), "\n")
		state = "Recent conversation (oldest first):\n" + h + "\n\nUser input (judge this one):\n" + text
		actAns, _, err = client.Evaluate(ctx, state, q)
		if err != nil {
			return err
		}
		action = actAns["action"]
	}

	res := IntentResult{
		Action:  action.Choice,
		ActionP: action.Confidence,
		Intents: map[string]float64{},
	}
	res.ShouldModify = res.Action == "modify"
	res.Primary = res.Action

	// 阶段 2：action==answer 时再判想要的答复类型（多标签）
	if res.Action == "answer" {
		ans2, _, err := client.Evaluate(ctx, state, intentQuestions())
		if err != nil {
			return err
		}
		best, bp := "", -1.0
		for _, id := range []string{"want_answer", "want_plan", "want_review"} {
			p := ans2[id].Noul
			label := strings.TrimPrefix(id, "want_")
			res.Intents[label] = p
			if p > bp {
				best, bp = label, p
			}
		}
		if best != "" {
			res.Primary = "answer:" + best
		}
	}

	if *jsonOut {
		b, _ := json.MarshalIndent(res, "", "  ")
		fmt.Println(string(b))
		return nil
	}
	printIntent(res, cfg.Lang)
	return nil
}

// printIntent 输出人类可读摘要：action 类别 + answer 时命中的答复意图。
func printIntent(r IntentResult, lang string) {
	zh := lang == "zh"
	label := map[string][2]string{
		"modify":  {"改代码/文件", "modify"},
		"execute": {"执行操作", "execute"},
		"answer":  {"不动手，要答复", "answer"},
		"unclear": {"无法判断", "unclear"},
	}
	l := label[r.Action]
	if zh {
		fmt.Printf("%s%s%s  %s(%.0f%%)%s\n", green(), l[0], reset(), dim(), r.ActionP*100, reset())
	} else {
		fmt.Printf("%s%s%s  %s(%.0f%%)%s\n", green(), l[1], reset(), dim(), r.ActionP*100, reset())
	}
	// answer 时列出命中的答复意图（≥0.5）
	var hits []string
	for k, p := range r.Intents {
		if p >= 0.5 {
			hits = append(hits, k)
		}
	}
	if len(hits) > 0 {
		if zh {
			fmt.Printf("意图: %s%s%s\n", "\x1b[1m", strings.Join(hits, " + "), reset())
		} else {
			fmt.Printf("intents: %s%s%s\n", "\x1b[1m", strings.Join(hits, " + "), reset())
		}
	}
}
