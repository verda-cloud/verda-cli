#!/bin/sh
# Verda CLI installer
# Usage: curl -sSL https://raw.githubusercontent.com/verda-cloud/verda-cli/main/scripts/install.sh | sh
#
# Environment variables:
#   VERDA_INSTALL_DIR        - Installation directory (default: ~/.verda/bin)
#   VERDA_VERSION            - Specific version to install (default: latest)
#   VERDA_INSTALL_SKIP_VERIFY - Set to 1 to skip archive checksum verification (NOT recommended)
#   VERDA_INSTALL_BASE_URL   - Override release asset base URL (testing only)

set -e

REPO="verda-cloud/verda-cli"
BINARY="verda"
INSTALL_DIR="${VERDA_INSTALL_DIR:-$HOME/.verda/bin}"
BASE_URL="${VERDA_INSTALL_BASE_URL:-https://github.com/${REPO}/releases/download}"

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
URL="${BASE_URL}/${VERDA_VERSION}/${FILENAME}"
SUMS_FILENAME="${BINARY}_${VERSION_NUM}_SHA256SUMS"
SUMS_URL="${BASE_URL}/${VERDA_VERSION}/${SUMS_FILENAME}"

echo "Installing Verda CLI ${VERDA_VERSION} (${OS}/${ARCH})..."
echo "  From: ${URL}"
echo "  To:   ${INSTALL_DIR}/${BINARY}"

# Create temp directory
TMP_DIR=$(mktemp -d)
trap 'rm -rf "$TMP_DIR"' EXIT

fail_verify() {
  echo "Error: $1"
  echo "Installation aborted; nothing was installed."
  echo "To bypass checksum verification (NOT recommended), re-run with VERDA_INSTALL_SKIP_VERIFY=1"
  exit 1
}

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

# Verify the archive against the release's SHA256SUMS (fail closed).
if [ "${VERDA_INSTALL_SKIP_VERIFY:-}" != "1" ]; then
  echo "Verifying checksum..."
  if command -v curl >/dev/null 2>&1; then
    curl -sSfL "$SUMS_URL" -o "${TMP_DIR}/${SUMS_FILENAME}" || fail_verify "could not download checksum file from ${SUMS_URL}"
  else
    wget -q "$SUMS_URL" -O "${TMP_DIR}/${SUMS_FILENAME}" || fail_verify "could not download checksum file from ${SUMS_URL}"
  fi

  # The sums file lists every platform asset; check only this archive's line.
  awk -v name="$FILENAME" '$2 == name' "${TMP_DIR}/${SUMS_FILENAME}" > "${TMP_DIR}/CHECKSUM"
  if [ ! -s "${TMP_DIR}/CHECKSUM" ]; then
    fail_verify "no checksum entry for ${FILENAME} in ${SUMS_FILENAME}"
  fi

  cd "$TMP_DIR"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum -c CHECKSUM > /dev/null || fail_verify "checksum mismatch for ${FILENAME}"
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 -c CHECKSUM > /dev/null || fail_verify "checksum mismatch for ${FILENAME}"
  else
    fail_verify "no SHA-256 checksum tool available (need sha256sum or shasum)"
  fi
  echo "  Checksum OK."
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
