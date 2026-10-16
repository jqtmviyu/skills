# `skills` Go CLI：命令与配置契约

本文定义 GitHub Release 中 Go 版 `skills` 命令当前对用户公开的 CLI、JSON 和本地状态文件行为。它不定义 Node.js 包的行为；Node 源码和 npm/Bun 分发不属于本文件范围。

## 分发与版本

Go 二进制通过 GitHub Release 分发，资产名为：

- macOS：`skills-darwin-amd64`、`skills-darwin-arm64`
- Linux：`skills-linux-amd64`、`skills-linux-arm64`
- Windows：`skills-windows-amd64.exe`、`skills-windows-arm64.exe`

`skills --version` 输出命令名和版本。Release tag 使用 `vX.Y.Z`；Release workflow 会把资产上传到当前 Release 所属的 GitHub 仓库。

## 命令行接口

| 命令 | 别名 | 用途 |
| --- | --- | --- |
| `skills add <source>` | `a` | 发现、选择并安装来源中的技能 |
| `skills use <source>[@<skill>]` | — | 生成技能使用提示；不安装技能 |
| `skills init [name]` | — | 在当前项目创建技能模板 |
| `skills list` | `ls` | 列出当前项目安装的技能；`--global` 切换到全局范围 |
| `skills remove [skills...]` | `rm`、`r` | 移除当前项目或全局安装的技能 |
| `skills update [skills...]` | `check`、`upgrade` | 检查并更新已安装技能 |
| `skills install` | `i`、`experimental_install` | 从项目 lock 文件恢复安装 |

运行 `skills --help` 或 `skills <command> --help` 查看当前版本的完整帮助。`list` 的文本输出为每个技能两行，按名称与路径对齐；`$HOME` 下的路径缩写为 `~`，当前项目内路径缩写为 `.`。ANSI 颜色仅用于终端输出；`NO_COLOR` 禁用颜色，`FORCE_COLOR` 可强制开启。

### 常用参数

- 范围：`add`、`update` 支持 `--project`（默认或自动选择）和 `--global`；`list`、`remove` 默认项目范围，并用 `--global` 切换全局范围。
- Agent：`--agent <id...>` 选择一个或多个 agent；`*` 表示所有支持的目标。当前 registry 包含 78 个 agent。
- 技能选择：`add --skill <name...>`；`*` 选择全部发现的技能。`--list` 只列出来源中可用的技能。
- 非交互运行：`--yes`；`add --all` 等价于选择全部技能与 agent 并启用 `--yes`。
- 安装方式：`add --copy` 复制文件，而不是使用默认的 agent 目录链接行为。
- 发现范围：`add --full-depth`、`use --full-depth` 搜索更深层目录。
- Eve：`add --subagent <name...>` 可指定 Eve 子 agent；`root` 表示 Eve 根 agent。
- 移除：`remove --all` 移除范围内全部技能并跳过确认；`--skill` 可指定技能名。
- 更新：`update --project` 或 `--global` 指定范围；默认在交互终端询问，非交互时根据项目是否已有技能选择范围。位置参数可按技能名或 lock 中的来源标识筛选，`--yes` 跳过确认。
- 使用：`use --skill <name>` 选择技能；`use --agent <id>` 可启动 Claude Code、Codex 或 Sarvam Code。
- 输出：`add --json` 输出 JSON 数组，必须同时使用 `--yes`；不能与 `--list` 合用。`list --json` 输出 JSON 数组。

GitHub、GitLab、本地路径和通用 HTTPS/SSH Git 来源均可使用；来源支持子路径、ref 和技能选择器。插件 manifest 与标准 well-known 来源可用于发现技能。

## JSON 输出

### `list --json`

输出技能对象数组，每个对象含以下字段：

- `name`：技能 metadata 名称
- `path`：安装目录
- `scope`：`project` 或 `global`
- `agents`：检测到的 agent 名称数组
- `source`、`sourceUrl`、`sourceType`：可从对应 lock 条目取得；没有 lock 信息时为 `null`

没有已安装技能时输出 `[]`。`pluginName` 当前仅用于文本分组，不包含在 JSON 中。

### `add --json`

输出结果数组。字段包括 `name`、`status`、`source`、`ref`、`hash`、`path`、`scope`、`agents`、`mode`、`security`、`reason` 和 `error`；可选字段在不适用时省略，`ref`、`hash` 和 `security` 可为 `null`。

当前 `status` 值为：

- `installed`：安装成功
- `skipped`：selector 没有匹配到来源中的技能
- `failed`：技能安装失败

`security` 为保留字段，目前输出 `null`。调用方应忽略不认识的字段，并允许将来增加字段。

## 安装位置、配置和 lock 文件

- 项目 lock 文件：当前项目根目录中的 `skills-lock.json`，格式版本为 v1。
- 全局 lock 文件：默认是 `$HOME/.agents/.skill-lock.json`；设置 `XDG_STATE_HOME` 时使用 `$XDG_STATE_HOME/skills/.skill-lock.json`。Go CLI 可读取 Global v1、v2、v3；写入时使用 v3。
- lock 写入会保留当前版本不认识的顶层字段和技能条目字段。
- 项目技能默认使用项目内 agent 配置目录；全局安装使用各 agent 的全局目录。共享通用目标为 `.agents/skills`。
- `XDG_CONFIG_HOME`（需绝对路径）用于解析使用 XDG 配置目录的 agent。部分 agent 支持各自的环境变量路径，例如 `CLAUDE_CONFIG_DIR`、`CODEX_HOME`、`VIBE_HOME`、`HERMES_HOME`、`AUTOHAND_HOME` 和 `GROK_HOME`。

两个 lock hash 的含义不同，不可互换：

- Global GitHub 来源的 `skillFolderHash` 是技能目录的 Git tree hash。
- Project 的 `computedHash` 是基于相对 POSIX 文件路径与文件内容计算的 SHA-256；排除 `.git` 和 `node_modules`，排序遵循当前进程 locale。

只读检查不会为了规范化格式而重写 lock 文件。

## Git 获取与更新语义

对于 Git 来源，CLI 请求 shallow partial clone（`blob:none`），先扫描树和候选 `SKILL.md` 元数据；选择后再 materialize 所选技能目录及其支持文件。服务器拒绝或忽略 filter 时会退回普通 shallow clone，并报告无法节省传输量。根目录技能可能需要仓库中的全部文件，因为整个仓库就是技能目录。

未固定 ref 的 GitHub Trees 检查使用仓库默认分支。更新只针对 lock 中的技能及其目录；标准 well-known lock 条目根据 digest/content 语义检查和更新。

## 错误与退出状态

- `skills --help`、`skills --version` 成功时返回 0。
- 未知顶层命令返回 2。
- 命令执行失败通常返回 1；具体命令也可能把“无可移除技能”等无操作结果作为成功返回。自动化应同时检查退出码和 JSON 中的 `status`，不要依赖终端文案。

## 明确不提供的功能

Go 版当前不实现 standalone `find`/`search`、`experimental_sync`、遥测、安全审计、Notion/Zapier 专用 API 或 snapshot 下载服务，也不提供 GitHub Enterprise 专用 API host 配置。标准 Git HTTPS/SSH 凭据与支持的 GitHub API 流程仍可使用。
