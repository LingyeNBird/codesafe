# codesafe-pi

A codesafe extension for [Pi](https://pi.dev) and [Oh My Pi](https://github.com/can1357/oh-my-pi) — commit-safety tooling driven by the TypeSafe System One API.

What it adds to the agent:

- **Intent hint** — each user message is classified (`modify` / `execute` / `answer` / `unclear`) and injected as a `[intent: …]` hint, plus a `[codesafe]` constraint block teaching the agent to use the codesafe tools instead of raw `git commit` / `rm`.
- **Soft delete reminder** — when a `bash`/`edit`/`write` tool call deletes something, a reminder to use `codesafe_delete` is appended.
- **Tools** — `codesafe_commit`, `codesafe_delete`, `codesafe_diff`, `codesafe_todo`, `codesafe_classify`, `codesafe_intent`.

## Install

```sh
pi  install npm:codesafe-pi     # Pi
omp install codesafe-pi         # Oh My Pi
```

On install, `postinstall` downloads the `codesafe` CLI binary for your platform so the `codesafe` command also works outside the agent. If your installer skips `postinstall` (or the download fails), the extension still works — only the CLI is absent.

## Prerequisites

The extension needs a TypeSafe API key in codesafe's `config.json`. Set it once with the standalone CLI:

```sh
codesafe config api_key=<your-key>
```

or write `{"api_key": "<key>"}` to `<userConfigDir>/codesafe/config.json` (`%APPDATA%/codesafe/config.json` on Windows). You can override the config path with the `CODESAFE_CONFIG` environment variable.

**Without a key** the extension degrades cleanly: the intent hint and constraint injection stay silent, and any `codesafe_*` tool call returns an "unavailable" error telling the agent to proceed with normal git/file operations and to ask you to configure the key.

## Project rules

The `codesafe_commit`/`codesafe_diff` tools check the diff against `rules` and `commit_rules` in a `codesafe.yaml` at your repo root — including `lines`-scoped rules that judge raw file line ranges (e.g. file headers) instead of the diff. See the main repo README for the schema.
