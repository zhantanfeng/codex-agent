# dcode Apple 芯片版（0.1.14）

支持 Apple 芯片的 Mac（M1、M2、M3、M4 等），目标系统为 macOS 11 或更新版本。手机继续使用现有 Android App，Relay 无需为本次适配升级。

## 已构建的本地端

Windows 上运行 `powershell -File scripts/build-macos.ps1` 生成 `bin/dcode-0.1.14-macos-arm64.tar.gz`。这是 Mac ARM64 原生命令行 Agent，包含双击启动脚本；原生窗口版需要在 Mac 上按下方步骤构建。

1. 在 Mac 安装并登录 Codex CLI，确认终端中的 `codex --version` 和 `codex login status` 正常。Agent 要求 CLI 0.156.0 或更新版本。
2. 解压包，把整个文件夹放在固定位置。双击 `start.command`，首次输入与手机相同的中转服务器地址。也可以使用下面的终端命令。
3. 终端显示“手机连接码”六位数字，在手机 dcode 输入该码配对。码十分钟有效；以后直接连接。
4. 保持终端打开，按 Control+C 停止。Mac 需要保持开机和联网；睡眠期间手机无法连接。

如果系统提示启动脚本没有执行权限，在该目录运行 `chmod +x start.command codex-remote`。发行包尚未使用 Apple Developer ID 签名或公证；若被系统拦截，可在“系统设置 → 隐私与安全性”核实来源后选择“仍要打开”。

```bash
cd /path/to/dcode-0.1.14-macos-arm64
./codex-remote init -relay ws://43.155.135.230:8080/ws
./codex-remote project add -id work -path "$HOME/projects/work"
./codex-remote run
```

`init` 仅首次执行；项目目录必须已经存在。也可以直接在手机“浏览电脑目录 / 新建会话”选择目录，Mac 的文件系统根目录是 `/`，用户目录通常为 `/Users/你的用户名`。

如需登录后自动运行，先确认正常连接，再运行 `./codex-remote install`。它写入当前用户的 `~/Library/LaunchAgents/dev.dcode.agent.plist`，下次登录生效，无需 sudo。移动程序后需重新执行 `install`。使用 `./codex-remote uninstall` 取消后续登录启动；当前进程需手动停止。自启动日志位于 `~/Library/Application Support/CodexRemote/agent.log`，建议定期查看大小并在停止后台后清理。

需要临时保持清醒时可用 `caffeinate -i ./codex-remote run` 启动；它允许屏幕关闭，不保证合盖后继续运行。

## 配置与密钥

配置、图片和日志保存在 `~/Library/Application Support/CodexRemote`。电脑身份私钥保存在当前用户的系统钥匙串（服务名 `dev.dcode.agent.identity`），`identity.bin` 只保存引用。钥匙串锁定或用户取消访问时会报错，不降级为明文。请在正常登录的用户会话下使用，勿用 sudo。

Mac 会生成新的电脑身份，需要与手机重新配对。不能直接复制 Windows 的 `identity.bin`。Codex 自己的登录信息继续留在本机。

Finder 和登录启动会读取 zsh 登录环境，并补充 `/opt/homebrew/bin` 等常见路径。如果仍找不到 Codex，请将配置中的 `codexCommand` 设置成 `command -v codex` 输出的绝对路径；npm 安装的 Codex 同时需要 PATH 中能找到 Node。macOS 保护的桌面、文稿等目录可能需要在系统设置中授予终端或 dcode 文件访问权限。

## 构建原生窗口版

在 Apple 芯片 Mac 上安装 Go 1.23+、Xcode Command Line Tools，并在完整仓库根目录运行：

```bash
xcode-select --install  # 已安装时跳过
go test ./...
bash scripts/build-desktop-macos.sh
```

输出 `bin/dcode.app` 和 `bin/dcode-desktop-macos-arm64.zip`。构建脚本使用本机 SDK 与临时签名，不包含 Developer ID 签名或公证。静态界面直接嵌入，不需要 Node 构建前端。Windows 上无法用当前 Go 工具链构建此 Cocoa 窗口版。

把 `dcode.app` 移到固定位置后打开。界面复用 Mac 命令行版的配对、配置与项目。原生窗口支持配对码、二维码、服务器设置、设备管理、日志、登录自启动与可选保持唤醒。Mac 版通过 Dock 和应用菜单运行，关闭窗口后从菜单 `dcode → 显示主窗口` 重新打开，选择 `dcode → 退出 dcode` 退出。

如果先前启用了命令行登录启动，先运行 `./codex-remote uninstall` 并停止旧后台，再开启桌面版登录启动，避免重复运行。Mac 版使用同一身份的文件锁拦截重复 Agent。桌面版自启动写入 `~/Library/LaunchAgents/dev.dcode.desktop.plist`，下次登录生效。

本次已在 Windows 上运行回归测试并交叉编译、检查 ARM64 Mach-O 格式；尚未在真实 Mac 上验证钥匙串弹窗、登录启动和原生窗口。
