# codesafe

[中文](README.zh-CN.md)

Fast per-file safety and bug triage for a directory, powered by the
[TypeSafe](https://typesafe.ai) System One API.

Point it at a folder and it sends every text file through five judgment
questions — defects, security, data-access, dependency usage, and internal
logic — then prints one line per file with a probability and a Nerd Font
fill-circle gauge for each dimension, sorted by overall risk.

- Works on any directory; uses `git ls-files` inside a repository, walks the
  tree otherwise
- Credential files (`.env`, `*.pem`, `*.key`, …) are never sent
- Binary and oversized files are skipped
- Config and documentation files only get a format-validity check
- Concurrent, rate-limited calls; API key stored once in your user config dir

## Install

Download a binary from [Releases](../../releases) for your platform, or build
from source with Go 1.27+:

```sh
go build -o codesafe ./cmd/codesafe
```

## Usage

```sh
# First run prompts for your TypeSafe API key (console.typesafe.ai) and saves it
./codesafe

# Scan another directory
./codesafe --dir /path/to/project

# List what would be scanned without calling the API
./codesafe --dry-run

# Update the stored API key
./codesafe --config <new-key>

# Tune concurrency / request rate (defaults: 16 parallel, 20 req/s)
./codesafe --concurrency 32 --rps 30
```

Output columns: `缺陷` (bugs) · `安全` (security) · `数据` (data access) ·
`依赖` (dependency usage) · `逻辑` (internal logic). Config/doc files show a
single `格式` (format) column. Rows sort by the equal-weight sum of
probabilities, highest first.

## License

AGPL-3.0-or-later — see COPYING.
