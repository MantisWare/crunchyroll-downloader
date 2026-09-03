#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")" && pwd)"
cd "$REPO_ROOT"

mkdir -p bin

OS="$(uname -s)"
ARCH="$(uname -m)"
BINARY_NAME="crunchyroll-downloader-gui"
LDFLAGS="-s -w"

case "$OS" in
  Darwin)
    BINARY_NAME="crunchyroll-downloader-gui"
    ;;
  Linux)
    BINARY_NAME="crunchyroll-downloader-gui"
    ;;
  MINGW*|MSYS*|CYGWIN*)
    BINARY_NAME="crunchyroll-downloader-gui.exe"
    LDFLAGS="-s -w -H windowsgui"
    ;;
esac

if ! command -v go >/dev/null 2>&1; then
  echo "Error: Go is required to build the UI." >&2
  exit 1
fi

echo "Building UI (${OS}/${ARCH}) → bin/${BINARY_NAME}"
echo "Note: the GUI requires CGO and a C compiler (Xcode CLT, gcc, or MinGW)."

CGO_ENABLED=1 go build -tags gui -ldflags="${LDFLAGS}" -o "bin/${BINARY_NAME}" .

echo "Done: bin/${BINARY_NAME}"
echo "Run with: ./bin/${BINARY_NAME}"
