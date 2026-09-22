/*
 * Copyright (C) 2026 codesafe contributors
 * SPDX-License-Identifier: AGPL-3.0-or-later
 * See COPYING in the project root for the full license.
 */
// rules.ts — codesafe.yaml rule evaluation: turn each rule into a noul over the
// diff, batch them into one request, plus the built-in "diff implements TODO"
// check and the commit-message rule pass.

import { readFileSync } from "node:fs";
import { join } from "node:path";
import type { Config, ProjectConfig, Rule } from "./config";
import { todoModeOf, todoOf } from "./config";
import { type Client, type Question, TokenLimitError, noul } from "./typesafe";

/** Result of evaluating one rule. */
export interface RuleResult {
	rule: Rule;
	pass: boolean;
	prob: number;
	skip?: boolean; // not applicable (files glob didn't match / todo skipped)
	truncated?: boolean; // diff was truncated to fit context
	todoMissing?: boolean; // strict todo_mode and no TODO set
}

const BUILTIN_TODO_ID = "__todo__";
const HAS_PREFIX_ID = "__has_prefix";

/** Extract changed file paths from a unified diff (dedup, strip a//b/). */
export function diffFiles(diff: string): string[] {
	const seen = new Set<string>();
	for (const line of diff.split("\n")) {
		if (!line.startsWith("diff --git ")) continue;
		const parts = line.split(/\s+/);
		if (parts.length >= 4) seen.add(parts[3].replace(/^b\//, ""));
	}
	return [...seen];
}

/** Minimal glob: "*.ext", "dir/*", "*.{a,b}", exact names. */
export function globMatch(pattern: string, name: string): boolean {
	if (!pattern) return true;
	const pats = pattern.includes("{")
		? expandBraces(pattern)
		: [pattern];
	return pats.some(p => wildcard(p, name));
}

function expandBraces(pat: string): string[] {
	const m = /\{([^}]*)\}/.exec(pat);
	if (!m) return [pat];
	return m[1].split(",").flatMap(alt => expandBraces(pat.slice(0, m.index) + alt + pat.slice(m.index + m[0].length)));
}

/** Classic * / ? wildcard match over the whole string. */
function wildcard(pat: string, s: string): boolean {
	const re = new RegExp(
		"^" + pat.split("").map(c => (c === "*" ? ".*" : c === "?" ? "." : c.replace(/[.+^${}()|[\]\\]/g, "\\$&"))).join("") + "$",
	);
	return re.test(s);
}

/** Substitute {{TODO}} in rule text/pass/fail with the quoted task. */
function interpTodo(r: Rule, todo: string): Rule {
	const sub = `the task: "${todo}"`;
	return {
		...r,
		text: (r.text ?? "").replaceAll("{{TODO}}", sub),
		pass: (r.pass ?? "").replaceAll("{{TODO}}", sub),
		fail: (r.fail ?? "").replaceAll("{{TODO}}", sub),
	};
}

function ruleId(r: Rule): string {
	return r.id || r.text || "";
}

/** Build the noul for one rule; scope it to its files glob when present. */
function ruleNoul(r: Rule): Question {
	const pass = r.pass || "the diff satisfies this rule";
	const fail = r.fail || "the diff violates this rule";
	let text = r.text ?? "";
	if (r.files) {
		text += `\n\nScope: this rule applies ONLY to files matching "${r.files}". Ignore all other files in the diff when judging.`;
	}
	return noul(text, { true: pass, false: fail });
}

/**
 * Evaluate diff rules + built-in TODO check in one request. Applies todo_mode
 * handling, {{TODO}} interpolation, files-glob scoping, and token-limit
 * narrowing/truncation fallback.
 */
export async function checkRules(
	client: Client,
	root: string,
	diff: string,
	rules: Rule[],
	todo: string,
	globalMode: string,
): Promise<RuleResult[]> {
	const files = diffFiles(diff);
	const active: Rule[] = [];
	const results: Record<string, RuleResult> = {};
	for (const r0 of rules) {
		let r = { ...r0 };
		if (!r.id) r.id = r.text;
		const id = ruleId(r);
		if (`${r.text}${r.pass}${r.fail}`.includes("{{TODO}}")) {
			const mode = todoModeOf(r.todo_mode || globalMode);
			if (!todo) {
				results[id] =
					mode === "strict"
						? { rule: r, pass: false, prob: 0, todoMissing: true }
						: { rule: r, pass: true, prob: 0, skip: true };
				continue;
			}
			r = interpTodo(r, todo);
		}
		// A rule with `lines` is judged on the matched files' line ranges, not the
		// shared diff — read each matching file's lines and ask it standalone.
		if (r.lines) {
			if (r.files && !files.some(f => globMatch(r.files as string, f))) {
				results[id] = { rule: r, pass: true, prob: 0, skip: true };
			} else {
				results[id] = await checkLinesRule(client, root, r, files);
			}
			continue;
		}
		if (r.files && !files.some(f => globMatch(r.files as string, f))) {
			results[id] = { rule: r, pass: true, prob: 0, skip: true };
		} else {
			active.push(r);
		}
	}
	if (todo) {
		active.push({
			id: BUILTIN_TODO_ID,
			level: "error",
			text: `Does this diff implement the task? Task: "${todo}"`,
			pass: "the diff implements the stated task",
			fail: "the diff does not implement the stated task",
		});
	}
	if (active.length === 0) return Object.values(results);
	const got = await askRules(client, diff, active);
	for (const r of got) results[ruleId(r.rule)] = r;
	// Preserve declared order + append builtin todo result last.
	const out: RuleResult[] = [];
	for (const r of rules) {
		const rr = results[ruleId(r)];
		if (rr) out.push(rr);
	}
	if (todo && results[BUILTIN_TODO_ID]) out.push(results[BUILTIN_TODO_ID]);
	return out;
}

/**
 * Judge a `lines` rule: read the touched files matching `files` glob, extract
 * the given line ranges, and ask the model on that content (not the shared
 * diff). Unreadable files (deleted/binary) are skipped; if none read → skip.
 */
async function checkLinesRule(client: Client, root: string, r: Rule, files: string[]): Promise<RuleResult> {
	const ranges = parseLineRanges(r.lines as string);
	const matched = files.filter(f => !r.files || globMatch(r.files, f));
	if (matched.length === 0) return { rule: r, pass: true, prob: 0, skip: true };
	const parts: string[] = [];
	for (const f of matched) {
		const seg = extractLines(join(root, f), ranges);
		if (seg) parts.push(`=== ${f} ===\n${seg}`);
	}
	if (parts.length === 0) return { rule: r, pass: true, prob: 0, skip: true };
	const res = await askRules(client, parts.join("\n"), [r]);
	return res[0];
}

/** Parse "1-4,-10--1,7" into 1-based inclusive [start,end] pairs (negatives = from end). Throws on bad input. */
function parseLineRanges(spec: string): [number, number][] {
	const out: [number, number][] = [];
	for (const part of spec.split(",")) {
		const p = part.trim();
		if (!p) continue;
		const ab = splitRange(p);
		if (!ab) throw new Error(`invalid lines range "${p}" (expected 1-4, -10--1, or a single line like 7)`);
		out.push(ab);
	}
	if (out.length === 0) throw new Error("lines is empty");
	return out;
}

/**
 * Split "1-4" / "-10--1" / "7" into (a,b). A '-' inside (index≥1) is a range
 * separator; scan right-to-left so the right segment keeps its leading '-'.
 */
function splitRange(s: string): [number, number] | null {
	if (!s.slice(1).includes("-")) {
		const n = Number(s);
		return Number.isInteger(n) ? [n, n] : null;
	}
	for (let i = s.length - 1; i >= 1; i--) {
		if (s[i] !== "-") continue;
		const a = Number(s.slice(0, i));
		const b = Number(s.slice(i + 1));
		if (Number.isInteger(a) && Number.isInteger(b)) return [a, b];
	}
	return null;
}

/**
 * Read a file's given line ranges. Ranges are 1-based inclusive; negatives count
 * from the end (-1 = last line). Out-of-range parts clamp; returns "" if no
 * lines or unreadable. Each output line is prefixed "N|".
 */
function extractLines(path: string, ranges: [number, number][]): string {
	let data: string;
	try {
		data = readFileSync(path, "utf8");
	} catch {
		return "";
	}
	const lines = data.split("\n");
	const total = lines.length;
	const resolve = (n: number): number => (n < 0 ? total + n : n - 1); // 1-based → 0-based, negative → from end
	const out: string[] = [];
	for (const [a, b] of ranges) {
		let lo = resolve(a);
		let hi = resolve(b);
		if (lo > hi) [lo, hi] = [hi, lo];
		lo = Math.max(0, lo);
		hi = Math.min(total - 1, hi);
		for (let i = lo; i <= hi; i++) out.push(`${i + 1}|${lines[i]}`);
	}
	return out.join("\n");
}

/** Batch-ask rules; on token limit split the longest, else narrow+truncate. */
async function askRules(client: Client, diff: string, rules: Rule[]): Promise<RuleResult[]> {
	const qs: Record<string, Question> = {};
	for (const r of rules) qs[ruleId(r)] = ruleNoul(r);
	try {
		const { answers } = await client.evaluate(diff, qs);
		return rules.map(r => {
			const a = answers[ruleId(r)] ?? { noul: 0 };
			return { rule: r, pass: a.noul >= 0.5, prob: a.noul };
		});
	} catch (err) {
		if (!(err instanceof TokenLimitError)) throw err;
		if (rules.length > 1) {
			// Split off the longest-text rule and ask it alone on narrowed diff.
			let bi = 0;
			for (let i = 1; i < rules.length; i++) if ((rules[i].text ?? "").length > (rules[bi].text ?? "").length) bi = i;
			const head = rules[bi];
			const rest = rules.filter((_, i) => i !== bi);
			const single = await askRules(client, narrowDiff(diff, head), [head]);
			const restRes = await askRules(client, diff, rest);
			return [...single, ...restRes];
		}
		// Single rule still too big: narrow to matching files, then truncate.
		const narrowed = narrowDiff(diff, rules[0]);
		const truncated = truncateDiff(narrowed);
		if (truncated === narrowed && narrowed === diff) throw err;
		const res = await askRules(client, truncated, rules);
		return res.map(r => ({ ...r, truncated: true }));
	}
}

/** Truncate a diff to ~96k chars, keeping whole "diff --git" file sections. */
export function truncateDiff(diff: string): string {
	const MAX = 96000;
	if (diff.length <= MAX) return diff;
	const kept: string[] = [];
	let cur = "";
	let total = 0;
	const flush = () => {
		if (!cur) return;
		if (total + cur.length <= MAX) {
			kept.push(cur);
			total += cur.length;
		}
		cur = "";
	};
	for (const line of diff.split("\n")) {
		if (line.startsWith("diff --git ")) flush();
		cur += line + "\n";
	}
	flush();
	let out = kept.join("\n");
	if (!out) out = diff.slice(0, MAX); // single oversized section -> hard cut
	return out + "\n... [diff truncated to fit context]\n";
}

/** Drop file sections whose path matches any glob (user-exempted from judging). */
export function excludeFiles(diff: string, globs: string[]): string {
	if (globs.length === 0) return diff;
	const kept: string[] = [];
	let cur = "";
	let drop = false;
	const flush = () => {
		if (!drop && cur) kept.push(cur);
		cur = "";
	};
	for (const line of diff.split("\n")) {
		if (line.startsWith("diff --git ")) {
			flush();
			const parts = line.split(/\s+/);
			drop = parts.length >= 4 && globs.some(g => globMatch(g, parts[3].replace(/^b\//, "")));
		}
		cur += line + "\n";
	}
	flush();
	return kept.join("\n");
}

/** Extract the file sections of diff matching the rule's files glob. */
function narrowDiff(diff: string, r: Rule): string {
	if (!r.files) return diff;
	const segs: string[] = [];
	let cur = "";
	let keep = false;
	const flush = () => {
		if (keep && cur) segs.push(cur);
		cur = "";
	};
	for (const line of diff.split("\n")) {
		if (line.startsWith("diff --git ")) {
			flush();
			const parts = line.split(/\s+/);
			keep = parts.length >= 4 && globMatch(r.files, parts[3].replace(/^b\//, ""));
		}
		cur += line + "\n";
	}
	flush();
	return segs.length === 0 ? diff : segs.join("\n");
}

/**
 * Check commit-message rules + detect a conventional prefix in one request.
 * Returns per-rule results and whether the subject carries a prefix.
 */
export async function checkCommitRules(
	client: Client,
	rawMessage: string,
	rules: Rule[],
	keepUserPrefix: boolean,
): Promise<{ results: RuleResult[]; hasPrefix: boolean }> {
	const qs: Record<string, Question> = {};
	const applicable: Rule[] = [];
	const skipped: RuleResult[] = [];
	for (const r0 of rules) {
		const r = { ...r0 };
		if (!r.id) r.id = r.text;
		// on:prefix only matters when the user's prefix is kept; under override the
		// prefix is regenerated, so such rules are skipped.
		if (r.on === "prefix" && !keepUserPrefix) {
			skipped.push({ rule: r, pass: true, prob: 0, skip: true });
			continue;
		}
		const part = commitPart(r.on);
		qs[ruleId(r)] = noul(`This is a git commit message. Check this rule about its ${part}. ${r.text ?? ""}`, {
			true: r.pass || `the ${part} satisfies the rule`,
			false: r.fail || `the ${part} violates the rule`,
		});
		applicable.push(r);
	}
	qs[HAS_PREFIX_ID] = noul(
		"Does the first line of this commit message begin with a conventional-commit type/scope prefix — a leading token like 'feat', 'fix(scope)', 'chore:', possibly with non-ASCII punctuation (full-width colon ：or brackets （）) or missing the space after the colon? Answer YES if it clearly starts with a type or type(scope) marker regardless of punctuation correctness.",
		{
			true: "The subject starts with a conventional-commit type/scope prefix, even if malformed.",
			false: "The subject has no type/scope prefix — plain description text.",
		},
	);
	const { answers } = await client.evaluate(rawMessage, qs);
	const hasPrefix = (answers[HAS_PREFIX_ID]?.noul ?? 0) >= 0.5;
	const out = [...skipped];
	for (const r of applicable) {
		const a = answers[ruleId(r)] ?? { noul: 0 };
		out.push({ rule: r, pass: a.noul >= 0.5, prob: a.noul });
	}
	return { results: out, hasPrefix };
}

function commitPart(on?: string): string {
	switch (on) {
		case "body":
			return "body (everything after the first line)";
		case "prefix":
			return "type(scope) prefix at the start of the first line, if present";
		case "all":
			return "entire message";
		default:
			return "subject (the first line, after any type/scope prefix)";
	}
}

/** First failing error-level rule, or undefined. */
export function firstFail(res: RuleResult[]): RuleResult | undefined {
	return res.find(r => !r.skip && !r.pass && (r.rule.level ?? "error") !== "warn");
}

/** Convenience: pull the project's todo + resolved mode for a repo root. */
export function projectTodo(cfg: Config, pc: ProjectConfig, root: string): { todo: string; mode: string } {
	return { todo: todoOf(cfg, root), mode: todoModeOf(pc.todo_mode) };
}
