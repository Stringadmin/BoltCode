#!/bin/sh
# BoltCode installer — downloads the latest release binary for your OS/arch.
#
# Usage:
#   curl -fsSL https://stringtest.top/install.sh | sh
#
# Or from the GitHub raw URL directly:
#   curl -fsSL https://raw.githubusercontent.com/Stringadmin/BoltCode/main/install.sh | sh
#
# Insecure piped installs are risky, so this script verifies the SHA-256
# checksum published alongside each release (checksums.txt, produced by
# GoReleaser) before extracting or running anything. A MITM or a tampered
# asset is rejected.
set -eu
# pipefail makes the curl|grep|head pipelines fail if curl fails, instead of
# silently swallowing the error when head closes the pipe early.
set -o pipefail 2>/dev/null || true

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
ASSET_URL=$(curl -fsSL --retry 3 --max-time 30 "$API_URL" \
  | grep "browser_download_url" \
  | grep "${OS}" \
  | grep "${ARCH}" \
  | grep -v "checksums" \
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

# --- download, verify, extract, install ---------------------------------------
TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

ARCHIVE_NAME="${ASSET_URL##*/}"
ARCHIVE="${TMPDIR}/${ARCHIVE_NAME}"
echo "Downloading ${ARCHIVE_NAME}..."
curl -fsSL --retry 3 --max-time 120 "$ASSET_URL" -o "$ARCHIVE"

# Verify the SHA-256 checksum published in the release. GoReleaser writes
# checksums.txt as "<sha256>  <filename>" lines; fetch it, find the line for
# this archive, and compare against the locally computed digest. This is the
# critical defense against a tampered or MITM'd download.
CHECKSUMS_URL=$(echo "$ASSET_URL" | sed -E 's|/[^/]+$|/checksums.txt|')
CHECKSUMS="${TMPDIR}/checksums.txt"
if curl -fsSL --retry 3 --max-time 30 "$CHECKSUMS_URL" -o "$CHECKSUMS"; then
  EXPECTED=$(grep "  ${ARCHIVE_NAME}\$" "$CHECKSUMS" | awk '{print $1}')
  if [ -z "$EXPECTED" ]; then
    echo "Checksum file found, but no entry for ${ARCHIVE_NAME}." >&2
    echo "Refusing to install an unverified binary." >&2
    exit 1
  fi
  ACTUAL=$(sha256sum "$ARCHIVE" | awk '{print $1}')
  if [ "$ACTUAL" != "$EXPECTED" ]; then
    echo "Checksum verification FAILED for ${ARCHIVE_NAME}." >&2
    echo "  expected: ${EXPECTED}" >&2
    echo "  actual:   ${ACTUAL}" >&2
    echo "The download may be corrupted or tampered with. Aborting." >&2
    exit 1
  fi
  echo "Checksum verified."
else
  echo "WARNING: could not download checksums.txt — the binary will NOT be" >&2
  echo "verified. Install at your own risk, or download manually from:" >&2
  echo "  https://github.com/${OWNER}/${REPO}/releases" >&2
  printf "  proceed anyway? [y/N] "
  read -r ANSWER
  case "$ANSWER" in
    y|Y|yes|YES) ;;
    *) echo "Aborting."; exit 1 ;;
  esac
fi

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
