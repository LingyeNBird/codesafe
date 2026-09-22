/*
 * Copyright (C) 2026 codesafe contributors
 * SPDX-License-Identifier: AGPL-3.0-or-later
 * See COPYING in the project root for the full license.
 */
// index.ts — codesafe Pi/OMP extension entry point.
//
// Loads the user's codesafe config (API key), injects a per-turn intent hint and
// a hard "use the codesafe tools" constraint into the system prompt, soft-warns
// when a tool result shows a raw delete, and registers the codesafe_* tools.

import * as fs from "node:fs";
import * as path from "node:path";

import type { ExtensionAPI, ExtensionContext } from "@earendil-works/pi-coding-agent";
import { Type } from "@sinclair/typebox";

import { classify, scopeCriteria, suggestion } from "./core/classify";
import { configPath, loadConfig, loadProject, todoModeOf, todoOf, type Config, type ProjectConfig } from "./core/config";
import { judgeDelete, quarantine, removeAll } from "./core/delete";
import { repoRoot, stagedDiff, worktreeDiff, unstagedDiff, commit as gitCommit } from "./core/git";
import { classifyIntent } from "./core/intent";
import {
	checkCommitRules,
	checkRules,
	excludeFiles,
	firstFail,
	type RuleResult,
} from "./core/rules";
import { Client, DEFAULT_MODEL } from "./core/typesafe";

// ─────────────────────────────────────────────────────────────────────────────
// Host/context plumbing
// ─────────────────────────────────────────────────────────────────────────────

/** Resolved runtime for one repository dir: client + project config + root. */
interface Ctx {
	client: Client;
	root: string;
	cfg: Config;
	pc: ProjectConfig;
	lang: string;
}

const cfg = loadConfig();

function makeClient(c: Config, model?: string, refresh?: boolean): Client {
	const client = new Client(c.api_key, model || DEFAULT_MODEL);
	client.cacheOn = c.cache !== false;
	client.refresh = refresh === true;
	return client;
}


/** Resolve a working dir to its repo context (client + project config + root).
 *  Config is re-read every call so codesafe_todo writes take effect immediately
 *  (the module-level `cfg` is a startup snapshot used only for the api_key gate). */
async function resolveCtx(dir: string | undefined, cwd: string, model?: string, refresh?: boolean): Promise<Ctx> {
	const base = dir || cwd;
	const root = (await repoRoot(base)) || base;
	const fresh = loadConfig();
	return {
		client: makeClient(fresh, model, refresh),
		root,
		cfg: fresh,
		pc: loadProject(root),
		lang: fresh.lang || "en",
	};
}

function noKey(): never {
	throw new Error(
		"codesafe: no TypeSafe API key. Run `codesafe config api_key=<key>` once, or set it in " +
			"the codesafe config.json — this extension reads the same file.",
	);
}

function requireKey(): void {
	if (!cfg.api_key) noKey();
}

// ─────────────────────────────────────────────────────────────────────────────
// Delete-pattern detection (soft reminder only — never blocks)
// ─────────────────────────────────────────────────────────────────────────────

