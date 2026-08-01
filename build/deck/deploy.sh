#!/usr/bin/env bash
# Fast dev loop: frontend builds on the host, sources rsync to the
# Deck, the Go binary builds there inside the GNOME SDK and replaces
# the installed flatpak's binary. Releases still go through CI.
set -euo pipefail

HOST="${DECK_HOST:-steamdeck}"
VERSION="dev-$(git rev-parse --short HEAD)"

echo "[1/4] Frontend build..."
npm --prefix frontend run build >/dev/null 2>&1

echo "[2/4] Sync sources to the Deck..."
rsync -az --delete \
  --exclude .git \
  --exclude node_modules \
  --exclude build/bin \
  --exclude .scratch \
  ./ "$HOST":dev/deckanator/

echo "[3/4] Build inside the SDK on the Deck..."
ssh "$HOST" "flatpak run --command=sh --filesystem=home --share=network org.gnome.Sdk//50 -c 'sh \$HOME/dev/deckanator/build/deck/remote-build.sh $VERSION'"

echo "[4/4] Swap the binary and restart..."
ssh "$HOST" 'sh $HOME/dev/deckanator/build/deck/install-run.sh'
echo "Deployed $VERSION (log: task deck:log)"
