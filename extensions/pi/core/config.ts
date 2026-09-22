/*
 * Copyright (C) 2026 codesafe contributors
 * SPDX-License-Identifier: AGPL-3.0-or-later
 * See COPYING in the project root for the full license.
 */
// config.ts — read codesafe's user config (config.json) and per-project
// codesafe.yaml. Mirrors the Go CLI's layout so the CLI and this extension
// share one credential + one ruleset.

import * as fs from "node:fs";
import * as os from "node:os";
import * as path from "node:path";

/** Persisted user config (config.json under the OS user-config dir). */
export interface Config {
	api_key: string;
	lang?: string;
	scopes?: string[];
	allow_none?: boolean;
	cache?: boolean;
	todo?: Record<string, string>;
}

/** One codesafe.yaml rule (diff rule or commit-message rule). */
export interface Rule {
	id?: string;
	level?: string; // "error" | "warn"
	files?: string; // glob, e.g. "*.vue"; empty = applies to whole diff
	text?: string; // judgement instructions; may contain {{TODO}}
	pass?: string;
	fail?: string;
	on?: string; // commit_rules: subject|body|prefix|all
	todo_mode?: string; // off|loose|strict, per-rule override
}

/** Per-project codesafe.yaml shape. */
export interface ProjectConfig {
	scopes?: Record<string, string>;
	allow_none?: boolean;
	rules?: Rule[];
	commit_rules?: Rule[];
	prefix_conflict?: string; // keep_user | override
	todo_mode?: string; // off | loose | strict
}

/** Resolve the user config path: <userConfigDir>/codesafe/config.json. */
export function configPath(): string {
	// os.UserConfigDir() equivalent: %APPDATA% on Windows, ~/.config on *nix,
	// ~/Library/Application Support on macOS.
	const base =
		process.platform === "win32"
			? (process.env.APPDATA ?? path.join(os.homedir(), "AppData", "Roaming"))
			: process.platform === "darwin"
				? path.join(os.homedir(), "Library", "Application Support")
				: (process.env.XDG_CONFIG_HOME ?? path.join(os.homedir(), ".config"));
	return path.join(base, "codesafe", "config.json");
}

/** Load user config; missing file/key -> empty api_key (caller checks). */
export function loadConfig(): Config {
	try {
		const data = fs.readFileSync(configPath(), "utf8");
		const cfg = JSON.parse(data) as Config;
		return cfg;
	} catch {
		return { api_key: "" };
	}
}

/** Project TODO for a repo root, from user config's todo map. */
export function todoOf(cfg: Config, root: string): string {
	return cfg.todo?.[root] ?? "";
}

/** Normalized todo_mode; empty/illegal -> "loose". */
export function todoModeOf(s?: string): string {
	return s === "off" || s === "strict" ? s : "loose";
}

/** Load codesafe.yaml/.yml under dir; absent -> empty config. */
export function loadProject(dir: string): ProjectConfig {
	for (const name of ["codesafe.yaml", "codesafe.yml"]) {
		const p = path.join(dir, name);
		if (!fs.existsSync(p)) continue;
		try {
			return parseYaml(fs.readFileSync(p, "utf8")) as ProjectConfig;
		} catch {
			return {};
		}
	}
	return {};
}

/**
 * Minimal YAML parse for codesafe.yaml's flat structure (maps, string lists,
 * list-of-maps). Not a general parser — only what our schema needs.
 */
function parseYaml(src: string): ProjectConfig {
	// Use the `yaml` package when available (bundled with pi/omp host); fall
	// back to a tiny hand parser for the simple structure codesafe.yaml uses.
	try {
		// eslint-disable-next-line @typescript-eslint/no-require-imports
		const yaml = require("yaml") as { parse(s: string): unknown };
		return (yaml.parse(src) ?? {}) as ProjectConfig;
	} catch {
		return miniYaml(src);
	}
}

/** Tiny YAML subset parser: top-level keys, scalar, string[], list-of-maps. */
function miniYaml(src: string): ProjectConfig {
	const out: Record<string, unknown> = {};
	const lines = src.split("\n");
	let key = "";
	let list: Record<string, string>[] | null = null;
	let cur: Record<string, string> | null = null;
	for (const raw of lines) {
		const line = raw.replace(/\s+#.*$/, "");
		if (!line.trim()) continue;
		const top = /^([A-Za-z_]+):\s*(.*)$/.exec(line);
		if (top && !/^\s/.test(line)) {
			key = top[1];
			list = null;
			cur = null;
			const v = top[2].trim();
			if (v !== "") out[key] = stripQuotes(v);
			else out[key] = key === "rules" || key === "commit_rules" ? [] : {};
			if (key === "rules" || key === "commit_rules") list = out[key] as Record<string, string>[];
			continue;
		}
		const item = /^\s+-\s+(.*)$/.exec(line);
		if (item && list) {
			cur = {};
			list.push(cur);
			const kv = /^([A-Za-z_]+):\s*(.*)$/.exec(item[1]);
			if (kv) cur[kv[1]] = stripQuotes(kv[2].trim());
			continue;
		}
		const kv = /^\s+([A-Za-z_]+):\s*(.*)$/.exec(line);
		if (kv && cur && list) cur[kv[1]] = stripQuotes(kv[2].trim());
		else if (kv && key) (out[key] as Record<string, string>)[kv[1]] = stripQuotes(kv[2].trim());
	}
	return out as ProjectConfig;
}

function stripQuotes(v: string): string {
	if ((v.startsWith('"') && v.endsWith('"')) || (v.startsWith("'") && v.endsWith("'"))) {
		return v.slice(1, -1);
	}
	return v;
}
