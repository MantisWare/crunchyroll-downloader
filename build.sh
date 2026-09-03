#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")" && pwd)"
cd "$REPO_ROOT"

mkdir -p bin

BINARY_NAME="crunchyroll-downloader"
if [[ "$(uname -s)" == MINGW* || "$(uname -s)" == MSYS* || "$(uname -s)" == CYGWIN* ]]; then
  BINARY_NAME="crunchyroll-downloader.exe"
fi

echo "Building CLI → bin/${BINARY_NAME}"
CGO_ENABLED=0 go build -ldflags="-s -w" -o "bin/${BINARY_NAME}" .

echo "Done: bin/${BINARY_NAME}"
