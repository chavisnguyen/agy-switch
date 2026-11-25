#!/usr/bin/env bash
set -euo pipefail

# Colors
GREEN='\033[0;32m'
CYAN='\033[0;36m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
RESET='\033[0m'

REPO="chavisnguyen/agys"
BINARY_NAME="agys"

echo -e "${CYAN}==> Installing ${BINARY_NAME}...${RESET}"

# Detect OS and Architecture
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"

case "$ARCH" in
    x86_64|amd64)
        ARCH="amd64"
        ;;
    arm64|aarch64)
        ARCH="arm64"
        ;;
    *)
        echo -e "${RED}Error: Unsupported architecture: $ARCH${RESET}" >&2
        exit 1
        ;;
esac

if [ "$OS" != "darwin" ] && [ "$OS" != "linux" ]; then
    echo -e "${RED}Error: Unsupported OS: $OS (only macOS and Linux are supported)${RESET}" >&2
    exit 1
fi

TEMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TEMP_DIR"' EXIT
TARGET_FILE="$TEMP_DIR/$BINARY_NAME"

# Check if Go is installed and we are inside source tree
if command -v go >/dev/null 2>&1 && [ -f "go.mod" ] && grep -q "^module agys$" "go.mod" 2>/dev/null; then
    echo -e "${CYAN}==> Go detected in source repo. Compiling from source...${RESET}"
    GIT_TAG="$(git describe --tags --exact-match 2>/dev/null || true)"
    LDFLAGS="-s -w"
    if [ -n "$GIT_TAG" ]; then
        LDFLAGS="$LDFLAGS -X main.Version=$GIT_TAG"
    fi
    go build -ldflags="$LDFLAGS" -o "$TARGET_FILE" .
    echo -e "${GREEN}✓ Local build succeeded.${RESET}"
else
    # Download pre-built binary from GitHub Releases
    ASSET_NAME="${BINARY_NAME}-${OS}-${ARCH}"
    DOWNLOAD_URL="https://github.com/${REPO}/releases/latest/download/${ASSET_NAME}"
    CHECKSUMS_URL="https://github.com/${REPO}/releases/latest/download/checksums.txt"
    CHECKSUMS_FILE="$TEMP_DIR/checksums.txt"

    echo -e "${CYAN}==> Downloading pre-compiled binary (${ASSET_NAME})...${RESET}"
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL "$DOWNLOAD_URL" -o "$TARGET_FILE" || {
            echo -e "${RED}Error: Failed to download ${DOWNLOAD_URL}${RESET}" >&2
            echo -e "${YELLOW}Tip: Ensure a GitHub Release exists with pre-built binaries.${RESET}" >&2
            exit 1
        }
        HTTP_CODE="$(curl -sSL -w "%{http_code}" "$CHECKSUMS_URL" -o "$CHECKSUMS_FILE" || echo "000")"
    elif command -v wget >/dev/null 2>&1; then
        wget -qO "$TARGET_FILE" "$DOWNLOAD_URL" || {
            echo -e "${RED}Error: Failed to download ${DOWNLOAD_URL}${RESET}" >&2
            exit 1
        }
        if wget -qO "$CHECKSUMS_FILE" "$CHECKSUMS_URL" 2>/dev/null; then
            HTTP_CODE="200"
        else
            HTTP_CODE="404"
        fi
    else
        echo -e "${RED}Error: Neither curl nor wget is available.${RESET}" >&2
        exit 1
    fi

    if [ "$HTTP_CODE" = "200" ] && [ -s "$CHECKSUMS_FILE" ]; then
        EXPECTED_SHA="$(awk -v asset="$ASSET_NAME" '{gsub(/^\*|\.\//, "", $2); if ($2 == asset) print tolower($1)}' "$CHECKSUMS_FILE")"
        if [ -z "$EXPECTED_SHA" ]; then
            echo -e "${RED}Error: ${ASSET_NAME} not found in checksums.txt${RESET}" >&2
            exit 1
        fi
        if command -v sha256sum >/dev/null 2>&1; then
            ACTUAL_SHA="$(sha256sum "$TARGET_FILE" | awk '{print tolower($1)}')"
        elif command -v shasum >/dev/null 2>&1; then
            ACTUAL_SHA="$(shasum -a 256 "$TARGET_FILE" | awk '{print tolower($1)}')"
        else
            echo -e "${RED}Error: Neither sha256sum nor shasum is available to verify binary.${RESET}" >&2
            exit 1
        fi
        if [ "$ACTUAL_SHA" != "$EXPECTED_SHA" ]; then
            echo -e "${RED}Error: SHA-256 checksum mismatch for ${ASSET_NAME}!${RESET}" >&2
            echo -e "  Expected: ${EXPECTED_SHA}" >&2
            echo -e "  Actual  : ${ACTUAL_SHA}" >&2
            exit 1
        fi
        echo -e "${GREEN}✓ Verified SHA-256 checksum.${RESET}"
    elif [ "$HTTP_CODE" = "404" ]; then
        echo -e "${YELLOW}⚠️  No checksums.txt in release; skipping SHA-256 verification.${RESET}"
    else
        echo -e "${RED}Error: Could not verify checksums.txt (HTTP ${HTTP_CODE}).${RESET}" >&2
        exit 1
    fi

    chmod +x "$TARGET_FILE"
    echo -e "${GREEN}✓ Download complete.${RESET}"
