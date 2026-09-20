# codesafe

用 [TypeSafe System One API](https://docs.typesafe.ai) 为你的暂存区或工作区 diff 建议一个 **conventional-commit `type(scope)`**。它读取你的改动并选出 commit 类型和 scope，让 AI 和脚本不用手分类就能拿到一致的 `type(scope)` 建议。

[English](README.md)

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

## 使用

```sh
# 首次运行会提示输入 TypeSafe API key（console.typesafe.ai 获取）并保存
# 分析暂存区 diff（无暂存时退到工作区 diff）
./codesafe

# 分析指定 commit
./codesafe --source 5b5e054

# 只分析工作区（未暂存）改动
./codesafe --source worktree

# 输出语言
./codesafe --config lang=zh       # 中文
./codesafe --config lang=en       # 英文

# 持久化配置
./codesafe --config api_key=<key>
./codesafe --config scopes=cli|server|web|docs   # 你的 scope 名

# 临时覆盖，仅本次生效、不落盘
./codesafe --set api_key=<key>
./codesafe --set lang=en
```

输出为建议的 `type`、`scope`，各自带置信度与备选，以及合并的 `type(scope)` 建议。

## 项目 scope

内置一组通用 scope（`app`、`ui`、`api`、`cli`、`docs`……）。按项目覆盖以贴合你的仓库约定。

**随仓库分发**（协作共享）——在仓库根建 `codesafe.yaml`：

```yaml
allow_none: false        # 可选：禁止 scope=none（强制每个 commit 带具体 scope）
scopes:
  - server
  - web: 前端界面
  - installer
  - cli
```

`- 名字` 用名字本身作描述；`- 名字: 描述` 给模型额外提示。

**用户级**（仅本机、不提交）：

```sh
./codesafe --config scopes=server|web|cli|installer
./codesafe --config nonescope=false     # 禁止 none scope
```

优先级：`codesafe.yaml` > `--config scopes` > 内置集。

### `none` scope

scope 集默认提供 `none`（"跨模块改动、无单一区域"）——遇到不属于任何单一 scope 的全仓改动时选它，建议输出不带括号（如 `feat!:`）。若项目要求每个 commit 必须带具体 scope，用 `codesafe.yaml` 的 `allow_none: false` 或 `./codesafe --config nonescope=false` 关掉。

## API key

在 [console.typesafe.ai](https://console.typesafe.ai) 获取。首次运行提示并保存到用户配置目录（权限 `0600`）。

## License

[AGPL-3.0-or-later](COPYING)
