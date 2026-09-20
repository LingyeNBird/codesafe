/*
 * Copyright (C) 2026 codesafe contributors
 * SPDX-License-Identifier: AGPL-3.0-or-later
 * See COPYING in the project root for the full license.
 */
// scan 包定义 commit 分类所需的 type/scope choice 问题。
package scan

import "codesafe/internal/typesafe"

// TypeCriteria 是 conventional-commit 类型选项 -> 说明。
var TypeCriteria = map[string]string{
	"feat":     "adds a new feature or capability",
	"fix":      "fixes a bug or incorrect behavior",
	"refactor": "restructures code without changing behavior",
	"perf":     "improves performance without changing behavior",
	"style":    "formatting/whitespace only, no logic change",
	"test":     "adds or changes tests only",
	"docs":     "documentation or comments only",
	"build":    "build system, dependencies, or packaging",
	"ci":       "CI/CD pipeline, workflows, automation",
	"chore":    "maintenance, version bumps, routine tasks",
	"revert":   "reverts a previous commit",
	"security": "fixes a security vulnerability or hardens security",
}

// FormScopes 是形态域 scope（跟项目结构组成有关，可被筛选）：一个项目可能有也可能没有这些层。
var FormScopes = map[string]string{
	"app":    "application code",
	"ui":     "user interface / frontend",
	"api":    "API / backend endpoints",
	"db":     "database / data layer",
	"cli":    "command-line interface",
	"config": "configuration / settings",
	"repo":   "repo-wide / cross-cutting",
}

// UniversalScopes 是横切/通用 scope（任何项目都可能有这类 commit，恒保留、不参与筛选）。
var UniversalScopes = map[string]string{
	"docs":    "documentation",
	"test":    "tests",
	"build":   "build / packaging",
	"ci":      "CI / automation",
	"deps":    "dependency updates",
	"release": "release / versioning",
}

// ScopeCriteria 是默认 scope 全集（形态域 + 通用域 + none）。
var ScopeCriteria = func() map[string]string {
	m := map[string]string{"none": "no single area — cross-cutting or repo-wide change"}
	for k, v := range FormScopes {
		m[k] = v
	}
	for k, v := range UniversalScopes {
		m[k] = v
	}
	return m
}()

// Questions 返回 commit 分类的问题。scopes 为空用内置集；
// allowNone 为真时保证集里含 "none"（跨模块改动可无 scope）。
func Questions(scopes map[string]string, allowNone bool) map[string]typesafe.Question {
	if len(scopes) == 0 {
		scopes = ScopeCriteria
	}
	if allowNone {
		scopes = copyWithNone(scopes)
	}
	return map[string]typesafe.Question{
		"type":  typesafe.Choice("This is a git commit (subject line + diff). Which conventional-commit type best describes the change?", TypeCriteria),
		"scope": typesafe.Choice("Which project scope does this change mainly affect?", scopes),
		"breaking": typesafe.Noul(
			"Does this change break an EXTERNAL contract that downstream consumers actually depend on? Breaking means: removing/renaming a public or exported API, changing a function signature or return type others call, removing or renaming a CLI flag/subcommand, changing a config file format or removing a config key, changing a documented output format (stdout/stderr structure others parse), or changing observable behavior that callers rely on. NOT breaking: internal refactors invisible to callers, renaming private/unexported symbols, changing comments/formatting, additive changes (new flag, new field, new endpoint), bug fixes that restore intended behavior, or changes to code no external party calls. When in doubt about whether anyone depends on it, it is not breaking.",
			map[string]string{
				"true":  "The change removes, renames, or alters a public/CLI/config/output contract that external users or callers depend on, in a way that would break them.",
				"false": "The change is internal, additive, a bug fix, or touches only code no external consumer depends on.",
			}),
	}
}

// copyWithNone 复制 scope 集并保证含 "none"（跨模块/仓库级改动无单一 scope 时使用）。
func copyWithNone(m map[string]string) map[string]string {
	out := make(map[string]string, len(m)+1)
	for k, v := range m {
		out[k] = v
	}
	if _, ok := out["none"]; !ok {
		out["none"] = "no single area — cross-cutting or repo-wide change"
	}
	return out
}

// ScopesFromNames 把 scope 名列表转成 名->描述 的 map；无描述时用名字本身。
func ScopesFromNames(names []string) map[string]string {
	out := map[string]string{}
	for _, n := range names {
		out[n] = n
	}
	return out
}
