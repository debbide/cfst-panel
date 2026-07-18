#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
mkdir -p dist
export CGO_ENABLED=0
GOOS=linux GOARCH=amd64 go build -buildvcs=false -o dist/cfst-panel ./cmd/server
GOOS=linux GOARCH=arm64 go build -buildvcs=false -o dist/cfst-panel-arm64 ./cmd/server
echo "built:"
echo "  dist/cfst-panel        (linux/amd64)"
echo "  dist/cfst-panel-arm64  (linux/arm64)"
