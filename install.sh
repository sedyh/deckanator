#!/usr/bin/env bash
set -euo pipefail

REPO="sedyh/deckanator"
INSTALL_DIR="$HOME/.local/share/deckanator"
BIN="$INSTALL_DIR/Deckanator"
DESKTOP_DIR="$HOME/.local/share/applications"
DESKTOP_FILE="$DESKTOP_DIR/deckanator.desktop"

TAG="${1:-}"
if [ -z "$TAG" ]; then
  echo "Fetching latest release..."
  TAG=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" | grep '"tag_name"' | cut -d'"' -f4)
fi

URL="https://github.com/$REPO/releases/download/$TAG/deckanator-linux-amd64"

echo "Installing Deckanator $TAG..."
mkdir -p "$INSTALL_DIR" "$DESKTOP_DIR"

curl -fsSL "$URL" -o "$BIN"
chmod +x "$BIN"

cat > "$DESKTOP_FILE" <<EOF
[Desktop Entry]
Name=Deckanator
Exec=$BIN
Icon=$INSTALL_DIR/icon.png
Type=Application
Categories=Game;
StartupNotify=false
EOF

curl -fsSL "https://github.com/$REPO/raw/$TAG/build/appicon.png" -o "$INSTALL_DIR/icon.png" 2>/dev/null || true

echo ""
echo "Installed to $BIN"
echo "To add to Steam: Games -> Add a Non-Steam Game -> Browse -> $BIN"
