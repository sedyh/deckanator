#!/usr/bin/env bash
set -euo pipefail

REPO="sedyh/deckanator"
APP_ID="io.github.sedyh.Deckanator"

TAG="${1:-}"
if [ -z "$TAG" ]; then
  echo "Fetching latest release..."
  TAG=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" | grep '"tag_name"' | cut -d'"' -f4)
fi

BASE="https://github.com/$REPO/releases/download/$TAG"

echo "Installing Deckanator $TAG..."

# 1. Install Flatpak app
FLATPAK_URL="$BASE/deckanator.flatpak"
FLATPAK_FILE="/tmp/deckanator.flatpak"
echo "[1] Downloading Flatpak bundle..."
curl -fsSL "$FLATPAK_URL" -o "$FLATPAK_FILE"
echo "[1] Installing Flatpak..."
flatpak install --user --noninteractive --no-related --or-update "$FLATPAK_FILE"

# 2. Run installer for Steam integration
INSTALLER_URL="$BASE/deckanator-installer-linux-amd64"
INSTALLER="/tmp/deckanator-installer"
echo "[2] Downloading installer..."
curl -fsSL "$INSTALLER_URL" -o "$INSTALLER"
chmod +x "$INSTALLER"
echo "[2] Configuring Steam shortcut..."
"$INSTALLER" --flatpak

echo ""
echo "Done! Restart Steam to see Deckanator in your library."
