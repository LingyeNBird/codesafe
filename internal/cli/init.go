/*
 * Copyright (C) 2026 codesafe contributors
 * SPDX-License-Identifier: AGPL-3.0-or-later
 * See COPYING in the project root for the full license.
 */
// codesafe init：生成只有注释的 codesafe.yaml 模板 + 一段给 AI 的提示词（让 AI 按项目填规则）。
package cli

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

// yamlTemplate 是注释掉的 codesafe.yaml 骨架——字段全注释当说明，不产生实际规则。
const yamlTemplate = `# codesafe.yaml — per-project config for the codesafe commit classifier.
# Uncomment and edit what you need. All fields optional.
#
# lang: zh                    # output language: en | zh
# allow_none: true            # allow scope=none (cross-cutting commits with no single scope)
# prefix_conflict: override   # when -m already carries type(scope): keep_user | override (default override)
#
# # scopes — the scope vocabulary for this project. If absent, codesafe screens a
# # built-in vocabulary against your directory tree. Give name: description pairs.
# scopes:
#   cli:     "command-line entrypoint / flag parsing"
#   api:     "HTTP/RPC server layer"
#   db:      "database / migrations / data layer"
#   ui:      "frontend / UI"
#   config:  "configuration loading & persistence"
#   ci:      "CI / release workflows"
#   docs:    "documentation"
#   test:    "tests"
#   deps:    "dependency manifests"
#
# # rules — code rules checked against the diff by "codesafe diff" and "codesafe commit".
# # Each rule is a yes/no statement the model judges. files=glob limits which files
# # the rule applies to; level=error (default) aborts on violation, warn only prints.
# rules:
#   - id: vue-css-split
#     level: error
#     files: "*.vue"
#     text: .vue files must not contain <style> blocks; CSS goes to a sibling .css file
#     pass: all .vue styles live in external .css files
#     fail: a .vue file still contains an inline <style> block
#
# # commit_rules — rules about the commit message itself, checked by "codesafe commit".
# # on = which part to check: subject | body | prefix | all.
# commit_rules:
#   - id: subject-zh
#     on: subject
#     text: the subject must be in Chinese (technical terms may stay English)
#     pass: the subject is primarily Chinese
#     fail: the subject is not Chinese
`

// aiPrompt 是给 AI 的提示词：让 AI 读项目后生成贴合的 codesafe.yaml。
const aiPrompt = `
───────────────────────── AI prompt ─────────────────────────
Read this project's directory structure, README, and a few representative source
files, then fill in codesafe.yaml for it. Write:

  scopes        — the functional areas/modules this project actually has
                  (derive from real top-level dirs / packages, not generic words).
  rules         — this project's own code conventions that a diff can violate
                  (style/architecture constraints specific to THIS codebase).
                  Each rule: id, level (error|warn), files (glob), text (the rule),
                  pass (what satisfies it), fail (what violates it).
  commit_rules  — rules about the commit message itself (language, format, etc).

Keep rules concrete and checkable from a diff. Omit generic advice that applies
to every project — only rules specific to this one.
─────────────────────────────────────────────────────────────
`

// runInit 生成 codesafe.yaml 注释模板到当前目录（已存在则不覆盖），并打印 AI 提示词。
func runInit(args []string) error {
	fs := flag.NewFlagSet("codesafe init", flag.ContinueOnError)
	dir := fs.String("dir", ".", "directory to write codesafe.yaml")
	if err := fs.Parse(args); err != nil {
		return err
	}
	path := filepath.Join(*dir, "codesafe.yaml")
	if _, err := os.Stat(path); err == nil {
		fmt.Printf("codesafe.yaml already exists at %s\n", path)
	} else {
		if err := os.WriteFile(path, []byte(yamlTemplate), 0o644); err != nil {
			return err
		}
		fmt.Printf("wrote %s\n", path)
	}
	fmt.Print(aiPrompt)
	return nil
}
