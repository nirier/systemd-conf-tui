# systemd-conf-tui

使用 **Go + Bubble Tea** 编写的中文终端界面，快速创建 systemd 服务。采用类似 lazygit / jjui 的分栏布局与键盘操作：左侧配置，右侧实时预览，按 `Enter` 才进入编辑。默认创建当前用户的 `--user` 服务，可在界面中按 **F2** 切换到需要 sudo 的系统服务模式。

## 开始使用

运行环境为使用 systemd 的 Linux，需要 `systemctl`、`systemd-analyze`；系统模式还需要 `sudo` 及相应权限。用户模式应在正常的用户登录会话中运行。

从 [Releases](https://github.com/nirier/systemd-conf-tui/releases/latest) 下载对应架构的 Linux 安装包，校验并解压后运行：

```sh
sha256sum --ignore-missing -c SHA256SUMS.txt
tar -xzf systemd-conf-tui_0.2.0_linux_amd64.tar.gz
./systemd-conf-tui
```

ARM64 设备使用 `linux_arm64` 安装包。也可以使用 Go **1.25+** 安装：

```sh
go install github.com/nirier/systemd-conf-tui@v0.2.0
systemd-conf-tui
```

从源码运行：

```sh
git clone https://github.com/nirier/systemd-conf-tui.git
cd systemd-conf-tui
go mod download
make build
./bin/systemd-conf-tui
# 或直接运行
go run .
```

请以自己的普通账户运行程序。整个 TUI 不需要 sudo；系统安装时会将终端交还给 sudo 输入密码。

## 三步创建

1. **基本信息**：`j/k` 选中字段，`Enter` 编辑服务名和启动命令；编辑时可用 `Tab` 保存并编辑下一项，描述可留空。
2. **运行设置**：`l` 或 `2` 切换到运行设置，按需填写工作目录、重启间隔和环境变量；服务类型、重启策略通过选择列表设置。接受默认值可直接继续。
3. **预览安装**：`l` 或 `3` 进入预览安装，核对完整 `.service` 文件及安装路径；`j/k` 选择自启或立即启动，`Space` 切换，按 `i` 安装。

配置步骤可随时切换；配置不完整或校验失败时无法进入安装页。编辑过程中实时更新预览，`Esc` 会还原本次编辑前的内容。

默认 `Type=simple`、`Restart=on-failure`、重启间隔 `3s`，预览页默认勾选自启和立即启动。安装会先执行 `systemd-analyze verify`，随后写入文件、执行 `daemon-reload`，再按所选选项执行 `enable` 和 `start`。启动成功表示 systemd 接受了启动请求，应用持续运行情况请用结果页的 `status` / `journalctl` 命令确认。

也可以按 `e` 将预览导出到当前目录，暂不安装。导出和安装均不会覆盖已有的目标文件，包括符号链接。请为新服务使用独有名称，避免遮蔽发行版安装在其他 unit 搜索路径中的同名服务。

| 按键 | 作用 |
| --- | --- |
| 浏览时 `j/k` / `↑↓` | 移动选中项；预览面板聚焦时滚动内容 |
| `h/l` / `←→` / `Ctrl+P/N` | 上一步 / 下一步 |
| `1`–`3`（高级模式 `1`–`5`） | 直接切换配置步骤 |
| `Tab` / `Shift+Tab` | 切换配置 / 预览面板 |
| `Enter` | 编辑当前字段；枚举字段打开选择列表 |
| 编辑时 `Enter` / `Ctrl+S` | 保存当前字段；多行编辑时 `Enter` 换行，`Ctrl+S` 保存 |
| 编辑时 `Tab` / `Shift+Tab` | 保存并编辑下一个 / 上一个字段（多行编辑除外） |
| `Esc` | 取消本次编辑；浏览时返回配置面板或上一步 |
| `g/G` / `Home/End` | 跳到列表或预览的开头 / 末尾 |
| `PgUp` / `PgDown` | 翻页滚动预览 |
| `F2` | 切换用户服务 / 系统服务，保留已填写内容 |
| `F3` | 打开或收起高级配置，保留配置值 |
| 安装页 `j/k` + `Space` / `Enter` | 选择并切换自启 / 立即启动选项，`Tab` 切到预览滚动 |
| 安装页 `i` / `e` | 安装 / 导出 |
| `?` | 查看快捷键帮助，`?` / `Esc` 关闭 |
| 结果页 `n` / `b` | 再建一个 / 尚未写入服务文件时返回修改 |
| `q` / `Ctrl+C` | 退出；编辑时 `q` 作为普通文字，`Ctrl+C` 始终退出 |

终端至少需要 48 列 × 20 行，推荐 120 列 × 30 行。96 列及以上使用左右分栏；窄终端使用单面板，按 `Tab` 查看实时预览。窄终端的安装页同时显示预览和安装选项。字段列表随选中项滚动，预览中的长行会折行，完整内容可滚动查看。

## 用户与系统模式

| | 用户模式（默认） | 系统模式（F2） |
| --- | --- | --- |
| 安装目录 | `$XDG_CONFIG_HOME/systemd/user`，默认 `~/.config/systemd/user` | `/etc/systemd/system` |
| 控制命令 | `systemctl --user …` | `sudo systemctl …` |
| 默认运行身份 | 当前用户 | root，可在高级配置设置 `User` / `Group` |
| 默认自启目标 | `default.target` | `multi-user.target` |

用户服务的自启通常随用户 manager 启动。需要退出登录后持续运行、或在开机时无需登录即运行时，可自行执行：

```sh
loginctl enable-linger "$USER"
```

程序不会自动修改 linger。用户服务只能依赖用户 manager 中的 unit；例如系统的 `network-online.target` 通常不适合作为用户服务的依赖。

## 高级配置

按 `F3` 可配置环境文件、运行用户/组、`After` / `Wants` / `Requires`、停止与重载命令、启动超时、内存上限、CPU 配额和安装目标。

继续到“自定义指令”页，按 `Enter` 编辑、`Ctrl+S` 保存，可追加原生 systemd 配置，例如：

```ini
[Service]
NoNewPrivileges=yes
PrivateTmp=yes
StandardOutput=journal
StandardError=journal

[Unit]
StartLimitIntervalSec=60
StartLimitBurst=5
```

支持 `[Unit]`、`[Service]`、`[Install]`，每行填写一个 `Key=Value`，支持注释和空值重置，不支持反斜杠续行。自定义段追加在生成内容之后，按 systemd 规则合并或重置指令；安装前请核对最终预览。字段值沿用 systemd 原生语法，不经过 shell 转义或展开。

- `ExecStart` 不会自动解释管道、重定向、`&&` 或 `~`。需要 shell 时显式填写 `/bin/sh -c '…'`。
- 推荐给可执行文件和工作目录填写绝对路径；路径含空格时使用 systemd 的引号语法。
- 环境变量示例：`"PORT=8080" "GREETING=hello world"`。服务文件权限为 `0644`，不要将密钥写入其中。
- `Type=oneshot` 不能搭配 `Restart=always` 或 `on-success`。`forking` / `notify` / `dbus` 等类型的附加指令可通过自定义段补齐。
- 导出只做本程序的结构校验；安装还会使用本机 `systemd-analyze` 检查指令、依赖和可执行文件。校验失败时不会安装。
- 如果文件已写入，但 reload / enable / start 失败，会保留文件并显示具体失败阶段和排查命令。程序不会自动重试或删除已经安装的 unit。

## 非交互生成

```sh
./bin/systemd-conf-tui --help
./bin/systemd-conf-tui --version
./bin/systemd-conf-tui --render examples/app.json
./bin/systemd-conf-tui --system --render examples/app.json > my-app.service
cat examples/app.json | ./bin/systemd-conf-tui --render -
```

`--render` 只输出 unit，不调用 sudo、不安装、不启动，Linux 以外也可用于生成配置。JSON 字段见 [示例](examples/app.json) 和 [Config](internal/unit/unit.go)，使用 snake_case；未知字段会报错。`extra` 字段可包含带 `\n` 的自定义段。交互界面和 JSON 使用相同的默认值和生成逻辑。

Shell 补全脚本位于 `completions/`：Bash 可 `source completions/systemd-conf-tui.bash`；Zsh 将 `_systemd-conf-tui` 放入 `$fpath` 并运行 `compinit`；Fish 将 `.fish` 文件放入 `~/.config/fish/completions/`。

## 开发

```sh
make test
make build
```

打包 Linux amd64 / arm64 发布文件并生成 SHA256 校验文件：

```sh
make release VERSION=0.2.0
```

```text
main.go                参数、JSON 生成入口和终端检测
internal/unit/         unit 配置、校验和生成
internal/service/      安装、原子写入和 systemctl 操作
internal/tui/          Bubble Tea 模型、分栏视图、编辑器和 sudo 终端交接
```

测试使用临时目录和模拟命令，覆盖配置校验、键盘导航、编辑保存与取消、模式切换、终端尺寸适配、快速流程、文件保护及部分安装失败；不会安装或启动真实服务。目前提供新建与导出，不包含已有服务的编辑、删除或状态管理。
