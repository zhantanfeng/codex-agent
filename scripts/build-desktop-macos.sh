#!/bin/bash
set -euo pipefail
cd -- "$(dirname -- "$0")/.."
if [ "$(uname -s)" != Darwin ]; then
  echo "请在 macOS 上构建原生桌面版（需要 Xcode Command Line Tools）。" >&2
  exit 1
fi
if ! xcrun --find clang >/dev/null 2>&1; then
  echo "请先运行 xcode-select --install，再重新构建。" >&2
  exit 1
fi
export GOCACHE="$PWD/.cache/go-build"
export GOPATH="$PWD/.cache/gopath"
export GOOS=darwin GOARCH=arm64 CGO_ENABLED=1
export MACOSX_DEPLOYMENT_TARGET=11.0
export CGO_CFLAGS="${CGO_CFLAGS:-} -mmacosx-version-min=11.0"
export CGO_LDFLAGS="${CGO_LDFLAGS:-} -mmacosx-version-min=11.0"
app_dir="$PWD/bin/dcode.app"
mkdir -p "$app_dir/Contents/MacOS" "$app_dir/Contents/Resources"
go build -tags desktop,production -trimpath -ldflags '-s -w' -o "$app_dir/Contents/MacOS/dcode" ./cmd/desktop
cp scripts/macos/Info.plist "$app_dir/Contents/Info.plist"
icon_dir="$PWD/.cache/dcode.iconset"
mkdir -p "$icon_dir"
for size in 16 32 128 256 512; do
  sips -z "$size" "$size" cmd/desktop/frontend/logo.png --out "$icon_dir/icon_${size}x${size}.png" >/dev/null
  twice=$((size * 2))
  sips -z "$twice" "$twice" cmd/desktop/frontend/logo.png --out "$icon_dir/icon_${size}x${size}@2x.png" >/dev/null
done
iconutil -c icns "$icon_dir" -o "$app_dir/Contents/Resources/dcode.icns"
codesign --force --deep --sign - "$app_dir"
codesign --verify --deep --strict "$app_dir"
ditto -c -k --sequesterRsrc --keepParent "$app_dir" "$PWD/bin/dcode-desktop-macos-arm64.zip"
echo "Built: $app_dir"
echo "Package: $PWD/bin/dcode-desktop-macos-arm64.zip"
