/*
 * Copyright (C) 2026 codesafe contributors
 * SPDX-License-Identifier: AGPL-3.0-or-later
 * See COPYING in the project root for the full license.
 */
// scan 包：枚举待扫描文件（git 仓库用 git ls-files，否则遍历目录）、按文件名/后缀过滤和分类。
package scan

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// maxFileBytes 跳过超过该大小的文件，避免巨型产物进入 token 预算。
const maxFileBytes = 256 * 1024

// FileKind 决定一个文件跑哪组问题。
type FileKind int

const (
	KindCode   FileKind = iota // 代码及其他文本：跑全部五个维度
	KindConfig                 // 配置文件：只问格式是否正确
	KindDoc                    // 文档文件：只问格式是否正确
)

// skippedDirs 非 git 模式下遍历时跳过这些目录。
var skippedDirs = map[string]bool{
	".git": true, ".svn": true, ".hg": true,
	"node_modules": true, "vendor": true, ".venv": true, "venv": true,
	"__pycache__": true, ".idea": true, ".vscode": true,
	"target": true, "dist": true, "build": true, ".next": true, ".cache": true,
}

// secretNames 命中即跳过：可能含用户真实凭据/密钥的文件。
var secretNames = map[string]bool{
	".env": true, ".env.local": true, ".env.development": true, ".env.production": true,
	".envrc": true, ".netrc": true, ".npmrc": true, ".pypirc": true,
	"id_rsa": true, "id_dsa": true, "id_ecdsa": true, "id_ed25519": true,
	"credentials": true, "secrets.json": true, "secrets.yaml": true, "secrets.yml": true,
	"serviceaccount.json": true, "service-account.json": true,
}

// secretExts 命中即跳过：私钥/证书类后缀。
var secretExts = map[string]bool{
	".pem": true, ".key": true, ".p12": true, ".pfx": true,
	".jks": true, ".keystore": true, ".kdbx": true, ".asc": true, ".gpg": true,
}

// configExts 只做格式检查的配置/数据文件后缀。
var configExts = map[string]bool{
	".json": true, ".yaml": true, ".yml": true, ".toml": true, ".xml": true,
	".ini": true, ".conf": true, ".cfg": true, ".properties": true,
	".lock": true, ".csv": true, ".tsv": true,
}

// docExts 只做格式检查的文档后缀。
var docExts = map[string]bool{
	".md": true, ".markdown": true, ".txt": true, ".rst": true, ".adoc": true,
}

// IsSecret 判断路径名是否像真实凭据文件（匹配文件名和后缀）。
func IsSecret(rel string) bool {
	base := strings.ToLower(filepath.Base(rel))
	if secretNames[base] {
		return true
	}
	if strings.HasPrefix(base, ".env.") || strings.HasSuffix(base, ".env") {
		return true
	}
	return secretExts[strings.ToLower(filepath.Ext(base))]
}

// Classify 按后缀把文件分成代码 / 配置 / 文档。
func Classify(rel string) FileKind {
	ext := strings.ToLower(filepath.Ext(rel))
	if configExts[ext] {
		return KindConfig
	}
	if docExts[ext] {
		return KindDoc
	}
	return KindCode
}

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

// ListFiles 返回要扫描的相对路径列表：git 仓库用 git ls-files，否则遍历 root。
// subdir 非空时只保留该前缀下的文件；files 非空时强制只扫给定路径列表（忽略 git/secret 过滤）。
func ListFiles(root, subdir string, files []string) ([]string, error) {
	if len(files) > 0 {
		var out []string
		for _, f := range files {
			f = strings.TrimSpace(f)
			if f != "" {
				out = append(out, filepath.ToSlash(f))
			}
		}
		return out, nil
	}
	var list []string
	var err error
	if isGitRepo(root) {
		list, err = trackedFiles(root)
	} else {
		list, err = walkFiles(root)
	}
	if err != nil {
		return nil, err
	}
	if subdir != "" {
		prefix := strings.Trim(filepath.ToSlash(subdir), "/") + "/"
		var filtered []string
		for _, f := range list {
			if strings.HasPrefix(f, prefix) {
				filtered = append(filtered, f)
			}
		}
		return filtered, nil
	}
	return list, nil
}

// isGitRepo 判断 root 是否在 git 仓库内。
func isGitRepo(root string) bool {
	cmd := exec.Command("git", "-C", root, "rev-parse", "--is-inside-work-tree")
	return cmd.Run() == nil
}

// trackedFiles 返回 git 跟踪文件的相对路径（POSIX 分隔符）。
func trackedFiles(root string) ([]string, error) {
	cmd := exec.Command("git", "-C", root, "ls-files", "-z")
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("git ls-files 失败: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, fmt.Errorf("git ls-files 失败: %w", err)
	}
	var files []string
	for _, p := range bytes.Split(out, []byte{0}) {
		if len(p) > 0 {
			files = append(files, string(p))
		}
	}
	return files, nil
}

// walkFiles 遍历 root 下所有文件，跳过 skippedDirs 和符号链接目录。
func walkFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && skippedDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	return files, err
}

// ReadFile 读取 root/rel 的内容；二进制或超限文件返回 ok=false。
func ReadFile(root, rel string) (content string, ok bool, err error) {
	p := filepath.Join(root, filepath.FromSlash(rel))
	f, err := os.Open(p)
	if err != nil {
		return "", false, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return "", false, err
	}
	if info.Size() > maxFileBytes {
		return "", false, nil
	}
	br := bufio.NewReader(f)
	head, err := br.Peek(512)
	if err != nil && err.Error() != "EOF" {
		return "", false, err
	}
	if bytes.IndexByte(head, 0) >= 0 {
		return "", false, nil
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return "", false, err
	}
	return string(data), true, nil
}
