#!/bin/sh
# Installs the latest yt-player release binary.
# Usage: curl -fsSL https://raw.githubusercontent.com/karthik1729/yt-player/main/install.sh | sh
set -eu

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case $(uname -m) in
x86_64 | amd64) arch=amd64 ;;
arm64 | aarch64) arch=arm64 ;;
*) echo "unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac
case $os in
linux | darwin) ;;
*) echo "unsupported OS: $os" >&2; exit 1 ;;
esac

dir=${INSTALL_DIR:-$HOME/.local/bin}
mkdir -p "$dir"
url="https://github.com/karthik1729/yt-player/releases/latest/download/yt-player-$os-$arch"
echo "Downloading $url"
curl -fsSL "$url" -o "$dir/yt-player.tmp"
chmod +x "$dir/yt-player.tmp"
mv "$dir/yt-player.tmp" "$dir/yt-player"
echo "Installed $dir/yt-player"

case ":$PATH:" in
*":$dir:"*) ;;
*) echo "Add $dir to your PATH." ;;
esac
command -v mpv >/dev/null || echo "Install mpv: brew install mpv (macOS) or your package manager."
command -v yt-dlp >/dev/null || echo "Install yt-dlp: brew install yt-dlp (macOS) or your package manager."
