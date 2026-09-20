# codesafe

Suggest a **conventional-commit `type(scope)`** for your staged or worktree diff, using the [TypeSafe System One API](https://docs.typesafe.ai). It reads your diff and picks a commit type and scope — so AI agents and scripts get a consistent `type(scope)` suggestion without hand-classifying.

[中文文档](README.zh-CN.md)

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

## Use

```sh
# First run prompts for your TypeSafe API key (console.typesafe.ai) and saves it
# Classify the staged diff (or worktree diff if nothing is staged)
./codesafe

# Classify a specific commit
./codesafe --source 5b5e054

# Classify only the worktree (unstaged) changes
./codesafe --source worktree

# Change output language
./codesafe --config lang=zh       # Chinese
./codesafe --config lang=en       # English

# Persistent config
./codesafe --config api_key=<key>
./codesafe --config scopes=cli|server|web|docs   # your scope names

# Temporary overrides for one run only (not saved)
./codesafe --set api_key=<key>
./codesafe --set lang=en
```

The output is the suggested `type`, `scope`, each with a confidence score and alternatives, plus a combined `type(scope)` suggestion.

## Project scopes

The tool ships a built-in scope set (`app`, `ui`, `api`, `cli`, `docs`, …). Override it per project so suggestions match your repo's conventions.

**Repo-committed** (shared with collaborators) — create `codesafe.yaml` in the repo root:

```yaml
allow_none: false        # optional: forbid scope=none (require a concrete scope)
scopes:
  - server
  - web: frontend UI
  - installer
  - cli
```

A bare `- name` uses the name as its own description; `- name: desc` gives the model a hint.

**User-level** (this machine only, not committed):

```sh
./codesafe --config scopes=server|web|cli|installer
./codesafe --config nonescope=false     # forbid the none scope
```

Priority: `codesafe.yaml` > `--config scopes` > built-in set.

### The `none` scope

The scope set always offers `none` ("cross-cutting change, no single area") — pick it for repo-wide changes that fit no single scope; the suggestion then has no parens (e.g. `feat!:`). Disable it with `allow_none: false` in `codesafe.yaml`, or `./codesafe --config nonescope=false`, if your project requires every commit to carry a concrete scope.

## API key

Get one at [console.typesafe.ai](https://console.typesafe.ai). First run prompts and saves it to the user config dir (`0600`).

## License

[AGPL-3.0-or-later](COPYING)
