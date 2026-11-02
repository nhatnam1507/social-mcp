#!/usr/bin/env bash
# Build static social-mcp binaries for every supported OS/arch into $OUT (default: dist).
set -euo pipefail

cd "$(dirname "$0")/.."
VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
OUT="${OUT:-dist}"
rm -rf "$OUT"
mkdir -p "$OUT"

for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64; do
  os="${target%/*}"
  arch="${target#*/}"
  out="${OUT}/social-mcp-${os}-${arch}"
  [ "$os" = windows ] && out="${out}.exe"
  echo "building ${out}"
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o "$out" ./cmd/social-mcp
done

(cd "$OUT" && sha256sum social-mcp-* > SHA256SUMS)
echo "done: $(ls "$OUT" | wc -l) files in ${OUT}/ (version ${VERSION})"
