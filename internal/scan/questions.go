/*
 * Copyright (C) 2026 codesafe contributors
 * SPDX-License-Identifier: AGPL-3.0-or-later
 * See COPYING in the project root for the full license.
 */
// scan 包定义逐文件安全/缺陷判断的五个 noul 问题、收集 git 跟踪文件、并发调用 TypeSafe 并聚合结果。
package scan

import "codesafe/internal/typesafe"

// Dim 描述一个判断维度：问题 id、显示标签和聚合权重。
type Dim struct {
	ID     string
	Label  string
	Weight float64
}

// Dims 是扫描使用的维度表，顺序即输出列顺序。
var Dims = []Dim{
	{ID: "bug", Label: "缺陷", Weight: 1},
	{ID: "security", Label: "安全", Weight: 1},
	{ID: "data_ops", Label: "数据", Weight: 1},
	{ID: "dependency", Label: "依赖", Weight: 1},
	{ID: "logic", Label: "逻辑", Weight: 1},
}

// FormatDim 是配置/文档文件唯一要跑的维度。
var FormatDim = Dim{ID: "format", Label: "格式", Weight: 1}

// FormatQuestion 判断文件格式是否合法、可被常规解析器读取。
func FormatQuestion(kind FileKind) typesafe.Question {
	subject := "configuration or data file"
	if kind == KindDoc {
		subject = "documentation file"
	}
	return typesafe.Noul(
		"Is this "+subject+" malformed — would a standard parser or reader reject it, or is it structurally broken (unclosed delimiters, truncated content, invalid syntax for its declared format)?",
		map[string]string{
			"true":  "The file has a concrete format defect that a parser would reject or a reader would find broken.",
			"false": "The file is well-formed for its format; stylistic or semantic issues do not count.",
		},
	)
}

// Questions 返回五个维度的 noul 问题，instructions 用英文写以匹配模型的主训练语言，
// criteria 把 true/false 边界写窄以避免字面化误读。
func Questions() map[string]typesafe.Question {
	q := map[string]typesafe.Question{}
	q["bug"] = typesafe.Noul(
		"Does this file contain code defects that would cause incorrect behavior or a crash at runtime?",
		map[string]string{
			"true":  "The file contains at least one concrete defect that would produce wrong output, corrupt state, or crash.",
			"false": "The file's behavior matches what it is written to do; style issues and possible improvements are not bugs.",
		},
	)
	q["security"] = typesafe.Noul(
		"Does this file contain a security vulnerability such as injection, hardcoded credentials, insecure deserialization, missing authorization checks, or leaking sensitive data?",
		map[string]string{
			"true":  "There is a concrete security weakness that an attacker or misuse could exploit.",
			"false": "No exploitable weakness; theoretical hardening suggestions do not count.",
		},
	)
	q["data_ops"] = typesafe.Noul(
		"Does this file contain a data-access defect such as building SQL by string concatenation with untrusted input, missing transactions where atomicity is required, or unparameterized queries?",
		map[string]string{
			"true":  "The file performs database or persistent-data operations with a concrete correctness or injection defect.",
			"false": "The file has no data-access defect, or performs no data-access operations at all.",
		},
	)
	q["dependency"] = typesafe.Noul(
		"Assuming every imported module and package exists, is there a problem in how this file uses its imports — wrong usage versus the library's conventional API, importing without use, or calling symbols the library does not provide?",
		map[string]string{
			"true":  "An import is used in a way inconsistent with its conventional API, or a needed import is absent.",
			"false": "Import usage is consistent with conventional APIs; do not judge whether the module exists.",
		},
	)
	q["logic"] = typesafe.Noul(
		"Within this single file, is there self-contradictory logic: conditions that are always true or always false, unreachable branches or dead code, contradictory assignments, or clear off-by-one boundary errors? Judge only what is visible inside this file.",
		map[string]string{
			"true":  "The file's internal logic contradicts itself in a way a reader can point to.",
			"false": "The file is internally consistent; do not speculate about other files.",
		},
	)
	return q
}
