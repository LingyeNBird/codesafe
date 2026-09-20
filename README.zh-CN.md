<div align="center">

<img src="assets/icon-256.png" width="120" alt="codesafe">

# codesafe

**面向 AI 辅助工作流的 commit 安全 CLI**

把 diff 分类成 conventional-commit `type(scope)`，用模型判定的规则守护 `git commit` 与文件删除，并通过 `codesafe.yaml` 强制项目级约定。

[![Release](https://img.shields.io/github/v/release/LingyeNBird/codesafe?style=flat-square)](https://github.com/LingyeNBird/codesafe/releases)
[![License](https://img.shields.io/badge/license-AGPL--3.0-blue?style=flat-square)](COPYING)
[![Powered by TypeSafe](https://img.shields.io/badge/powered%20by-TypeSafe%20System%20One-2fe08a?style=flat-square)](https://docs.typesafe.ai)

[English](README.md) · [Releases](https://github.com/LingyeNBird/codesafe/releases) · [API 文档](https://docs.typesafe.ai)

</div>

---

## 安装

```sh
curl -fsSL https://raw.githubusercontent.com/LingyeNBird/codesafe/main/install.sh | bash
```

或从 [Releases](https://github.com/LingyeNBird/codesafe/releases) 下载：

| 平台 | 文件 |
|---|---|
| Windows (x64) | `codesafe-windows-amd64.exe` |
| Linux (x64) | `codesafe-linux-amd64` |
| macOS (Apple Silicon) | `codesafe-darwin-arm64` |
| macOS (Intel) | `codesafe-darwin-amd64` |

## 命令

| 命令 | 作用 |
|---|---|
| `codesafe` | 给暂存/工作区 diff 建议 `type(scope)`（单行，可接进 `git commit -m`）。 |
| `codesafe commit -m ...` | 校验 message+代码规则 → 生成前缀 → 执行 `git commit`。 |
| `codesafe diff` | 按 `codesafe.yaml` 的 `rules` 检查 diff，报告违反项。 |
| `codesafe delete <path>` | 删前判安全性：`safe` 删除、`sensitive` 移回收、`dangerous` 中断。 |
| `codesafe init` | 生成注释模板 `codesafe.yaml` + 打印给 AI 的配置提示词。 |
| `codesafe agent` | 打印一段贴进 `AGENTS.md`/`CLAUDE.md` 的规则，让 AI 用 codesafe。 |
| `codesafe todo <任务>` | 记录当前任务；`commit`/`diff` 会判定 diff 是否实现了它。 |

### 分类（默认）

```sh
./codesafe                    # 暂存 diff，否则工作区 → 输出如 "feat(cli)!"
./codesafe --detail           # 完整百分比 + token/费用统计
./codesafe --source worktree  # 只查未暂存；--source <sha> 查某 commit
./codesafe --source staged    # 只查暂存
```

### 守护式提交

```sh
./codesafe commit -m "重构为 conventional-commit 分类器" -m "- 新增 scope 筛选"
```

先查 `commit_rules`（如 subject 须中文），再对暂存 diff 查 `rules`，生成前缀后执行 `git commit`。第一个 `-m` 自带的 `type(scope):` 前缀按 `prefix_conflict`（`keep_user`/`override`）决定保留或替换。

### 守护式删除

```sh
./codesafe delete build/          # 先判定再执行
./codesafe delete build/ --check  # 只输出判定，不删不移
./codesafe delete tmp/ --yes      # 跳过判定
```
判定：`safe` → `os.RemoveAll`；`sensitive` → 移到系统临时目录下的回收目录（可恢复）；`dangerous` → 中断。

### 任务 TODO

```sh
./codesafe todo "加 OAuth 登录"    # 记录本仓库当前任务
./codesafe todo                    # 查看
./codesafe todo --clear            # 清空（commit 成功也会自动清空）
```

设了 TODO 后，`codesafe commit`/`codesafe diff` 会额外问模型 diff 是否实现了该任务——没实现就中断提交。规则可在 `text`/`pass`/`fail` 里用 `{{TODO}}` 引用当前任务；`todo_mode`（`off`|`loose`|`strict`，默认 `loose`）控制含 `{{TODO}}` 的规则是否强制要求已设 TODO——`strict` 在 TODO 为空时中断提交，也可在单条规则上覆盖。


### 项目配置 `codesafe.yaml`

```yaml
lang: zh
allow_none: true
prefix_conflict: override

scopes:                    # 本项目的 scope 词表
  cli:    "命令行入口与子命令"
  api:    "HTTP/RPC 层"
  ci:     "CI / release workflow"

rules:                     # 代码规则——对 diff 检查
  - id: vue-css-split
    level: error           # error 中断 · warn 只提示
    files: "*.vue"         # diff 触及匹配文件才问
    text: .vue 文件不得内联 <style> 块，CSS 拆到同名 .css
    pass: 所有 .vue 样式都在外部 .css
    fail: 存在 .vue 内联 <style>

commit_rules:              # 针对 commit message 本身的规则
  - id: subject-zh
    on: subject            # subject | body | prefix | all
    text: subject 必须是中文
    pass: subject 主体语言为中文
    fail: subject 不是中文
```

不写 `scopes` 时，codesafe 用内置词表对目录树做筛选（项目是 CLI 时 `cli` 这类词会被判"整体即此物"而剔除），结果按项目缓存。

### 给 AI 用

```sh
./codesafe agent           # 打印一段贴进 AGENTS.md / CLAUDE.md 的规则
```

把输出贴进你的 agent 规则文件，AI 就会用 `codesafe delete`/`commit`/`diff` 代替裸 `rm`/`git commit`。

## 配置

```sh
./codesafe --config api_key=<key>          # 存 TypeSafe key（console.typesafe.ai）
./codesafe --config lang=zh|en
./codesafe --config scopes=cli|server|web
./codesafe --config nonescope=false
./codesafe --set lang=en                   # 单次覆盖，不落盘
```

优先级：`codesafe.yaml` > `--config` > 内置/筛选默认。

## API key

到 [console.typesafe.ai](https://console.typesafe.ai) 获取。首次运行提示输入并存到用户配置目录（`0600`）。

## License

[AGPL-3.0-or-later](COPYING)

## 友链

- [linux.do](https://linux.do)
