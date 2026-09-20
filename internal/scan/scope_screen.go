/*
 * Copyright (C) 2026 codesafe contributors
 * SPDX-License-Identifier: AGPL-3.0-or-later
 * See COPYING in the project root for the full license.
 */
// scan 包：项目 scope 筛选——对形态域 scope 判"它是这个项目里有边界的子集吗"，通用域恒保留。
package scan

import (
	"context"
	"fmt"
	"hash/fnv"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"codesafe/internal/typesafe"
)

// universalScopeNames 恒保留的通用 scope（横切关注点，任何项目都可能有这类 commit）。
var universalScopeNames = []string{"docs", "test", "build", "ci", "deps", "release"}

// formScopeNames 参与筛选的形态域 scope（跟项目结构组成有关）。
var formScopeNames = []string{"app", "ui", "api", "db", "cli", "config", "repo"}

// DirFingerprint 返回项目目录结构的指纹——目录集变了筛选就该重跑。
func DirFingerprint(root string) string {
	dirs, conv, err := repoDirs(root)
	if err != nil {
		return ""
	}
	h := fnv.New32a()
	for _, d := range dirs {
		h.Write([]byte(d))
		h.Write([]byte{0})
	}
	for _, c := range conv {
		h.Write([]byte(c))
		h.Write([]byte{0})
	}
	return fmt.Sprintf("%x", h.Sum32())
}

// ScreenScopes 用模型筛形态域 scope：喂项目目录树+README，判每个 scope 是否是有边界的子集。
// 返回最终 scope 集 = 通过筛选的形态域 + 全部通用域 + none。
// threshold 默认 0.5。
func ScreenScopes(ctx context.Context, client *typesafe.Client, root string, threshold float64) (map[string]string, error) {
	if threshold <= 0 {
		threshold = 0.5
	}
	state, err := projectState(root)
	if err != nil {
		return nil, err
	}
	qs := map[string]typesafe.Question{}
	for _, s := range formScopeNames {
		qs[s] = screenQuestion(s)
	}
	answers, _, err := client.Evaluate(ctx, state, qs)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, s := range formScopeNames {
		if answers[s].Noul >= threshold {
			out[s] = FormScopes[s]
		}
	}
	// 通用域恒保留
	for _, s := range universalScopeNames {
		out[s] = UniversalScopes[s]
	}
	return out, nil
}

// screenQuestion 生成"该 scope 是否为此项目有边界子集"的 noul。
func screenQuestion(s string) typesafe.Question {
	ins := fmt.Sprintf(`This is a software project (file tree + README above). Decide whether %q is a useful conventional-commit scope for THIS project.

A scope names a DOMAIN/FEATURE area (auth, db, installer, parser, a specific module). Ask two questions:

A) Does the project actually contain a %q area? Structural hints: cmd/, cli/, argv-parsing main → cli; db/, database/, migrations/, SQL → db; .github/workflows/, .gitlab-ci.yml → ci; Dockerfile, build/, Makefile → build; config/settings module → config; HTTP/RPC server layer → api; UI/frontend dir → ui.

B) Is %q merely the project's own deliverable form — i.e. the whole project IS a %q and nothing else? Only reject on this when the project's product itself is %q: a tool that is a command-line program end-to-end → drop 'cli'; a repo that is only documentation → drop 'docs'; a pure API-only service → drop 'api'. Do NOT reject just because a %q directory exists — an internal/cli/ package inside a larger CLI is still just 'the entrypoint', not a domain, so drop 'cli'; but 'ci'/'docs'/'release'/'build' are almost always genuine sub-areas (they're never the product itself) → keep them when the structural hint exists.

YES = %q names a real functional sub-area. NO = the project has no %q area, or %q IS the whole product's form.`, s, s, s, s, s, s, s, s, s)
	return typesafe.Noul(ins, map[string]string{
		"true":  fmt.Sprintf("%q is a distinct functional area forming a proper subset of the project.", s),
		"false": fmt.Sprintf("The project has no %q area, OR %q is literally the whole product's own form.", s, s),
	})
}

// projectState 组装 state：目录树（git ls-files 的目录部分 + 约定文件）+ README。
func projectState(root string) (string, error) {
	var b strings.Builder
	b.WriteString("Directories in this repo:\n")
	dirs, conv, err := repoDirs(root)
	if err != nil {
		return "", err
	}
	b.WriteString(strings.Join(dirs, "\n"))
	if len(conv) > 0 {
		b.WriteString("\nConventional files: " + strings.Join(conv, ", "))
	}
	if r := readReadme(root); r != "" {
		b.WriteString("\n\nREADME:\n" + r)
	}
	return b.String(), nil
}

// repoDirs 返回仓库目录路径集（最多两级深）与约定文件名集。
func repoDirs(root string) (dirs []string, conv []string, err error) {
	out, err := exec.Command("git", "-C", root, "ls-files").Output()
	if err != nil {
		return walkDirs(root)
	}
	set := map[string]bool{}
	for _, f := range strings.Split(string(out), "\n") {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		idx := strings.LastIndex(f, "/")
		d, base := ".", f
		if idx >= 0 {
			d, base = f[:idx], f[idx+1:]
		}
		if d != "." {
			parts := strings.Split(d, "/")
			if len(parts) > 2 {
				d = parts[0] + "/" + parts[1]
			}
			set[d] = true
		}
		switch strings.ToLower(base) {
		case "readme.md", "go.mod", "package.json", "dockerfile", "makefile", "cargo.toml", "pyproject.toml", "cmakelists.txt", "go.work", "pnpm-workspace.yaml":
			conv = append(conv, base)
		}
	}
	for d := range set {
		dirs = append(dirs, d)
	}
	sortStrings(dirs)
	if len(dirs) > 300 {
		dirs = append(dirs[:300], "...")
	}
	return dirs, conv, nil
}

// walkDirs 非 git 目录的遍历：跳过无意义目录，收集目录路径与约定文件。
func walkDirs(root string) (dirs []string, conv []string, err error) {
	skip := map[string]bool{".git": true, "node_modules": true, "vendor": true, ".venv": true, "venv": true, "__pycache__": true, ".idea": true, ".vscode": true, "target": true, "dist": true, "build": false, ".next": true, ".cache": true}
	set := map[string]bool{}
	err = filepath.Walk(root, func(p string, info os.FileInfo, e error) error {
		if e != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if info.IsDir() {
			if skip[info.Name()] {
				return filepath.SkipDir
			}
			parts := strings.Split(rel, "/")
			if len(parts) > 2 {
				rel = parts[0] + "/" + parts[1]
			}
			set[rel] = true
			return nil
		}
		switch strings.ToLower(info.Name()) {
		case "readme.md", "go.mod", "package.json", "dockerfile", "makefile", "cargo.toml", "pyproject.toml", "cmakelists.txt", "go.work", "pnpm-workspace.yaml":
			conv = append(conv, info.Name())
		}
		return nil
	})
	for d := range set {
		dirs = append(dirs, d)
	}
	sortStrings(dirs)
	if len(dirs) > 300 {
		dirs = append(dirs[:300], "...")
	}
	return dirs, conv, err
}

// readReadme 读根目录 README（截断 4000 字符）。
func readReadme(root string) string {
	for _, n := range []string{"README.md", "README.zh-CN.md", "README.txt", "README"} {
		if b, err := os.ReadFile(filepath.Join(root, n)); err == nil {
			s := string(b)
			if len(s) > 4000 {
				s = s[:4000]
			}
			return s
		}
	}
	return ""
}

// sortStrings 简单插入排序。
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
