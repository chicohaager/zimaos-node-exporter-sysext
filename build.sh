#!/bin/sh
# Build the node-exporter sysext for ZimaOS.
#   sh build.sh [amd64|arm64]      (default: amd64)
# Output in dist/: node-exporter-<arch>.raw (+ .sha256), for amd64 also node-exporter.raw.
set -eu
cd "$(dirname "$0")"
ARCH="${1:-amd64}"
MODKIT="${LINTUX_MODKIT:-../lintux-modkit}"
[ -f "$MODKIT/sysext/add-web.sh" ] || { echo "lintux-modkit with sysext/add-web.sh not found at $MODKIT (set LINTUX_MODKIT)" >&2; exit 1; }
VERSION=$(cat VERSION)

# Upstream releases, pinned by version AND sha256 (from the sha256sums.txt of each release, 2026-10-25).
NE_VERSION=1.12.1
SE_VERSION=0.15.0
case "$ARCH" in
  amd64) NE_SUM=b51d8a76aa2a9156a55d501aca6276fae09e262259a5e4e831d2c2222f084e63
         SE_SUM=32ff269f2ca5708e2aa8209c715192efcc32f87bd575cae50a76d391a484976e ;;
  arm64) NE_SUM=ad35b605f9954b9f1ffddf5ba054bdc5a98d790b9eae5291e1eeb83f1ecbd0e7
         SE_SUM=9a870fe6401748f51f5edcd73057f1c90c0b975f1f48d986eacc61443320276c ;;
  *) echo "unsupported arch: $ARCH" >&2; exit 2 ;;
esac
NE="node_exporter-$NE_VERSION.linux-$ARCH"
SE="smartctl_exporter-$SE_VERSION.linux-$ARCH"

rm -rf build
mkdir -p build/root build/dl
curl -fsSL -o "build/dl/$NE.tar.gz" "https://github.com/prometheus/node_exporter/releases/download/v$NE_VERSION/$NE.tar.gz"
curl -fsSL -o "build/dl/$SE.tar.gz" "https://github.com/prometheus-community/smartctl_exporter/releases/download/v$SE_VERSION/$SE.tar.gz"
printf '%s  %s\n%s  %s\n' "$NE_SUM" "build/dl/$NE.tar.gz" "$SE_SUM" "build/dl/$SE.tar.gz" | sha256sum -c -
tar -xzf "build/dl/$NE.tar.gz" -C build/dl
tar -xzf "build/dl/$SE.tar.gz" -C build/dl
go vet ./...
go test -count=1 ./...

cp -a files/. build/root/
chmod 755 build/root/usr/bin/node-exporter-zima
install -m 755 "build/dl/$NE/node_exporter" "build/dl/$SE/smartctl_exporter" build/root/usr/bin/
mkdir -p build/root/usr/libexec/node-exporter build/root/usr/share/doc/node-exporter
cp "$MODKIT/sysext/unit-watchdog.sh" build/root/usr/libexec/node-exporter/unit-watchdog.sh
cp "build/dl/$NE/LICENSE" build/root/usr/share/doc/node-exporter/LICENSE.node_exporter
cp "build/dl/$SE/LICENSE" build/root/usr/share/doc/node-exporter/LICENSE.smartctl_exporter
printf 'node_exporter %s (%s.tar.gz sha256 %s)\nsmartctl_exporter %s (%s.tar.gz sha256 %s)\n' \
  "$NE_VERSION" "$NE" "$NE_SUM" "$SE_VERSION" "$SE" "$SE_SUM" > build/root/usr/share/doc/node-exporter/packages.txt
cp README.md build/root/usr/share/doc/node-exporter/README
# The tile: module JSON (its "services" make zpkg enable and start node-exporter-web; without the
# JSON zpkg answers "module not installed", rc=1 — measured 1.8.0), UI files and the backend.
sh "$MODKIT/sysext/add-web.sh" node-exporter build/root "$VERSION"
# -buildvcs=false: without it Go stamps the nearest tag and "dirty" (any untracked file) into the binary,
# so a rebuild from the release tag differed from the release (measured 2026-10-23, restic-web).
CGO_ENABLED=0 GOOS=linux GOARCH="$ARCH" go build -trimpath -buildvcs=false -ldflags="-s -w -X main.version=$VERSION" \
  -o build/root/usr/bin/node-exporter-web ./cmd/node-exporter-web
sh "$MODKIT/sysext/build-sysext.sh" node-exporter build/root "$ARCH" dist "$VERSION"
cat "dist/node-exporter-$ARCH.raw.sha256"
