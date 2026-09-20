/*
 * Copyright (C) 2026 codesafe contributors
 * SPDX-License-Identifier: AGPL-3.0-or-later
 * See COPYING in the project root for the full license.
 */
// codesafe CLI 入口：解析参数、加载配置并运行扫描流程。
package main

import (
	"fmt"
	"os"

	"codesafe/internal/cli"
)

// main 执行 CLI 并以非零码报告运行错误。
func main() {
	if err := cli.Run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "codesafe:", err)
		os.Exit(1)
	}
}
