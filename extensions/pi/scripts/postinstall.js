#!/usr/bin/env node
/*
 * Copyright (C) 2026 codesafe contributors
 * SPDX-License-Identifier: AGPL-3.0-or-later
 * See COPYING in the project root for the full license.
 */
// postinstall — download the platform-specific codesafe binary from GitHub
// Releases into bin/. Silent no-op on any failure (unsupported platform,
// offline, release missing) so `npm install` never breaks over the CLI.
import { chmodSync, createWriteStream, existsSync, mkdirSync, readFileSync } from "node:fs";
import { get } from "node:https";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const pkg = JSON.parse(readFileSync(join(root, "package.json"), "utf8"));

// Map (platform, arch) → release asset name, matching .github/workflows/release.yml.
const ASSETS = {
	"win32:x64": "codesafe-windows-amd64.exe",
	"linux:x64": "codesafe-linux-amd64",
	"darwin:arm64": "codesafe-darwin-arm64",
	"darwin:x64": "codesafe-darwin-amd64",
};

const asset = ASSETS[`${process.platform}:${process.arch}`];
if (!asset) process.exit(0); // unsupported platform — extension still works, no CLI

const url = `https://github.com/LingyeNBird/codesafe/releases/download/v${pkg.version}/${asset}`;
const outDir = join(root, "bin");
const outFile = join(outDir, process.platform === "win32" ? "codesafe.exe" : "codesafe");

try {
	mkdirSync(outDir, { recursive: true });
	await download(url, outFile);
	if (process.platform !== "win32") chmodSync(outFile, 0o755);
} catch {
	process.exit(0); // never fail the install over a missing CLI binary
}

/** Download url→dest following redirects; rejects on non-2xx or stream error. */
function download(url, dest, redirects = 5) {
	return new Promise((resolve, reject) => {
		get(url, res => {
			if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) {
				res.resume();
				if (redirects <= 0) return reject(new Error("too many redirects"));
				return resolve(download(res.headers.location, dest, redirects - 1));
			}
			if (res.statusCode !== 200) {
				res.resume();
				return reject(new Error(`HTTP ${res.statusCode}`));
			}
			const f = createWriteStream(dest);
			res.pipe(f);
			f.on("finish", () => f.close(resolve));
			f.on("error", reject);
		}).on("error", reject);
	});
}
