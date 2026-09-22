/*
 * Copyright (C) 2026 codesafe contributors
 * SPDX-License-Identifier: AGPL-3.0-or-later
 * See COPYING in the project root for the full license.
 */
// codesafe delete：删目录/文件前，把项目根 + 目标路径 + 目录树喂模型判安全性，危险则中断。
package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"codesafe/internal/config"
	"codesafe/internal/scan"
	"codesafe/internal/typesafe"
)

// runDelete 删前安全检查：解析真实路径 → 生成限层目录树 → 模型判危险则中断，否则执行删除。
func runDelete(args []string) error {
	fs := flag.NewFlagSet("codesafe delete", flag.ContinueOnError)
	dir := fs.String("dir", ".", "project root directory")
	model := fs.String("model", "jev-latest", "TypeSafe model ID")
	yes := fs.Bool("yes", false, "skip the safety verdict and delete immediately")
	refresh := fs.Bool("refresh", false, "bypass the response cache and re-judge")
	check := fs.Bool("check", false, "run the verdict but do not delete or move")
	if err := fs.Parse(reorderFlags(args)); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) == 0 {
		return fmt.Errorf("delete needs a target path")
	}
	target := rest[0]

	cfg, _, root, err := setup(*dir)
	if err != nil {
		return err
	}
	if cfg.APIKey == "" {
		return config.ErrNoAPIKey
	}

	// 解析符号链接拿到真实绝对路径
	abs, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		real = abs
	}
	real = filepath.Clean(real)

	if *yes {
		return doDelete(real)
	}

	// 三档判定：safe 直接删 / sensitive 移到回收目录 / dangerous 中断
	client := newClient(cfg, *model, *refresh)
	verdict, prob, err := judgeDelete(context.Background(), client, root, real)
	if err != nil {
		return err
	}
	if *check {
		fmt.Printf("%s判定: %s (%.0f%%)%s %s\n", dim(), verdict, prob*100, reset(), real)
		return nil
	}
	switch verdict {
	case "dangerous":
		return fmt.Errorf("该删除过于危险，可能删除不该删的内容 (%.0f%%): %s", prob*100, real)
	case "sensitive":
		dest, err := quarantine(real)
		if err != nil {
			return err
		}
		fmt.Printf("%s敏感删除%s → 已移动到回收目录 %s (%.0f%%)\n", yellow(), reset(), dest, prob*100)
		return nil
	default: // safe
		fmt.Printf("%s删前判定安全 (%.0f%%)%s %s\n", green(), prob*100, reset(), real)
		return doDelete(real)
	}
}

// reorderFlags 把 args 里的 flag 项移到位置参数前面——Go flag 包遇首个非 flag 即停，
// 这样 "delete <path> --check" 也能解析 --check。已知取值的 flag(-dir/-model)连带其值前移。
func reorderFlags(args []string) []string {
	var flags, pos []string
	takesVal := map[string]bool{"-dir": true, "--dir": true, "-model": true, "--model": true, "-context": true, "--context": true, "-history": true, "--history": true}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") {
			flags = append(flags, a)
			// -x value 形式（非 -x=v）：把下一个也归 flag
			if takesVal[a] && !strings.Contains(a, "=") && i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
		} else {
			pos = append(pos, a)
		}
	}
	return append(flags, pos...)
}

// doDelete 执行实际删除。
func doDelete(path string) error {
	return os.RemoveAll(path)
}

// quarantine 把 path 移到系统临时目录下的 codesafe-deleted/<时间戳>/ 代替删除。
func quarantine(path string) (string, error) {
	base := filepath.Join(os.TempDir(), "codesafe-deleted", timeStamp())
	if err := os.MkdirAll(base, 0o755); err != nil {
		return "", err
	}
	dest := filepath.Join(base, filepath.Base(path))
	if err := os.Rename(path, dest); err != nil {
		// 跨盘 rename 失败 → copy + delete
		if err := copyTree(path, dest); err != nil {
			return "", err
		}
		return dest, os.RemoveAll(path)
	}
	return dest, nil
}

