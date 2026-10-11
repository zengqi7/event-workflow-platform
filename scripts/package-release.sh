#!/usr/bin/env bash
set -euo pipefail
# Run on Linux with Go, tar, zip and sha256sum; external scanners are not bundled.
version="${1:-snapshot}"
if [[ ! "$version" =~ ^[A-Za-z0-9._-]+$ ]]; then
  echo "invalid version" >&2
  exit 1
fi
out="dist/$version"
[[ ! -e "$out" ]] || { echo "output already exists; use a new version" >&2; exit 1; }
mkdir -p "$out"
for target in linux/amd64 linux/arm64 windows/amd64 windows/arm64 darwin/amd64 darwin/arm64; do
  os="${target%/*}"
  arch="${target#*/}"
  bundle="$out/event-workflow-platform-$version-$os-$arch"
  mkdir -p "$bundle/bin"
  for command in http-server worker migrate key inspect webhook-sink; do
    suffix=""
    [[ "$os" != windows ]] || suffix=".exe"
    CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go build -trimpath -ldflags="-s -w" -o "$bundle/bin/$command$suffix" "./cmd/$command"
  done
  cp README.md LICENSE "$bundle/"
  cp -r docs migrations "$bundle/"
  base="$(basename "$bundle")"
  if [[ "$os" == windows ]]; then
    (cd "$out" && zip -qr "$base.zip" "$base")
  else
    tar -czf "$out/$base.tar.gz" -C "$out" "$base"
  fi
done
(cd "$out" && sha256sum ./*.tar.gz ./*.zip > checksums.txt)
