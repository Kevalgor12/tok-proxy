#!/bin/sh
# tok installer for macOS / Linux.
#   curl -fsSL https://raw.githubusercontent.com/Kevalgor12/tok-proxy/main/scripts/install.sh | sh
# Downloads the standalone tok binary (no Node required), puts it on PATH, installs hooks.
set -e

# Override with TOK_REPO=owner/repo to install from a fork.
REPO="${TOK_REPO:-Kevalgor12/tok-proxy}"

OS=$(uname -s)
ARCH=$(uname -m)
case "$OS" in
  Linux)  os=linux ;;
  Darwin) os=macos ;;
  *) echo "Unsupported OS: $OS" >&2; exit 1 ;;
esac
case "$ARCH" in
  x86_64|amd64)  arch=x64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) echo "Unsupported architecture: $ARCH" >&2; exit 1 ;;
esac

ASSET="tok-$os-$arch"
URL="https://github.com/$REPO/releases/latest/download/$ASSET"
DIR="$HOME/.local/bin"
DEST="$DIR/tok"

mkdir -p "$DIR"
echo "Downloading $URL ..."
curl -fsSL "$URL" -o "$DEST"
chmod +x "$DEST"

# Nudge PATH if needed.
case ":$PATH:" in
  *":$DIR:"*) ;;
  *)
    echo "Add $DIR to your PATH, e.g.:"
    echo '  echo '"'"'export PATH="$HOME/.local/bin:$PATH"'"'"' >> ~/.profile'
    ;;
esac

# Detect AI tools and install hooks.
"$DEST" init

echo ""
echo "tok installed to $DEST"
echo "Restart your AI tool, then run:  tok doctor"
