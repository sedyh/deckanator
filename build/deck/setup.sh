#!/usr/bin/env bash
# One-time Steam Deck dev setup over ssh: the GNOME SDK matching the
# app's runtime plus a Go toolchain in the (writable) home directory.
set -euo pipefail

HOST="${DECK_HOST:-steamdeck}"
GO_VERSION="1.25.8"

echo "[1/3] GNOME SDK 50 on the Deck..."
ssh "$HOST" '
  set -e
  if flatpak info --user org.gnome.Sdk//50 >/dev/null 2>&1 || flatpak info org.gnome.Sdk//50 >/dev/null 2>&1; then
    echo "  SDK already installed"
    exit 0
  fi
  if curl -fsI -m 8 https://dl.flathub.org/repo/config >/dev/null 2>&1; then
    flatpak remote-add --user --if-not-exists flathub https://dl.flathub.org/repo/flathub.flatpakrepo
  else
    echo "  Flathub unreachable, trying mirrors..."
    for M in https://mirror.sjtu.edu.cn/flathub https://mirrors.ustc.edu.cn/flathub; do
      curl -fsI -m 8 "$M/config" >/dev/null 2>&1 || continue
      curl -fsSL -m 30 "$M/flathub.gpg" -o /tmp/flathub-mirror.gpg || continue
      flatpak remote-add --user --if-not-exists --gpg-import=/tmp/flathub-mirror.gpg flathub-mirror "$M" && break
    done
  fi
  flatpak install --user -y --noninteractive org.gnome.Sdk//50
'

echo "[2/3] Go toolchain on the Deck..."
ssh "$HOST" "
  set -e
  if [ -x \$HOME/dev/go/bin/go ] && \$HOME/dev/go/bin/go version | grep -q go$GO_VERSION; then
    echo \"  go$GO_VERSION already installed\"
  else
    mkdir -p \$HOME/dev
    curl -fsSL -o /tmp/go.tgz https://go.dev/dl/go$GO_VERSION.linux-amd64.tar.gz
    rm -rf \$HOME/dev/go
    tar -C \$HOME/dev -xzf /tmp/go.tgz
    rm -f /tmp/go.tgz
  fi
  \$HOME/dev/go/bin/go version
"

echo "[3/3] Source directory..."
ssh "$HOST" 'mkdir -p $HOME/dev/deckanator'
echo "Deck is ready: task deck:deploy"
