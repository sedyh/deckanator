#!/usr/bin/env bash
set -euo pipefail

REPO="sedyh/deckanator"

TAG="${1:-}"
if [ -z "$TAG" ]; then
  echo "Fetching latest release..."
  TAG=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" | grep '"tag_name"' | cut -d'"' -f4)
fi

URL="https://github.com/$REPO/releases/download/$TAG/deckanator-installer-linux-amd64"
INSTALLER="/tmp/deckanator-installer"

echo "Downloading installer $TAG..."
curl -fsSL "$URL" -o "$INSTALLER"
chmod +x "$INSTALLER"

echo "Running installer..."
"$INSTALLER" --version "$TAG"
