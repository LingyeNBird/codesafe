/*
 * Copyright (C) 2026 codesafe contributors
 * SPDX-License-Identifier: AGPL-3.0-or-later
 * See COPYING in the project root for the full license.
 */
// codesafe todo：设置/查看/清空当前项目的任务描述。任务在 commit/diff 时参与判定。
package cli

import (
	"flag"
	"fmt"
	"strings"

	"codesafe/internal/config"
	"codesafe/internal/scan"
)

// runTodo 处理 codesafe todo：<text> 设置；无参=查看；--clear=清空。
func runTodo(args []string) error {
	fs := flag.NewFlagSet("codesafe todo", flag.ContinueOnError)
	dir := fs.String("dir", ".", "git repo directory")
	clear := fs.Bool("clear", false, "clear the current todo")
	if err := fs.Parse(args); err != nil {
		return err
	}
	root, err := scan.RepoRoot(*dir)
	if err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	text := strings.TrimSpace(strings.Join(fs.Args(), " "))
	switch {
	case *clear:
		if err := config.SetTodo(&cfg, root, ""); err != nil {
			return err
		}
		fmt.Println("todo cleared")
		return nil
	case text == "":
		cur := config.TodoOf(cfg, root)
		if cur == "" {
			fmt.Println("no todo set — use `codesafe todo <task>`")
		} else {
			fmt.Println(cur)
		}
		return nil
	default:
		if err := config.SetTodo(&cfg, root, text); err != nil {
			return err
		}
		fmt.Printf("todo set: %s\n", text)
		return nil
	}
}
