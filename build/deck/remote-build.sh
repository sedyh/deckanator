#!/bin/sh
# Runs inside the GNOME SDK sandbox on the Deck: builds the wails app
# the same way `wails build` would (desktop,production tags) against
# the runtime's webkit2gtk-4.1.
set -e

cd "$HOME/dev/deckanator"
export PATH="$HOME/dev/go/bin:$PATH"
export GOPATH="$HOME/dev/gopath"
export GOCACHE="$HOME/dev/gocache"
export GOMODCACHE="$HOME/dev/gomod"
export CGO_ENABLED=1

VERSION="${1:-dev}"
go build -tags desktop,production,webkit2_41 -ldflags "-X main.version=$VERSION" -o "$HOME/dev/Deckanator.new" .
echo "built $VERSION"
