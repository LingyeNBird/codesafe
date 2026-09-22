#!/usr/bin/env node
/*
 * Copyright (C) 2026 codesafe contributors
 * SPDX-License-Identifier: AGPL-3.0-or-later
 * See COPYING in the project root for the full license.
 */
// codesafe launcher shim — resolves the platform binary placed next to this
// file by postinstall and forwards argv + exit code. If the binary is missing
// (e.g. the installer skipped postinstall), it points the user at a fix.
import { spawnSync } from "node:child_process";
import { existsSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const name = process.platform === "win32" ? "codesafe.exe" : "codesafe";
const bin = join(here, name);

if (!existsSync(bin)) {
	console.error(
		"codesafe CLI binary not installed (postinstall did not run or failed).\n" +
			"  Fix: `npm rebuild codesafe-pi` inside node_modules, or download the binary for your " +
			"platform from https://github.com/LingyeNBird/codesafe/releases and place it as " +
			bin,
	);
	process.exit(1);
}

const r = spawnSync(bin, process.argv.slice(2), { stdio: "inherit" });
process.exit(r.status ?? (r.signal ? 1 : 0));
