/*
 * Copyright (C) 2026 codesafe contributors
 * SPDX-License-Identifier: AGPL-3.0-or-later
 * See COPYING in the project root for the full license.
 */
// intent.ts — two-stage user-intent classification: stage 1 picks one of four
// actions (modify/execute/answer/unclear); stage 2, when action=answer, scores
// which response kinds are wanted (answer/plan/review, multi-label).

import type { Client } from "./typesafe";
import { choice, noul } from "./typesafe";

/** Outcome of an intent classification. */
export interface IntentResult {
	action: string; // modify | execute | answer | unclear
	actionP: number; // confidence on the action choice
	shouldModify: boolean;
	intents: Record<string, number>; // answer/plan/review probabilities (answer only)
	primary: string; // top intent label, or the action itself
}

/** Stage-1 action choice: which of the four buckets the user's message fits. */
function actionQuestion() {
	return choice(
		"The user wrote the latest message in a conversation with an AI coding assistant. What is the user asking the assistant to do? Choose the ONE best fit. " +
			"KEY DISTINCTION: 'modify'/'execute' mean the user wants the assistant to DO something to the project or system now. 'answer' means the user wants a REPLY — an explanation, an opinion, a plan to read, a review, or a yes/no judgement — without the assistant changing or running anything. " +
			"Phrases like 'is it better to change X', 'should we add Y', '你觉得…比较好吗', '是否要新建', '打算改哪些' are ASKING for an opinion/plan → answer, not modify. A bug report or problem statement without an explicit 'fix it' is also answer (investigate/propose, don't change).",
		{
			modify:
				"Change the CONTENT of something — edit/write/create/delete code, files, docs, images, config. Any clear directive to modify/adjust/redo a target, however phrased.",
			execute:
				"Run a command or operational action WITHOUT changing source content — git ops (commit/tag/push), build, test, run, restart, deploy, generate a one-off result.",
			answer:
				"Wants a reply only — an explanation, an opinion, a plan to read, a review/inspection, or a yes/no judgement. Includes proposing/discussing a change, asking 'should we', reporting a problem. Nothing gets changed or executed.",
			unclear: "Cannot tell what the user wants — ambiguous, missing context, or no actionable intent.",
		},
	);
}

/** Stage-2 reply-kind nouls, evaluated only when action==answer. */
function intentQuestions() {
	return {
		want_answer: noul(
			"The user wants an explanation or a direct answer to a question — e.g. 'what does this do', 'why is it like this', 'how does X work'.",
			{
				true: "The user wants an explanation or an answer to a question.",
				false: "The user is not asking for an explanation or answer.",
			},
		),
		want_plan: noul(
			"The user wants a plan, a design, an approach, or options to consider — e.g. 'how should we do this', 'give me a proposal', 'what's the best way', 'is it better to change X'. They want to see and discuss the approach before any change.",
			{
				true: "The user wants a plan/proposal/approach to review.",
				false: "The user is not asking for a plan or approach.",
			},
		),
		want_review: noul(
			"The user wants the assistant to review, check, or critique existing code or a change — e.g. 'is this correct', 'review this', 'any problems here', 'find the bug'. They want findings/opinions, not edits.",
			{
				true: "The user wants a review, critique, or inspection of existing code.",
				false: "The user is not asking for a review or inspection.",
			},
		),
	};
}

/**
 * Classify the user's intent. `context` supplies surrounding text; `history`
 * supplies recent prior user messages used to disambiguate an unclear result
 * (one retry, then it stays unclear).
 */
export async function classifyIntent(
	client: Client,
	text: string,
	opts: { context?: string; history?: string[] } = {},
): Promise<IntentResult> {
	let state: unknown = text;
	if (opts.context) state = `Context:\n${opts.context}\n\nUser input:\n${text}`;

	const q = { action: actionQuestion() };
	let { answers } = await client.evaluate(state, q);
	let action = answers.action;

	// Unclear + history -> one re-judge with the recent conversation folded in.
	if (action.choice === "unclear" && opts.history && opts.history.length > 0) {
		state =
			`Recent conversation (oldest first):\n${opts.history.join("\n")}` +
			`\n\nUser input (judge this one):\n${text}`;
		({ answers } = await client.evaluate(state, q));
		action = answers.action;
	}

	const res: IntentResult = {
		action: action.choice,
		actionP: action.confidence,
		shouldModify: action.choice === "modify",
		intents: {},
		primary: action.choice,
	};

	if (res.action === "answer") {
		const { answers: a2 } = await client.evaluate(state, intentQuestions());
		let best = "";
		let bp = -1;
		for (const id of ["want_answer", "want_plan", "want_review"] as const) {
			const p = a2[id]?.noul ?? 0;
			const label = id.slice("want_".length);
			res.intents[label] = p;
			if (p > bp) {
				best = label;
				bp = p;
			}
		}
		if (best) res.primary = `answer:${best}`;
	}
	return res;
}
