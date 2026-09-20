/*
 * Copyright (C) 2026 codesafe contributors
 * SPDX-License-Identifier: AGPL-3.0-or-later
 * See COPYING in the project root for the full license.
 */
// scan 包负责从 git 取 staged/working diff，并定义 commit 分类问题。
package scan

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// RepoRoot 返回 dir 所属仓库的顶层绝对路径；不是仓库时返回 ("", nil)。
func RepoRoot(dir string) (string, error) {
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			msg := strings.TrimSpace(string(ee.Stderr))
			if strings.Contains(msg, "not a git repository") {
				return "", nil
			}
			return "", fmt.Errorf("git rev-parse 失败: %s", msg)
		}
		return "", fmt.Errorf("未找到 git: %w", err)
	}
	abs, err := filepath.Abs(strings.TrimSpace(string(out)))
	if err != nil {
		return "", err
	}
	return abs, nil
}

// StagedDiff 返回 staged 区的 diff（git diff --cached）。
func StagedDiff(dir string) (string, error) {
	return gitDiff(dir, "--cached")
}

// WorktreeDiff 返回工作区相对 HEAD 的全部 diff（git diff HEAD，含暂存+未暂存）。
func WorktreeDiff(dir string) (string, error) {
	return gitDiff(dir, "HEAD")
}

// UnstagedDiff 返回未暂存部分的 diff（git diff，不含已 add 的）。
func UnstagedDiff(dir string) (string, error) {
	return gitDiff(dir)
}

// CommitDiff 返回指定 commit 的 diff（git show <rev>，含 subject 行）。
func CommitDiff(dir, rev string) (string, error) {
	cmd := exec.Command("git", "-C", dir, "show", rev, "--format=%s")
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("git show 失败: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return "", err
	}
	return string(out), nil
}

// gitDiff 执行 git diff 并返回输出。
func gitDiff(dir string, args ...string) (string, error) {
	argv := append([]string{"-C", dir, "diff"}, args...)
	cmd := exec.Command("git", argv...)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("git diff 失败: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return "", err
	}
	return string(out), nil
}
