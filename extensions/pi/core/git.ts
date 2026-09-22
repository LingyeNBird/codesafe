/*
 * Copyright (C) 2026 codesafe contributors
 * SPDX-License-Identifier: AGPL-3.0-or-later
 * See COPYING in the project root for the full license.
 */
// git.ts — minimal git access for the extension: repo root, staged/worktree
// diff, and the final `git commit`. Uses the host's shell via child_process.

import { execFile } from "node:child_process";

/** Run git and return trimmed stdout; throws on non-zero exit. */
function git(dir: string, args: string[]): Promise<string> {
	const { promise, resolve, reject } = Promise.withResolvers<string>();
	execFile("git", ["-C", dir, ...args], { maxBuffer: 64 * 1024 * 1024 }, (err, stdout, stderr) => {
		if (err) reject(new Error(stderr?.toString().trim() || String(err)));
		else resolve(stdout.toString());
	});
	return promise;
}

/** Top-level repo path for dir, or "" when not inside a git repository. */
export async function repoRoot(dir: string): Promise<string> {
	try {
		const out = await git(dir, ["rev-parse", "--show-toplevel"]);
		return out.trim();
	} catch (err) {
		if (/not a git repository/i.test(String(err))) return "";
		throw err;
	}
}

/** Staged diff (git diff --cached). */
export function stagedDiff(dir: string): Promise<string> {
	return git(dir, ["diff", "--cached"]);
}

/** Whole-worktree diff vs HEAD (staged + unstaged). */
export function worktreeDiff(dir: string): Promise<string> {
	return git(dir, ["diff", "HEAD"]);
}

/** Unstaged-only diff. */
export function unstagedDiff(dir: string): Promise<string> {
	return git(dir, ["diff"]);
}

/** Commit with the given -m arguments; returns git's combined output. */
export function commit(dir: string, messages: string[]): Promise<string> {
	const args = ["commit"];
	for (const m of messages) args.push("-m", m);
	return git(dir, args);
}
