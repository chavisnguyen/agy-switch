#!/usr/bin/env bash
set -euo pipefail

# Colors
GREEN='\033[0;32m'
CYAN='\033[0;36m'
YELLOW='\033[1;33m'
RESET='\033[0m'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

echo -e "${CYAN}==> Building agys from source...${RESET}"
if ! command -v go >/dev/null 2>&1; then
    echo "Error: Go is not installed. Please install Go to build agys." >&2
    exit 1
fi

go build -ldflags="-s -w" -o agys .
echo -e "${GREEN}✓ Build succeeded: agys${RESET}"

# Install binary to global path
INSTALL_DIRS=(
    "/opt/homebrew/bin"
    "$HOME/.local/bin"
    "/usr/local/bin"
)

INSTALLED=0
for dir in "${INSTALL_DIRS[@]}"; do
    if [ -w "$dir" ] || [ -w "$(dirname "$dir")" ]; then
        mkdir -p "$dir" 2>/dev/null || true
        if [ -w "$dir" ]; then
            rm -f "$dir/agys" "$dir/agy-switch"
            ln -sf "$SCRIPT_DIR/agys" "$dir/agys"
            echo -e "${GREEN}✓ Global shortcut linked:${RESET} $dir/agys -> $SCRIPT_DIR/agys"
            INSTALLED=1
            break
        fi
    fi
done

if [ "$INSTALLED" -eq 0 ]; then
    echo -e "${YELLOW}Warning: Could not link to system bin. Trying ~/.local/bin...${RESET}"
    mkdir -p "$HOME/.local/bin"
    rm -f "$HOME/.local/bin/agy-switch"
    ln -sf "$SCRIPT_DIR/agys" "$HOME/.local/bin/agys"
    echo -e "${GREEN}✓ Linked to:${RESET} $HOME/.local/bin/agys"
fi

echo ""
echo -e "${GREEN}🎉 Installation completed successfully!${RESET}"
echo -e "👉 You can now run ${CYAN}agys${RESET} directly from any terminal:"
echo -e "   ${CYAN}agys${RESET}            # Interactive menu"
echo -e "   ${CYAN}agys status${RESET}     # View account status & quotas"
echo -e "   ${CYAN}agys next${RESET}       # Rotate to next account"
echo -e "   ${CYAN}agys <name>${RESET}     # Switch to profile"
echo -e "   ${CYAN}agys run${RESET}        # Run agy with auto-failover"
