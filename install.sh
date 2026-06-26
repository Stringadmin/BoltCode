#!/bin/sh
# BoltCode installer — downloads the latest release binary for your OS/arch.
#
# Usage:
#   curl -fsSL https://stringtest.top/install.sh | sh
#
# Or from the GitHub raw URL directly:
#   curl -fsSL https://raw.githubusercontent.com/Stringadmin/BoltCode/main/install.sh | sh
#
set -eu

OWNER="Stringadmin"
REPO="BoltCode"
BINARY="boltcode"

# --- detect OS and architecture -----------------------------------------------
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$OS" in
  linux|darwin) ;;
  *) echo "Sorry, BoltCode does not have a prebuilt binary for OS: $OS" >&2; exit 1 ;;
esac

case "$ARCH" in
  x86_64|amd64) ARCH="x86_64" ;;
  arm64|aarch64) ARCH="arm64" ;;
  *) echo "Sorry, BoltCode does not have a prebuilt binary for arch: $ARCH" >&2; exit 1 ;;
esac

# --- resolve the download URL via the GitHub API ------------------------------
echo "Fetching the latest BoltCode release..."

# Query the latest release to find the matching asset. We match on the
# OS/arch name pattern used by GoReleaser (see .goreleaser.yml).
API_URL="https://api.github.com/repos/${OWNER}/${REPO}/releases/latest"
ASSET_URL=$(curl -fsSL "$API_URL" \
  | grep "browser_download_url" \
  | grep "${OS}" \
  | grep "${ARCH}" \
  | head -n 1 \
  | sed -E 's/.*"(https:\/\/[^"]+)".*/\1/')

if [ -z "$ASSET_URL" ]; then
  echo "Could not find a prebuilt binary for ${OS}/${ARCH}." >&2
  echo "Download one manually from: https://github.com/${OWNER}/${REPO}/releases" >&2
  exit 1
fi

# --- pick an install directory ------------------------------------------------
INSTALL_DIR="/usr/local/bin"
if [ -w "$INSTALL_DIR" ]; then
  SUDO=""
else
  SUDO="sudo"
  echo "(installing to ${INSTALL_DIR} requires sudo)"
fi

# --- download, extract, install ----------------------------------------------
TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

echo "Downloading ${ASSET_URL##*/}..."
ARCHIVE="${TMPDIR}/${ASSET_URL##*/}"
curl -fsSL "$ASSET_URL" -o "$ARCHIVE"

echo "Extracting..."
case "$ASSET_URL" in
  *.tar.gz)
    tar -xzf "$ARCHIVE" -C "$TMPDIR"
    ;;
  *.zip)
    # unzip may not be installed on minimal Linux; fall back to python.
    if command -v unzip >/dev/null 2>&1; then
      unzip -o "$ARCHIVE" -d "$TMPDIR" >/dev/null
    else
      python3 -c "import zipfile,sys; zipfile.ZipFile(sys.argv[1]).extractall(sys.argv[2])" "$ARCHIVE" "$TMPDIR"
    fi
    ;;
  *)
    echo "Unknown archive format: $ASSET_URL" >&2; exit 1 ;;
esac

# Locate the binary (it may be at the top level or nested one dir deep).
BIN_PATH=""
for candidate in "$TMPDIR/$BINARY" "$TMPDIR"/*/"$BINARY"; do
  if [ -f "$candidate" ]; then
    BIN_PATH="$candidate"
    break
  fi
done
if [ -z "$BIN_PATH" ]; then
  echo "Could not find the '$BINARY' binary in the archive." >&2; exit 1
fi

echo "Installing to ${INSTALL_DIR}/${BINARY}..."
$SUDO install -m 0755 "$BIN_PATH" "${INSTALL_DIR}/${BINARY}"

# --- verify -------------------------------------------------------------------
echo
if command -v "$BINARY" >/dev/null 2>&1; then
  echo "✓ BoltCode installed. Run: boltcode --help"
else
  echo "✓ Installed to ${INSTALL_DIR}/${BINARY} (may not be on your PATH yet)."
  echo "  Add ${INSTALL_DIR} to your PATH, or run it directly."
fi
