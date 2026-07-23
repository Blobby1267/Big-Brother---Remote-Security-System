#!/usr/bin/env bash
set -euo pipefail

# Resolve project-relative paths so script works from any current directory.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
OUTPUT_PATH="${1:-$PROJECT_ROOT/relay-server}"
TARGET_GOARCH="${GOARCH:-arm64}"

# Build Linux relay binary for Raspberry Pi-compatible architecture.
cd "$PROJECT_ROOT/relay"
GOOS=linux GOARCH="$TARGET_GOARCH" go build -o "$OUTPUT_PATH" .

echo "Built relay binary at $OUTPUT_PATH"