/** Shell verbs and APIs that delete files/dirs. */
const DELETE_PATTERNS: RegExp[] = [
	/\b(?:rm|rmdir|rd|unlink|shred)\b/i,
	/\b(?:del|erase|Remove-Item|ri)\b/i,
	/\bgit\s+(?:rm|clean)\b/i,
	/\b(?:rimraf|send2trash|removeSync|rmSync|rmtree|remove_tree)\b/i,
	/\b(?:delete|remove|destroy)\s*\(/i,
	/\bfs(?:Promises)?\.(?:rm|rmSync|unlink|unlinkSync|renameSync)\s*\(/i,
];

/** True when a bash command text looks like it may delete something. */
function looksLikeDelete(cmd: string): boolean {
	return DELETE_PATTERNS.some(re => re.test(cmd));
}

// ─────────────────────────────────────────────────────────────────────────────
// Tool parameter schemas (TypeBox — understood by both Pi and OMP)
// ─────────────────────────────────────────────────────────────────────────────

const dirParam = Type.Optional(Type.String({ description: "git repo dir (default: cwd)" }));
const modelParam = Type.Optional(Type.String({ description: "TypeSafe model id" }));
const refreshParam = Type.Optional(Type.Boolean({ description: "bypass the response cache" }));

// ─────────────────────────────────────────────────────────────────────────────
// Result formatting helpers
// ─────────────────────────────────────────────────────────────────────────────

function text(s: string) {
	return { content: [{ type: "text" as const, text: s }] };
}

function formatViolations(res: RuleResult[]): string {
	const lines: string[] = [];
	for (const r of res) {
		if (r.skip || r.pass) continue;
		const lvl = (r.rule.level ?? "error") === "warn" ? "warn" : "error";
		lines.push(`${lvl} ${r.rule.id ?? r.rule.text}: ${r.rule.fail ?? "violates rule"}`);
	}
	return lines.join("\n");
}

// ─────────────────────────────────────────────────────────────────────────────
// Extension entry
// ─────────────────────────────────────────────────────────────────────────────

export default function codesafeExtension(pi: ExtensionAPI): void {
	// ── 1) intent hint + constraint injection, per turn ────────────────────────
	pi.on("before_agent_start", async (event, ctx) => {
		if (!cfg.api_key) return; // no key -> degrade silently to no-op
		let intentLine = "";
		try {
			const intent = await classifyIntent(makeClient(cfg), event.prompt, {});
			intentLine = intentHint(intent);
		} catch {
			intentLine = "";
		}

		const constraint =
			"[codesafe]\n" +
			"- Never commit code with a raw `git commit` / shell command — call the codesafe_commit tool.\n" +
			"- Never delete files or directories with `rm`/`Remove-Item`/`git rm`/`unlink` or an edit/write delete — call codesafe_delete.\n" +
			"- If the user gave you a concrete task, record it with codesafe_todo before you start, and self-check with codesafe_diff before committing.\n" +
			"- The [intent: ...] tag on the user's message is a hint about what they want; it is not a command. If it is 'unclear', state in one sentence what you believe the user wants before proceeding.";

		// systemPrompt is a string on Pi, string[] on OMP — append accordingly.
		const sp = (event as { systemPrompt?: string | string[] }).systemPrompt;
		const nextPrompt = Array.isArray(sp) ? [...sp, constraint] : `${sp ?? ""}\n\n${constraint}`;

		return {
			message: intentLine
				? {
						customType: "codesafe-intent",
						content: intentLine,
						display: false,
					}
				: undefined,
			systemPrompt: nextPrompt,
		};
	});

	// ── 2) soft reminder when a tool result shows a raw delete ────────────────
	pi.on("tool_result", async event => {
		if (event.toolName !== "bash" && event.toolName !== "edit" && event.toolName !== "write") return;
		const text = event.content
			?.map((c: { type?: string; text?: string }) => (c.type === "text" ? c.text : ""))
			.join("\n") ?? "";
		const src =
			event.toolName === "bash"
				? ((event as { input?: { command?: string } }).input?.command ?? "")
				: JSON.stringify((event as { input?: unknown }).input ?? "");
		if (!looksLikeDelete(src) && !looksLikeDelete(text)) return;
		return {
			content: [
				...(event.content ?? []),
				{
					type: "text" as const,
					text: "\n[codesafe] A delete operation was detected. For deleting files/dirs, use the codesafe_delete tool — it runs a safety check and can quarantine instead of destroying data.",
				},
			],
		};
	});

	// ── 3) codesafe_commit ─────────────────────────────────────────────────────
	pi.registerTool({
		name: "codesafe_commit",
		label: "codesafe commit",
		loadMode: "essential",
		description:
			"Validate the commit message + staged diff against codesafe.yaml rules and the recorded TODO, generate a conventional-commit type(scope) prefix, and run git commit. Use this instead of `git commit`.",
		parameters: Type.Object({
			message: Type.String({ description: "commit subject line (first -m)" }),
			body: Type.Optional(Type.String({ description: "optional body paragraphs (additional -m)" })),
			breaking: Type.Optional(Type.Boolean({ description: "mark as a breaking change (adds !)" })),
			reclassify: Type.Optional(Type.Boolean({ description: "re-run the type(scope) classifier for the prefix — DISCARDS the user's own prefix. Restricted: only when the user explicitly wants the prefix regenerated, or the subject's prefix is malformed/unparseable. Never to override a prefix you merely disagree with." })),
			pass: Type.Optional(Type.Array(Type.String(), { description: "glob(s) of files to exempt from rule checks" })),
			dir: dirParam,
			model: modelParam,
			refresh: refreshParam,
		}),
		async execute(_id, params, _sig, _upd, ctx) {
			requireKey();
			const c = await resolveCtx(params.dir, ctx.cwd, params.model, params.refresh);
			const raw = params.body ? `${params.message}\n\n${params.body}` : params.message;

			// commit_rules + prefix detection on the raw message.
			const keepUser = c.pc.prefix_conflict === "keep_user";
			const { results: msgRes, hasPrefix } = await checkCommitRules(c.client, raw, c.pc.commit_rules ?? [], keepUser);
			const fail = firstFail(msgRes);
			if (fail) return text(`commit message violates rule ${fail.rule.id}: ${fail.rule.fail}`);

			// Strip a user-supplied prefix when the model detects one. A valid
			// `type(scope):` prefix is always stripped (also under reclassify, so a
			// regenerated prefix doesn't double up). An unparseable "prefix" bails
			// — unless reclassify=true, which skips detection and just reclassifies.
			let subject = params.message;
			let userPrefix = "";
			if (hasPrefix) {
				const m = /^([a-zA-Z]+\s*[（(]?[^:：)）]*[)）]?\s*!?)\s*[:：]\s*/.exec(subject);
				if (m) {
					userPrefix = m[1].replace(/[（）]/g, m2 => (m2 === "（" ? "(" : ")")).replace(/\s+/g, "");
					subject = subject.slice(m[0].length).trim();
				} else if (!params.reclassify) {
					return text("commit subject looks prefixed but is malformed — use `type(scope): ` format, or pass reclassify=true to regenerate the prefix");
				}
			}

			// code rules + TODO check on the staged diff.
			const todo = todoOf(c.cfg, c.root);
			const mode = todoModeOf(c.pc.todo_mode);
			let diff = await stagedDiff(c.root);
			diff = excludeFiles(diff, params.pass ?? []);
			if (diff.trim() && ((c.pc.rules?.length ?? 0) > 0 || todo)) {
				const res = await checkRules(c.client, diff, c.pc.rules ?? [], todo, todoModeOf(c.pc.todo_mode));
				for (const r of res) {
					if (r.skip) continue;
					if (r.todoMissing) return text(`rule ${r.rule.id} requires a TODO (todo_mode=strict); set one with codesafe_todo`);
					if (r.rule.id === "__todo__" && !r.pass) return text(`diff does not implement the TODO: "${todo}"`);
					if (!r.pass && (r.rule.level ?? "error") !== "warn") return text(`diff violates rule ${r.rule.id}: ${r.rule.fail}`);
				}
			}
			if (mode === "strict" && !todo) return text("todo_mode=strict: set a task with codesafe_todo first");

			// prefix: keep user's or classify.
			let prefix: string;
			if (userPrefix && keepUser && !params.reclassify) {
				prefix = userPrefix;
			} else {
				const res = await classify(c.client, diff, c.pc.scopes ?? scopeCriteria(), c.pc.allow_none !== false, subject);
				res.breaking = params.breaking === true;
				prefix = suggestion(res);
			}
			if (params.breaking && !prefix.endsWith("!")) prefix += "!";

			const full = `${prefix}: ${subject}`;
			const msgs = [full, ...(params.body ? [params.body] : [])];
			try {
				await gitCommit(c.root, msgs);
			} catch (err) {
				return text(`git commit failed: ${String(err)}`);
			}
			return text(`committed: ${full}`);
		},
	});

	// ── 4) codesafe_delete ─────────────────────────────────────────────────────
	pi.registerTool({
		name: "codesafe_delete",
		label: "codesafe delete",
		loadMode: "essential",
		description:
			"Safely delete a file or directory: the target's directory tree is judged safe/sensitive/dangerous first. Dangerous aborts, sensitive is quarantined to a recover area, safe is deleted. Use this instead of rm/Remove-Item/git rm.",
		parameters: Type.Object({
			path: Type.String({ description: "file or directory to delete" }),
			quarantine: Type.Optional(Type.Boolean({ description: "move to a recover area instead of deleting" })),
			dir: dirParam,
			model: modelParam,
			refresh: refreshParam,
		}),
		async execute(_id, params, _sig, _upd, ctx) {
			requireKey();
			const c = await resolveCtx(params.dir, ctx.cwd, params.model, params.refresh);
			const target = params.path;
			const { verdict, prob } = await judgeDelete(c.client, c.root, target);
			if (verdict === "dangerous") {
				return text(`refused: deleting ${target} is dangerous (p=${prob.toFixed(2)}) — would destroy system/user data or unrelated files`);
			}
			if (verdict === "sensitive" || params.quarantine) {
				const dest = quarantine(target);
				return text(`quarantined ${target} -> ${dest} (verdict=${verdict} p=${prob.toFixed(2)})`);
			}
			removeAll(target);
			return text(`deleted ${target} (verdict=safe p=${prob.toFixed(2)})`);
		},
	});

	// ── 5) codesafe_diff ───────────────────────────────────────────────────────
	pi.registerTool({
		name: "codesafe_diff",
		label: "codesafe diff",
		loadMode: "essential",
		description: "Check the current worktree/staged diff against codesafe.yaml rules and the recorded TODO.",
		parameters: Type.Object({
			staged: Type.Optional(Type.Boolean({ description: "check only the staged diff" })),
			worktree: Type.Optional(Type.Boolean({ description: "check only the unstaged diff" })),
			pass: Type.Optional(Type.Array(Type.String(), { description: "glob(s) of files to exempt" })),
			dir: dirParam,
			model: modelParam,
			refresh: refreshParam,
		}),
		async execute(_id, params, _sig, _upd, ctx) {
			requireKey();
			const c = await resolveCtx(params.dir, ctx.cwd, params.model, params.refresh);
			let diff = params.staged ? await stagedDiff(c.root) : params.worktree ? await unstagedDiff(c.root) : await worktreeDiff(c.root);
			diff = excludeFiles(diff, params.pass ?? []);
			if (!diff.trim()) return text("no changes to check");
			const todo = todoOf(c.cfg, c.root);
			const res = await checkRules(c.client, diff, c.pc.rules ?? [], todo, todoModeOf(c.pc.todo_mode));
			const v = formatViolations(res);
			return text(v || "diff satisfies all rules");
		},
	});

	// ── 6) codesafe_todo / clear ───────────────────────────────────────────────
	pi.registerTool({
		name: "codesafe_todo",
		label: "codesafe todo",
		loadMode: "essential",
		description: "Record the current task for this repo. commit/diff will check that the staged diff implements it.",
		parameters: Type.Object({
			text: Type.String({ description: "task description; empty clears it" }),
			dir: dirParam,
		}),
		async execute(_id, params, _sig, _upd, ctx) {
			const base = params.dir || ctx.cwd;
			const root = (await repoRoot(base)) || base;
			setTodo(root, params.text);
			return text(params.text ? `todo set: ${params.text}` : "todo cleared");
		},
	});

	// ── 7) codesafe_classify ───────────────────────────────────────────────────
	pi.registerTool({
		name: "codesafe_classify",
		label: "codesafe classify",
		loadMode: "essential",
		description: "Classify a diff into a conventional-commit type + scope suggestion.",
		parameters: Type.Object({
			dir: dirParam,
			model: modelParam,
			refresh: refreshParam,
		}),
		async execute(_id, params, _sig, _upd, ctx) {
			requireKey();
			const c = await resolveCtx(params.dir, ctx.cwd, params.model, params.refresh);
			const diff = await stagedDiff(c.root);
			const res = await classify(c.client, diff, c.pc.scopes ?? scopeCriteria(), c.pc.allow_none !== false);
			return text(JSON.stringify({ suggestion: suggestion(res), ...res }));
		},
	});

	// ── 8) codesafe_intent ─────────────────────────────────────────────────────
	pi.registerTool({
		name: "codesafe_intent",
		label: "codesafe intent",
		loadMode: "essential",
		description: "Classify a piece of user text into modify/execute/answer/unclear (+ reply kinds for answer).",
		parameters: Type.Object({
			text: Type.String({ description: "the user's message" }),
			context: Type.Optional(Type.String({ description: "surrounding context" })),
			history: Type.Optional(Type.Array(Type.String(), { description: "recent prior user messages, oldest first" })),
			model: modelParam,
			refresh: refreshParam,
		}),
		async execute(_id, params, _sig, _upd, ctx) {
			requireKey();
			const c = makeClient(cfg, params.model, params.refresh);
			const res = await classifyIntent(c, params.text, { context: params.context, history: params.history });
			return text(JSON.stringify(res));
		},
	});
}

// ─────────────────────────────────────────────────────────────────────────────
// Intent hint text
// ─────────────────────────────────────────────────────────────────────────────

function intentHint(r: { action: string; primary: string }): string {
	switch (r.action) {
		case "modify":
			return "[intent: modify] the user wants you to change code/files.";
		case "execute":
			return "[intent: execute] the user wants a command/operation run (no source edits).";
		case "answer":
			return `[intent: ${r.primary}] the user wants a reply (explanation/plan/review) — do not change anything.`;
		default:
			return "[intent: unclear] state in one sentence what you believe the user wants, then continue.";
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// TODO persistence (write-through to codesafe's config.json todo map)
// ─────────────────────────────────────────────────────────────────────────────

function setTodo(root: string, text: string): void {
	const p = configPath();
	let cfgData: Config = { api_key: "" };
	try {
		cfgData = JSON.parse(fs.readFileSync(p, "utf8")) as Config;
	} catch {
		/* keep defaults */
	}
	cfgData.todo = cfgData.todo ?? {};
	if (text) cfgData.todo[root] = text;
	else delete cfgData.todo[root];
	fs.mkdirSync(path.dirname(p), { recursive: true });
	fs.writeFileSync(p, JSON.stringify(cfgData), { mode: 0o600 });
}
