<div align="center">

<img src="assets/icon-256.png" width="120" alt="codesafe">

# codesafe

**A commit-safety CLI for AI-assisted workflows**

Classify your diff into a conventional-commit `type(scope)`, guard `git commit` and file deletion behind model-judged rules, and enforce per-project conventions from a `codesafe.yaml`.

[![Release](https://img.shields.io/github/v/release/LingyeNBird/codesafe?style=flat-square)](https://github.com/LingyeNBird/codesafe/releases)
[![License](https://img.shields.io/badge/license-AGPL--3.0-blue?style=flat-square)](COPYING)
[![Powered by TypeSafe](https://img.shields.io/badge/powered%20by-TypeSafe%20System%20One-2fe08a?style=flat-square)](https://docs.typesafe.ai)

[中文文档](README.zh-CN.md) · [Releases](https://github.com/LingyeNBird/codesafe/releases) · [API Docs](https://docs.typesafe.ai)

</div>

---

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/LingyeNBird/codesafe/main/install.sh | bash
```

Or grab a binary from [Releases](https://github.com/LingyeNBird/codesafe/releases):

| Platform | File |
|---|---|
| Windows (x64) | `codesafe-windows-amd64.exe` |
| Linux (x64) | `codesafe-linux-amd64` |
| macOS (Apple Silicon) | `codesafe-darwin-arm64` |
| macOS (Intel) | `codesafe-darwin-amd64` |

| Command | What it does |
|---|---|
| `codesafe` | Suggest a `type(scope)` for the staged/worktree diff (single line, pipe into `git commit -m`). |
| `codesafe commit -m ...` | Validate message + code rules, generate the prefix, run `git commit`. |
| `codesafe diff` | Check the diff against `codesafe.yaml` `rules`; report violations. Oversized diffs are truncated and flagged (split the commit for full coverage); `--pass <glob>` exempts files from judgement. |
| `codesafe delete <path>` | Judge safety before deleting — `safe` deletes, `sensitive` quarantines, `dangerous` aborts. |
| `codesafe init` | Write a commented `codesafe.yaml` template + print an AI prompt for filling it. |
| `codesafe agent` | Print a rules block to paste into `AGENTS.md` / `CLAUDE.md` so AI agents use codesafe. |
| `codesafe todo <task>` | Set the current task; `commit`/`diff` verify the diff implements it. |

### Classify (default)

```sh
./codesafe                    # staged diff, else worktree → prints e.g. "feat(cli)!"
./codesafe --detail           # full percentages + token/cost stats
./codesafe --source worktree  # unstaged only; --source <sha> for a commit
./codesafe --source staged    # staged only
```

### Guarded commit

```sh
./codesafe commit -m "重构为 conventional-commit 分类器" -m "- 新增 scope 筛选"
```

Checks `commit_rules` (e.g. subject must be Chinese), then `rules` against the staged diff, generates the prefix, and runs `git commit`. A self-supplied `type(scope):` prefix in the first `-m` is honored or replaced per `prefix_conflict` (`keep_user` / `override`).

### Guarded delete

```sh
./codesafe delete build/          # judge then act
./codesafe delete build/ --check  # verdict only, no delete/move
./codesafe delete tmp/ --yes      # skip the verdict
```

Verdicts: `safe` → `os.RemoveAll`; `sensitive` → moved to a quarantine dir under the system temp dir (recoverable); `dangerous` → aborts.

### Task TODO

```sh
./codesafe todo "add OAuth login"   # record the current task for this repo
./codesafe todo                     # show it
./codesafe todo --clear             # clear it (also auto-cleared on successful commit)
```

When a todo is set, `codesafe commit`/`codesafe diff` additionally ask the model whether the diff implements that task — a mismatch aborts the commit. Rules may embed `{{TODO}}` in `text`/`pass`/`fail` to reference the current task; `todo_mode` (`off`|`loose`|`strict`, default `loose`) controls whether a `{{TODO}}` rule requires a set todo — `strict` aborts the commit when no todo is set, and can be set per-rule.

### Project config — `codesafe.yaml`

```yaml
lang: zh
allow_none: true
prefix_conflict: override

scopes:                    # this project's scope vocabulary
  cli:    "command-line entrypoint / subcommands"
  api:    "HTTP/RPC layer"
  ci:     "CI / release workflows"

rules:                     # code rules — checked against the diff
  - id: vue-css-split
    level: error           # error aborts · warn only prints
    files: "*.vue"         # only asked when the diff touches matching files
    text: .vue files must not contain <style> blocks
    pass: all .vue styles live in external .css files
    fail: a .vue file still contains an inline <style> block

commit_rules:              # rules about the commit message itself
  - id: subject-zh
    on: subject            # subject | body | prefix | all — prefix only applies under prefix_conflict=keep_user
    text: the subject must be in Chinese
    pass: the subject is primarily Chinese
    fail: the subject is not Chinese
```

If `scopes` is absent, codesafe screens a built-in vocabulary against your directory tree (a scope like `cli` is dropped when the whole project *is* a CLI) and caches the result per project.

### AI agents

```sh
./codesafe agent           # prints a rules block for AGENTS.md / CLAUDE.md
```

Paste the output into your agent rules file so the AI uses `codesafe delete`/`commit`/`diff` instead of raw `rm`/`git commit`.

## Config

```sh
./codesafe --config api_key=<key>          # save TypeSafe key (console.typesafe.ai)
./codesafe --config lang=zh|en
./codesafe --config scopes=cli|server|web
./codesafe --config nonescope=false
./codesafe --config cache=false             # disable the response cache (bbolt, keyed by request SHA-256, 1h TTL)
./codesafe --set lang=en                   # one-run override, not saved
./codesafe diff --refresh                  # bypass cache and re-judge once
./codesafe diff --pass 'dist/**'          # skip files matching a glob (repeatable) — useful when a single file is still too large after splitting
```

Priority: `codesafe.yaml` > `--config` > built-in / screened defaults.

## API key

Get one at [console.typesafe.ai](https://console.typesafe.ai). First run prompts and saves it to the user config dir (`0600`).

## License

[AGPL-3.0-or-later](COPYING)

## Links

- [linux.do](https://linux.do)
