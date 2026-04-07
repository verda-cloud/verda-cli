#!/bin/sh
# Verda CLI installer
# Usage: curl -sSL https://raw.githubusercontent.com/verda-cloud/verda-cli/main/scripts/install.sh | sh
#
# Environment variables:
#   VERDA_INSTALL_DIR  - Installation directory (default: ~/.verda/bin)
#   VERDA_VERSION      - Specific version to install (default: latest)

set -e

REPO="verda-cloud/verda-cli"
BINARY="verda"
INSTALL_DIR="${VERDA_INSTALL_DIR:-$HOME/.verda/bin}"

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
mkdir -p "$INSTALL_DIR"
mv "$BINARY" "$INSTALL_DIR/$BINARY"
chmod +x "$INSTALL_DIR/$BINARY"

# Ensure ~/.verda/bin is in PATH
setup_path() {
  case ":$PATH:" in
    *":$INSTALL_DIR:"*) return ;; # already in PATH
  esac

  SHELL_NAME="$(basename "$SHELL")"
  case "$SHELL_NAME" in
    zsh)  RC_FILE="$HOME/.zshrc" ;;
    bash)
      if [ -f "$HOME/.bashrc" ]; then
        RC_FILE="$HOME/.bashrc"
      else
        RC_FILE="$HOME/.bash_profile"
      fi
      ;;
    fish) RC_FILE="$HOME/.config/fish/config.fish" ;;
    *)    RC_FILE="$HOME/.profile" ;;
  esac

  PATH_LINE="export PATH=\"$INSTALL_DIR:\$PATH\""
  if [ "$SHELL_NAME" = "fish" ]; then
    PATH_LINE="set -gx PATH $INSTALL_DIR \$PATH"
  fi

  if [ -f "$RC_FILE" ] && grep -qF "$INSTALL_DIR" "$RC_FILE" 2>/dev/null; then
    return # already configured
  fi

  echo "" >> "$RC_FILE"
  echo "# Added by Verda CLI installer" >> "$RC_FILE"
  echo "$PATH_LINE" >> "$RC_FILE"
  echo "  Added $INSTALL_DIR to PATH in $RC_FILE"
  echo "  Run: source $RC_FILE  (or open a new terminal)"
}

# Only set up PATH if using the default location
if [ "$VERDA_INSTALL_DIR" = "" ]; then
  setup_path
fi

echo ""
echo "Verda CLI ${VERDA_VERSION} installed successfully!"
echo ""
echo "Get started:"
echo "  verda auth login     # Configure credentials"
echo "  verda vm list        # List VM instances"
echo "  verda --help         # See all commands"

# Warn about old binary in system path
OLD_BINARY="$(command -v verda 2>/dev/null || true)"
if [ -n "$OLD_BINARY" ] && [ "$OLD_BINARY" != "$INSTALL_DIR/$BINARY" ]; then
  echo ""
  echo "Warning: an older verda binary exists at $OLD_BINARY"
  echo "  Remove it to avoid conflicts: sudo rm $OLD_BINARY"
fi
