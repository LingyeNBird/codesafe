/*
 * Copyright (C) 2026 codesafe contributors
 * SPDX-License-Identifier: AGPL-3.0-or-later
 * See COPYING in the project root for the full license.
 */
// classify.ts — conventional-commit type/scope classification over a diff.

import type { Client } from "./typesafe";
import { choice } from "./typesafe";

/** conventional-commit type -> meaning. */
export const TYPE_CRITERIA: Record<string, string> = {
	feat: "adds a new feature or capability",
	fix: "fixes a bug or incorrect behavior",
	refactor: "restructures code without changing behavior",
	perf: "improves performance without changing behavior",
	style: "formatting/whitespace only, no logic change",
	test: "adds or changes tests only",
	docs: "documentation or comments only",
	build: "build system, dependencies, or packaging",
	ci: "CI/CD pipeline, workflows, automation",
	chore: "maintenance, version bumps, routine tasks",
	revert: "reverts a previous commit",
	security: "fixes a security vulnerability or hardens security",
};

/** Form scopes (project-structure dependent). */
export const FORM_SCOPES: Record<string, string> = {
	app: "application code",
	ui: "user interface / frontend",
	api: "API / backend endpoints",
	db: "database / data layer",
	cli: "command-line interface",
	config: "configuration / settings",
	repo: "repo-wide / cross-cutting",
};

/** Universal scopes (any project can have these). */
export const UNIVERSAL_SCOPES: Record<string, string> = {
	docs: "documentation",
	test: "tests",
	build: "build / packaging",
	ci: "CI / automation",
	deps: "dependency updates",
	release: "release / versioning",
};

/** Default full scope set: form + universal + none. */
export function scopeCriteria(): Record<string, string> {
	return { none: "no single area — cross-cutting or repo-wide change", ...FORM_SCOPES, ...UNIVERSAL_SCOPES };
}

/** Classification result for one commit diff. */
export interface ClassifyResult {
	type: string;
	scope: string;
	breaking: boolean;
	typeConfidence: number;
	scopeConfidence: number;
}

/** Render the "type(scope)" / "type(scope)!" / "type!" / "type" suggestion. */
export function suggestion(r: ClassifyResult): string {
	let s = r.type;
	if (r.scope && r.scope !== "none") s += `(${r.scope})`;
	if (r.breaking) s += "!";
	return s;
}

/** Classify a diff into conventional type + scope. `subject` (the user's -m text) is optional reference context. */
export async function classify(
	client: Client,
	diff: string,
	scopes: Record<string, string>,
	allowNone: boolean,
	subject?: string,
): Promise<ClassifyResult> {
	const scopeSet = Object.keys(scopes).length === 0 ? scopeCriteria() : { ...scopes };
	if (allowNone && !("none" in scopeSet)) {
		scopeSet.none = "no single area — cross-cutting or repo-wide change";
	}
	// Feed the user's subject as reference context ahead of the diff. The prompt
	// (questions) is unchanged — this only adds signal to the state the model reads.
	const state = subject?.trim() ? `commit subject (user-provided): "${subject.trim()}"\n\n${diff}` : diff;
	const { answers } = await client.evaluate(state, {
		type: choice(
			"This is a git commit (subject line + diff). Which conventional-commit type best describes the change?",
			TYPE_CRITERIA,
		),
		scope: choice("Which project scope does this change mainly affect?", scopeSet),
	});
	const t = answers.type;
	const s = answers.scope;
	return {
		type: t.choice,
		scope: s.choice,
		breaking: false,
		typeConfidence: t.confidence,
		scopeConfidence: s.confidence,
	};
}
