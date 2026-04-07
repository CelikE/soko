#!/bin/sh
# soko install script
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/CelikE/soko/master/install.sh | sh
#   curl -fsSL https://raw.githubusercontent.com/CelikE/soko/master/install.sh | sh -s -- --dir ~/.local/bin
#   curl -fsSL https://raw.githubusercontent.com/CelikE/soko/master/install.sh | sh -s -- --version v0.13.0

set -e

REPO="CelikE/soko"
BINARY="soko"
INSTALL_DIR="/usr/local/bin"
VERSION=""

# Parse flags.
while [ $# -gt 0 ]; do
  case "$1" in
    --dir)
      INSTALL_DIR="$2"
      shift 2
      ;;
    --version)
      VERSION="$2"
      shift 2
      ;;
    *)
      echo "unknown flag: $1"
      exit 1
      ;;
  esac
done

# Detect OS.
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$OS" in
  linux)  OS="linux" ;;
  darwin) OS="darwin" ;;
  mingw*|msys*|cygwin*) OS="windows" ;;
  *)
    echo "unsupported OS: $OS"
    exit 1
    ;;
esac

# Detect architecture.
ARCH=$(uname -m)
case "$ARCH" in
  x86_64|amd64)  ARCH="amd64" ;;
  arm64|aarch64) ARCH="arm64" ;;
  *)
    echo "unsupported architecture: $ARCH"
    exit 1
    ;;
esac

# Determine version.
if [ -z "$VERSION" ]; then
  VERSION=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | cut -d '"' -f4)
  if [ -z "$VERSION" ]; then
    echo "failed to determine latest version"
    exit 1
  fi
fi

VERSION_NUM="${VERSION#v}"

# Determine file extension.
EXT="tar.gz"
if [ "$OS" = "windows" ]; then
  EXT="zip"
fi

# Download.
FILENAME="${BINARY}_${VERSION_NUM}_${OS}_${ARCH}.${EXT}"
URL="https://github.com/${REPO}/releases/download/${VERSION}/${FILENAME}"

echo "downloading soko ${VERSION} for ${OS}/${ARCH}..."

TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

curl -fsSL "$URL" -o "${TMPDIR}/${FILENAME}"

# Extract.
cd "$TMPDIR"
if [ "$EXT" = "zip" ]; then
  unzip -q "$FILENAME"
else
  tar xzf "$FILENAME"
fi

# Install.
mkdir -p "$INSTALL_DIR"

if [ -w "$INSTALL_DIR" ]; then
  cp "$BINARY" "$INSTALL_DIR/"
else
  echo "installing to ${INSTALL_DIR} (requires sudo)..."
  sudo cp "$BINARY" "$INSTALL_DIR/"
fi

chmod +x "${INSTALL_DIR}/${BINARY}"

echo "soko ${VERSION} installed to ${INSTALL_DIR}/${BINARY}"

# Verify.
if command -v soko >/dev/null 2>&1; then
  echo ""
  soko version
else
  echo ""
  echo "note: ${INSTALL_DIR} may not be on your PATH"
  echo "add this to your shell profile:"
  echo "  export PATH=\"\$PATH:${INSTALL_DIR}\""
fi