fi

# Determine install destination
INSTALL_DIRS=(
    "/opt/homebrew/bin"
    "/usr/local/bin"
    "$HOME/.local/bin"
    "$HOME/bin"
)

INSTALLED_PATH=""
for dir in "${INSTALL_DIRS[@]}"; do
    if [ -d "$dir" ] && [ -w "$dir" ]; then
        rm -f "$dir/$BINARY_NAME"
        cp -f "$TARGET_FILE" "$dir/$BINARY_NAME"
        chmod +x "$dir/$BINARY_NAME"
        if [ "$OS" = "darwin" ] && command -v codesign >/dev/null 2>&1; then
            codesign -s - --force "$dir/$BINARY_NAME" 2>/dev/null || true
        fi
        INSTALLED_PATH="$dir/$BINARY_NAME"
        break
    fi
done

# If no writable system directory found, try ~/.local/bin
if [ -z "$INSTALLED_PATH" ]; then
    USER_BIN="$HOME/.local/bin"
    mkdir -p "$USER_BIN"
    rm -f "$USER_BIN/$BINARY_NAME"
    cp -f "$TARGET_FILE" "$USER_BIN/$BINARY_NAME"
    chmod +x "$USER_BIN/$BINARY_NAME"
    INSTALLED_PATH="$USER_BIN/$BINARY_NAME"
fi

# Clean up any legacy agy-switch links/binaries
for dir in "${INSTALL_DIRS[@]}"; do
    rm -f "$dir/agy-switch" 2>/dev/null || true
done

echo ""
echo -e "${GREEN}🎉 ${BINARY_NAME} installed successfully!${RESET}"
echo -e "   Location: ${CYAN}${INSTALLED_PATH}${RESET}"

# Verify if installed dir is in PATH
INSTALLED_DIR="$(dirname "$INSTALLED_PATH")"
case ":$PATH:" in
    *":$INSTALLED_DIR:"*)
        ;;
    *)
        echo ""
        echo -e "${YELLOW}⚠️  Note: $INSTALLED_DIR is not in your \$PATH.${RESET}"
        echo -e "   Add this line to your ~/.zshrc or ~/.bashrc:"
        echo -e "   ${CYAN}export PATH=\"$INSTALLED_DIR:\$PATH\"${RESET}"
        ;;
esac

echo ""
echo -e "👉 Quick Start:"
echo -e "   ${CYAN}agys${RESET}            # Open interactive menu"
echo -e "   ${CYAN}agys status${RESET}     # View accounts & real-time quota"
echo -e "   ${CYAN}agys next${RESET}       # Rotate to next account"
echo -e "   ${CYAN}agys run${RESET}        # Run agy with auto-failover"
