#!/usr/bin/env bash
# Promotes the newest -rc.N tag: tags the same commit without the
# suffix and pushes, making it the stable release CI builds.
set -euo pipefail

git fetch --tags --quiet
LAST=$(git tag --list 'v*-rc.*' --sort=-v:refname | head -1)
[ -n "$LAST" ] || { echo "no rc tags found"; exit 1; }

STABLE="${LAST%-rc.*}"
if git rev-parse "$STABLE" >/dev/null 2>&1; then
  echo "$STABLE already exists"
  exit 1
fi

COMMIT=$(git rev-list -n 1 "$LAST")
echo "promoting $LAST -> $STABLE ($COMMIT)"
git tag "$STABLE" "$COMMIT"
git push origin "$STABLE"
