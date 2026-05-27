#!/bin/bash
# 交叉编译 vless-audit 为所有平台
# 产出放在 dist/ 目录
set -e

VERSION=$(git describe --tags --always 2>/dev/null || echo "dev")
BUILD_TIME=$(date -u +%Y-%m-%dT%H:%M:%SZ)

PLATFORMS=(
  "linux/amd64"
  "linux/arm64"
  "windows/amd64"
  "darwin/amd64"
  "darwin/arm64"
)

mkdir -p dist

for PLATFORM in "${PLATFORMS[@]}"; do
  GOOS="${PLATFORM%/*}"
  GOARCH="${PLATFORM#*/}"
  OUTPUT="dist/vless-audit-${GOOS}-${GOARCH}"
  if [ "$GOOS" = "windows" ]; then OUTPUT="${OUTPUT}.exe"; fi

  echo "编译 $GOOS/$GOARCH → $OUTPUT"
  GOOS=$GOOS GOARCH=$GOARCH CGO_ENABLED=0 go build \
    -buildvcs=false \
    -ldflags="-s -w -X main.version=${VERSION} -X main.buildTime=${BUILD_TIME}" \
    -o "$OUTPUT" \
    ./cmd/vless-audit/
done

echo ""
echo "编译完成:"
ls -lh dist/
