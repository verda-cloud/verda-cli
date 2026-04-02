#!/bin/sh
# Verda CLI installer
# Usage: curl -sSL https://raw.githubusercontent.com/verda-cloud/verda-cli/main/scripts/install.sh | sh
#
# Environment variables:
#   VERDA_INSTALL_DIR  - Installation directory (default: /usr/local/bin)
#   VERDA_VERSION      - Specific version to install (default: latest)

set -e

REPO="verda-cloud/verda-cli"
BINARY="verda"
INSTALL_DIR="${VERDA_INSTALL_DIR:-/usr/local/bin}"

# Detect OS
OS="$(uname -s)"
case "$OS" in
  Linux*)  OS="linux" ;;
  Darwin*) OS="darwin" ;;
  MINGW*|MSYS*|CYGWIN*) OS="windows" ;;
  *)
    echo "Error: Unsupported operating system: $OS"
    exit 1
    ;;
esac

# Detect architecture
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64)  ARCH="amd64" ;;
  arm64|aarch64) ARCH="arm64" ;;
  *)
    echo "Error: Unsupported architecture: $ARCH"
    exit 1
    ;;
esac

# Resolve version
if [ -z "$VERDA_VERSION" ]; then
  VERDA_VERSION=$(curl -sSf "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | cut -d '"' -f4)
  if [ -z "$VERDA_VERSION" ]; then
    echo "Error: Could not determine latest version. Set VERDA_VERSION manually."
    exit 1
  fi
fi

VERSION_NUM="${VERDA_VERSION#v}"

# Determine archive format
EXT="tar.gz"
if [ "$OS" = "windows" ]; then
  EXT="zip"
fi

FILENAME="${BINARY}_${VERSION_NUM}_${OS}_${ARCH}.${EXT}"
URL="https://github.com/${REPO}/releases/download/${VERDA_VERSION}/${FILENAME}"

echo "Installing Verda CLI ${VERDA_VERSION} (${OS}/${ARCH})..."
echo "  From: ${URL}"
echo "  To:   ${INSTALL_DIR}/${BINARY}"

# Create temp directory
TMP_DIR=$(mktemp -d)
trap 'rm -rf "$TMP_DIR"' EXIT

# Download
echo "Downloading..."
if command -v curl >/dev/null 2>&1; then
  curl -sSfL "$URL" -o "${TMP_DIR}/${FILENAME}"
elif command -v wget >/dev/null 2>&1; then
  wget -q "$URL" -O "${TMP_DIR}/${FILENAME}"
else
  echo "Error: curl or wget is required"
  exit 1
fi

# Extract
echo "Extracting..."
cd "$TMP_DIR"
if [ "$EXT" = "tar.gz" ]; then
  tar xzf "$FILENAME"
elif [ "$EXT" = "zip" ]; then
  unzip -q "$FILENAME"
fi

# Install
if [ -w "$INSTALL_DIR" ]; then
  mv "$BINARY" "$INSTALL_DIR/$BINARY"
else
  echo "Elevating permissions to install to ${INSTALL_DIR}..."
  sudo mv "$BINARY" "$INSTALL_DIR/$BINARY"
fi

chmod +x "$INSTALL_DIR/$BINARY"

echo ""
echo "Verda CLI ${VERDA_VERSION} installed successfully!"
echo ""
echo "Get started:"
echo "  verda auth login     # Configure credentials"
echo "  verda vm list        # List VM instances"
echo "  verda --help         # See all commands"
