/*
 * Copyright (C) 2026 codesafe contributors
 * SPDX-License-Identifier: AGPL-3.0-or-later
 * See COPYING in the project root for the full license.
 */
// delete.ts — pre-delete safety judgement: build a depth-limited directory tree
// of the target and ask the model to classify the deletion safe/sensitive/
// dangerous. Dangerous aborts; sensitive goes to a recover area; safe deletes.

import * as fs from "node:fs";
import * as os from "node:os";
import * as path from "node:path";
import type { Client } from "./typesafe";
import { TokenLimitError, choice } from "./typesafe";

export type DeleteVerdict = "safe" | "sensitive" | "dangerous";

/** Judge deletion safety; retries with a shallower tree on token limit. */
export async function judgeDelete(
	client: Client,
	root: string,
	target: string,
): Promise<{ verdict: DeleteVerdict; prob: number }> {
	for (let depth = 4; depth >= 1; depth--) {
		const state = `Project root: ${root}\nTarget to delete: ${target}\n\nDirectory tree of target (depth-limited):\n${buildTree(target, depth)}`;
		try {
			const { answers } = await client.evaluate(state, {
				del: choice(
					`You are given a project root, a directory tree, and a target path to be deleted. Judge the deletion:

safe — obviously disposable: build/dist output, node_modules, vendor, caches, .tmp, generated artifacts, empty or clearly throwaway dirs.
sensitive — anything that might hold user-authored or recoverable-wanted content: documents, source files, configs, notes, unfamiliar data dirs. When in doubt between safe and sensitive, choose sensitive — it gets moved to a recover area, not destroyed.
dangerous — would destroy things outside the intended area: filesystem/drive root, user home, system dirs (Windows, Program Files, /etc, /usr), another project's files, or a symlink escaping the target. Must NOT delete or move.`,
					{
						safe: "permanent delete is fine — disposable/generated content",
						sensitive: "move to a recover area — possibly user data, err on the side of caution",
						dangerous: "abort — would destroy system/user data or unrelated files",
					},
				),
			});
			const a = answers.del;
			return { verdict: a.choice as DeleteVerdict, prob: a.probabilities[a.choice] ?? a.confidence };
		} catch (err) {
			if (err instanceof TokenLimitError && depth > 1) continue;
			throw err;
		}
	}
	throw new Error("directory tree too large to judge safely");
}

/** Build a depth-limited directory tree; each level caps at 60 entries. */
function buildTree(root: string, depth: number): string {
	let out = root + "\n";
	out += walkTree(root, depth, "");
	return out;
}

function walkTree(dir: string, depth: number, indent: string): string {
	let out = "";
	if (depth <= 0) {
		const { dirs, files } = countEntries(dir);
		if (dirs > 0 || files > 0) out += `${indent}  … ${dirs} folders, ${files} files\n`;
		return out;
	}
	let entries: fs.Dirent[];
	try {
		entries = fs.readdirSync(dir, { withFileTypes: true });
	} catch {
		return out;
	}
	const MAX = 60;
	for (let i = 0; i < entries.length; i++) {
		if (i >= MAX) {
			out += `${indent}  … ${entries.length - MAX} more entries\n`;
			break;
		}
		const e = entries[i];
		if (e.isDirectory()) {
			out += `${indent}${e.name}/\n`;
			out += walkTree(path.join(dir, e.name), depth - 1, indent + "  ");
		} else {
			out += `${indent}${e.name}\n`;
		}
	}
	return out;
}

function countEntries(dir: string): { dirs: number; files: number } {
	try {
		const es = fs.readdirSync(dir, { withFileTypes: true });
		let dirs = 0;
		let files = 0;
		for (const e of es) e.isDirectory() ? dirs++ : files++;
		return { dirs, files };
	} catch {
		return { dirs: 0, files: 0 };
	}
}

/** Permanently remove a path (files and directories). */
export function removeAll(target: string): void {
	fs.rmSync(target, { recursive: true, force: true });
}

/** Move a path into a quarantine dir under the OS temp dir (recover area). */
export function quarantine(target: string): string {
	const stamp = new Date()
		.toISOString()
		.replace(/[-:T]/g, "")
		.replace(/\..*$/, "")
		.slice(0, 14);
	const dest = path.join(os.tmpdir(), "codesafe-deleted", stamp, path.basename(target));
	fs.mkdirSync(path.dirname(dest), { recursive: true });
	try {
		fs.renameSync(target, dest);
	} catch {
		// Cross-device move: copy then remove.
		copyTree(target, dest);
		removeAll(target);
	}
	return dest;
}

function copyTree(src: string, dst: string): void {
	const st = fs.statSync(src);
	if (st.isDirectory()) {
		fs.mkdirSync(dst, { recursive: true });
		for (const e of fs.readdirSync(src)) copyTree(path.join(src, e), path.join(dst, e));
	} else {
		fs.copyFileSync(src, dst);
	}
}
