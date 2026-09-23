#!/usr/bin/env bash
# Build a simple .deb for laststate-relay (amd64).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
VERSION="${VERSION:-0.3.0-alpha}"
ARCH="${ARCH:-amd64}"
STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT

mkdir -p "$STAGE/usr/local/bin" \
  "$STAGE/etc/laststate" \
  "$STAGE/lib/systemd/system" \
  "$STAGE/DEBIAN"

(cd "$ROOT" && go build -trimpath -ldflags "-s -w -X main.cliVersion=v$VERSION" \
  -o "$STAGE/usr/local/bin/laststate-relay" ./cmd/laststate-relay)

cp "$ROOT/packaging/systemd/laststate-relay.service" "$STAGE/lib/systemd/system/"
cp "$ROOT/relay.yaml.example" "$STAGE/etc/laststate/relay.yaml"

cat >"$STAGE/DEBIAN/control" <<EOF
Package: laststate-relay
Version: $VERSION
Section: net
Priority: optional
Architecture: $ARCH
Maintainer: Last State contributors
Description: Offline-first Latch to Trace diagnostics gateway
EOF

mkdir -p "$ROOT/dist"
dpkg-deb --build "$STAGE" "$ROOT/dist/laststate-relay_${VERSION}_${ARCH}.deb"
echo "wrote dist/laststate-relay_${VERSION}_${ARCH}.deb"
