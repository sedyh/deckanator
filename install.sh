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

# The bundle itself comes from GitHub, but a missing GNOME runtime is
# pulled from Flathub, which some networks cannot reach. The mirrors
# serve the same GPG-signed repository, so falling back to one is safe.
RUNTIME="org.gnome.Platform/x86_64/50"
FLATHUB_MIRRORS="https://mirror.sjtu.edu.cn/flathub https://mirrors.ustc.edu.cn/flathub"
if ! flatpak info --user "$RUNTIME" >/dev/null 2>&1 && ! flatpak info "$RUNTIME" >/dev/null 2>&1; then
  if curl -fsI -m 8 https://dl.flathub.org/repo/config >/dev/null 2>&1; then
    flatpak remote-add --user --if-not-exists flathub https://dl.flathub.org/repo/flathub.flatpakrepo
  else
    echo "Flathub is unreachable, looking for a mirror to fetch the runtime..."
    MIRROR_OK=""
    for MIRROR in $FLATHUB_MIRRORS; do
      curl -fsI -m 8 "$MIRROR/config" >/dev/null 2>&1 || continue
      curl -fsSL -m 30 "$MIRROR/flathub.gpg" -o /tmp/flathub-mirror.gpg 2>/dev/null || continue
      [ -s /tmp/flathub-mirror.gpg ] || continue
      if flatpak remote-add --user --if-not-exists --gpg-import=/tmp/flathub-mirror.gpg flathub-mirror "$MIRROR"; then
        echo "Using Flathub mirror: $MIRROR"
        MIRROR_OK=1
        break
      fi
    done
    if [ -z "$MIRROR_OK" ]; then
      echo "The GNOME runtime ($RUNTIME) is not installed and neither Flathub nor its"
      echo "mirrors are reachable. Connect a VPN for this first install only: later"
      echo "updates come from GitHub and install from the launcher's settings."
      exit 1
    fi
  fi
fi

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
