# codesafe Pi/OMP extension

A single TypeScript extension that runs in both **Pi** (`@earendil-works/pi-coding-agent`) and **Oh My Pi** (OMP). It adds codesafe's commit-safety and intent-awareness to the agent.

## What it does

- **Intent hint** — each user prompt is classified (modify / execute / answer / unclear) and a short `[intent: …]` tag is injected for the model as a hint. `unclear` tells the model to state its reading of the user's intent in one sentence before acting.
- **System-prompt constraint** — tells the model to use `codesafe_commit` / `codesafe_delete` instead of raw `git commit` / `rm` / delete-by-edit.
- **Soft reminder** — when a `bash`/`edit`/`write` tool result shows a delete-like operation, a note is appended suggesting `codesafe_delete` (non-blocking).
- **Tools** — `codesafe_commit`, `codesafe_delete`, `codesafe_diff`, `codesafe_todo`, `codesafe_classify`, `codesafe_intent`.

## Layout

```
extensions/
  shared/    host-agnostic core (TypeSafe client, rules, intent, delete, git, config)
  pi/        the extension entry (index.ts) + package manifest
```

The same `index.ts` loads in Pi and OMP — OMP's legacy-pi compatibility layer
rewrites the `@earendil-works/*` / `@sinclair/typebox` imports onto the host.

## Install

Point the host at the `pi/` directory:

- **Pi**: copy/symlink into `<project>/.pi/extensions/` or `~/.pi/agent/extensions/`
- **OMP**: `<project>/.omp/extensions/` or `~/.omp/agent/extensions/`, or list it in `config.yml` under `extensions:`

The extension reads codesafe's user config (`<userConfigDir>/codesafe/config.json`)
for the TypeSafe API key — configure it once with the `codesafe` CLI and both share it.
