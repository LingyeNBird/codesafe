# codesafe

基于 [TypeSafe](https://typesafe.ai) System One API 的逐文件安全/缺陷快速筛查工具。

指向一个文件夹，它会对每个文本文件跑五个判断维度——缺陷、安全、数据访问、
依赖用法、内部逻辑——然后按总体风险从高到低逐行输出概率和 Nerd Font 圆环仪表。

- 任意目录都能用：git 仓库里走 `git ls-files`，否则遍历整个目录树
- 凭据文件（`.env`、`*.pem`、`*.key` 等）不会被发送
- 二进制和超大文件自动跳过
- 配置文件和文档只做格式合法性检查
- 并发限速调用；API key 首次运行时输入一次并保存到用户配置目录

## 安装

从 [Releases](../../releases) 下载对应平台的二进制文件，或用 Go 1.27+ 自行编译：

```sh
go build -o codesafe ./cmd/codesafe
```

## 使用

```sh
# 首次运行会提示输入 TypeSafe API key（console.typesafe.ai 获取）并保存
./codesafe

# 扫描其他目录
./codesafe --dir /path/to/project

# 只列出将被扫描的文件，不调用 API
./codesafe --dry-run

# 更新已保存的 API key
./codesafe --config <new-key>

# 调整并发/请求速率（默认 16 并发，20 req/s）
./codesafe --concurrency 32 --rps 30
```

输出列：`缺陷` · `安全` · `数据` · `依赖` · `逻辑`；配置/文档文件只显示
`格式` 一列。行按五个维度等权加总降序排列。

## License

AGPL-3.0-or-later — 见 COPYING。
