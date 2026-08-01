#!/bin/sh
# Runs on the Deck host: swaps the freshly built binary into the
# installed flatpak (breaking the ostree hardlink first) and restarts
# the launcher on the Deck's display. Any real bundle install later
# restores ostree consistency.
set -e

APP=io.github.sedyh.Deckanator
TARGET="$HOME/.local/share/flatpak/app/$APP/current/active/files/bin/Deckanator"

test -f "$HOME/dev/Deckanator.new"
rm -f "$TARGET"
cp "$HOME/dev/Deckanator.new" "$TARGET"
chmod 755 "$TARGET"

systemctl --user stop deck-dbg 2>/dev/null || true
pkill -f "[D]eckanator" 2>/dev/null || true
sleep 1
systemd-run --user --collect --unit=deck-dbg \
  -E DISPLAY=:0 -E WAYLAND_DISPLAY=wayland-0 \
  sh -c "flatpak run $APP > /tmp/deck.log 2>&1"
echo "launcher restarted"
