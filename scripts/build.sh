#!/usr/bin/env bash
set -euo pipefail

# Cross-platform output folder for build artifacts.
OUTDIR=dist
mkdir -p "$OUTDIR"

# Module list and target matrix used for cross-compilation.
bins=(agent controller relay)
oses=(linux darwin windows)
archs=(amd64 arm64)

# Produce one binary per module/OS/arch target.
for os in "${oses[@]}"; do
  for arch in "${archs[@]}"; do
    for b in "${bins[@]}"; do
      out="$OUTDIR/${b}-${os}-${arch}"
      if [ "$os" = "windows" ]; then out+='.exe'; fi
      echo "Building $b for $os/$arch -> $out"
      GOOS=$os GOARCH=$arch go build -o "$out" "./$b"
    done
  done
done

echo "Builds written to $OUTDIR"
