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

Linux / macOS one-liner (installs to `~/.local/bin`, adds PATH + `cs` alias to
your shell rc):

```sh
curl -fsSL https://raw.githubusercontent.com/LingyeNBird/codesafe/main/install.sh | bash
```

Or download a binary from [Releases](../../releases) for your platform, or build
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

# Update stored settings
./codesafe --config api_key=<new-key>
./codesafe --config lang=zh      # Chinese labels (default en)
./codesafe --config glyph=emoji  # emoji gauge if your terminal lacks Nerd Font
```

Output columns: `bug` · `security` · `data` · `deps` · `logic` (Chinese:
缺陷/安全/数据/依赖/逻辑). Config/doc files show a single `format` column.
Rows sort by the equal-weight sum of probabilities, highest first.

## License

AGPL-3.0-or-later — see COPYING.
