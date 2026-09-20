/*
 * Copyright (C) 2026 codesafe contributors
 * SPDX-License-Identifier: AGPL-3.0-or-later
 * See COPYING in the project root for the full license.
 */
// codesafe agent：输出一段给 AI 的规则文本——教 AI 用 codesafe 代替裸 rm/git commit。
package cli

import (
	"flag"
	"fmt"
)

// agentRules 是贴进 AI 规则文件（AGENTS.md / CLAUDE.md / .cursorrules）的文本。
const agentRules = `# codesafe rules for AI agents

When working in this repository, use the codesafe CLI instead of raw file/git
operations. These are hard requirements — obey codesafe's verdicts; if it
refuses, stop and report, never work around it.

## Do not delete or commit directly

1. NEVER delete files or directories with rm / rm -rf / shell delete commands.
   To delete, run "codesafe delete <path>". It judges safety first — refuses
   dangerous targets and quarantines (moves to a recover area) sensitive ones
   instead of permanent delete. "codesafe delete <path> --check" prints the
   verdict (safe|sensitive|dangerous) without touching anything; "--yes" skips
   the check.

2. NEVER commit with "git commit". To commit, run
   "codesafe commit -m '<subject>' [-m '<body>' ...]". It validates the
   project's commit-message rules and code rules from codesafe.yaml, generates
   the correct type(scope) prefix, and runs git commit itself. Do not hand-write
   the type(scope) prefix — codesafe generates it (set prefix_conflict in
   codesafe.yaml to keep_user if you want your own prefix honored). If codesafe
   aborts, fix the reported rule violation and retry. Never add a commit_rule
   that checks the type(scope) prefix — codesafe generates it, so it is always
   correct. "on: prefix" rules run only under prefix_conflict=keep_user; in
   override mode they are skipped, since the prefix is regenerated anyway.

3. To check pending changes against project rules, run "codesafe diff". It
   lists which codesafe.yaml rules the diff violates. "--staged" checks staged
   only, "--worktree" unstaged only, default is the whole worktree diff.

## Commit-message suggestion

- "codesafe" (bare) prints a suggested type(scope) for the staged or worktree
  diff — pipe it into your commit subject. "--detail" shows confidence and
  per-candidate percentages. "--source staged|worktree|<sha>" picks the diff.
- Scope vocabulary: the project may define scopes in codesafe.yaml; otherwise
  codesafe screens a built-in set against the directory tree and caches it.

## Task tracking (TODO)

- "codesafe todo <task>" records the current task for this repo. "codesafe
  todo" shows it, "codesafe todo --clear" clears it. A successful
  "codesafe commit" clears it automatically.
- When a todo is set, "codesafe commit"/"codesafe diff" also ask the model
  whether the diff implements that task; a mismatch aborts the commit.
- codesafe.yaml "rules" may embed {{TODO}} in text/pass/fail — it is replaced
  with the current task text. todo_mode: off|loose|strict controls whether a
  {{TODO}} rule requires a set todo (strict aborts when empty).

## Config

- Per-project rules/scopes live in codesafe.yaml at the repo root. Scaffold it
  with "codesafe init" (commented template + AI instructions) or see a filled
  example with "codesafe init --sample".
- "codesafe --config key=value" persists user config (api_key, lang, scopes,
  nonescope). "codesafe --set key=value" applies to one run only.
`

// runAgent 打印 AI 规则文本到 stdout（用户贴进 AGENTS.md/CLAUDE.md 等）。
func runAgent(args []string) error {
	fs := flag.NewFlagSet("codesafe agent", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	fmt.Print(agentRules)
	return nil
}
