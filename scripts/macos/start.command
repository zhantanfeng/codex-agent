#!/bin/bash
set -euo pipefail
cd -- "$(dirname -- "$0")"
chmod +x ./codex-remote
config_dir="$HOME/Library/Application Support/CodexRemote"

if [ ! -f "$config_dir/config.json" ]; then
  relay_url=$(/usr/bin/osascript -e 'text returned of (display dialog "请输入电脑与手机共同使用的中转服务器地址：" default answer "ws://43.155.135.230:8080/ws" with title "dcode 首次配置" buttons {"取消", "保存"} default button "保存")')
  ./codex-remote init -relay "$relay_url"
fi

echo "dcode 已启动。请在手机上输入下方显示的六位手机连接码。"
echo "保持此终端窗口打开；按 Control+C 停止。"
exec ./codex-remote run