// timeStamp 返回紧凑时间戳用于回收目录名。
func timeStamp() string { return time.Now().Format("20060102-150405") }

// copyTree 递归复制文件/目录（跨盘移动的 fallback）。
func copyTree(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		data, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, data, info.Mode())
	}
	if err := os.MkdirAll(dst, info.Mode()); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if err := copyTree(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

// judgeDelete 生成限层目录树并问模型三档判定；超 max_tokens 则降层重试。
// 返回 (safe|sensitive|dangerous, 该档概率, err)。
func judgeDelete(ctx context.Context, client *typesafe.Client, root, target string) (string, float64, error) {
	for depth := 4; depth >= 1; depth-- {
		state := deleteState(root, target, depth)
		q := typesafe.Choice(
			`You are given a project root, a directory tree, and a target path to be deleted. Judge the deletion:

safe — obviously disposable: build/dist output, node_modules, vendor, caches, .tmp, generated artifacts, empty or clearly throwaway dirs.
sensitive — anything that might hold user-authored or recoverable-wanted content: documents, source files, configs, notes, unfamiliar data dirs. When in doubt between safe and sensitive, choose sensitive — it gets moved to a recover area, not destroyed.
dangerous — would destroy things outside the intended area: filesystem/drive root, user home, system dirs (Windows, Program Files, /etc, /usr), another project's files, or a symlink escaping the target. Must NOT delete or move.`,
			map[string]string{
				"safe":      "permanent delete is fine — disposable/generated content",
				"sensitive": "move to a recover area — possibly user data, err on the side of caution",
				"dangerous": "abort — would destroy system/user data or unrelated files",
			})
		ans, _, err := client.Evaluate(ctx, state, map[string]typesafe.Question{"del": q})
		if err != nil {
			if scan.IsTokenLimit(err) && depth > 1 {
				continue // 降层重试
			}
			return "", 0, err
		}
		a := ans["del"]
		return a.Choice, a.Probabilities[a.Choice], nil
	}
	return "", 0, fmt.Errorf("目录树过大，无法判定安全性")
}

// deleteState 组装 state：项目根 + 目标路径 + 目录树（限 depth 层）。
func deleteState(root, target string, depth int) string {
	var b strings.Builder
	b.WriteString("Project root: " + root + "\n")
	b.WriteString("Target to delete: " + target + "\n\n")
	b.WriteString("Directory tree of target (depth-limited):\n")
	b.WriteString(buildTree(target, depth))
	return b.String()
}

// buildTree 生成目录树文本：最多 depth 层，每层最多 60 条；到底显示 "N folders, M files"。
func buildTree(root string, depth int) string {
	var b strings.Builder
	b.WriteString(root + "\n")
	walkTree(&b, root, depth, 0, "")
	return b.String()
}

// walkTree 递归列目录。depth 剩余层数；到底统计子目录与文件数；每层最多 60 条。
func walkTree(b *strings.Builder, dir string, depth int, level int, indent string) {
	if depth <= 0 {
		d, f := countEntries(dir)
		if d > 0 || f > 0 {
			fmt.Fprintf(b, "%s  … %d folders, %d files\n", indent, d, f)
		}
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	const maxPer = 60
	for i, e := range entries {
		if i >= maxPer {
			fmt.Fprintf(b, "%s  … %d more entries\n", indent, len(entries)-maxPer)
			break
		}
		if e.IsDir() {
			fmt.Fprintf(b, "%s%s/\n", indent, e.Name())
			walkTree(b, filepath.Join(dir, e.Name()), depth-1, level+1, indent+"  ")
		} else {
			fmt.Fprintf(b, "%s%s\n", indent, e.Name())
		}
	}
}

// countEntries 数目录下的直接子目录数与文件数（不递归）。
func countEntries(dir string) (dirs, files int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, 0
	}
	for _, e := range entries {
		if e.IsDir() {
			dirs++
		} else {
			files++
		}
	}
	return dirs, files
}
