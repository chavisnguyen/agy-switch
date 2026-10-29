#!/usr/bin/env bash
set -euo pipefail

# Colors
GREEN='\033[0;32m'
CYAN='\033[0;36m'
YELLOW='\033[1;33m'
RESET='\033[0m'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

echo -e "${CYAN}==> Building agy-switch from source...${RESET}"
if ! command -v go >/dev/null 2>&1; then
    echo "Error: Go is not installed. Please install Go to build agy-switch." >&2
    exit 1
fi

go build -ldflags="-s -w" -o agy-switch .
echo -e "${GREEN}✓ Build succeeded.${RESET}"

# Install binary to global path
INSTALL_TARGETS=(
    "/opt/homebrew/bin/agy-switch"
    "$HOME/.local/bin/agy-switch"
    "/usr/local/bin/agy-switch"
)

INSTALLED=0
for target in "${INSTALL_TARGETS[@]}"; do
    target_dir="$(dirname "$target")"
    if [ -w "$target_dir" ] || [ -w "$(dirname "$target_dir")" ]; then
        mkdir -p "$target_dir" 2>/dev/null || true
        if [ -w "$target_dir" ]; then
            rm -f "$target"
            ln -sf "$SCRIPT_DIR/agy-switch" "$target"
            echo -e "${GREEN}✓ Global shortcut linked:${RESET} $target -> $SCRIPT_DIR/agy-switch"
            INSTALLED=1
            break
        fi
    fi
done

if [ "$INSTALLED" -eq 0 ]; then
    echo -e "${YELLOW}Warning: Could not link to system bin. Trying ~/.local/bin...${RESET}"
    mkdir -p "$HOME/.local/bin"
    ln -sf "$SCRIPT_DIR/agy-switch" "$HOME/.local/bin/agy-switch"
    echo -e "${GREEN}✓ Linked to:${RESET} $HOME/.local/bin/agy-switch"
fi

# Set up shell alias in ~/.zshrc and ~/.bashrc
ALIAS_LINE='alias agy="agy-switch run"'
SHELL_CONFIGS=()
[ -f "$HOME/.zshrc" ] && SHELL_CONFIGS+=("$HOME/.zshrc")
[ -f "$HOME/.bashrc" ] && SHELL_CONFIGS+=("$HOME/.bashrc")

# If neither exists, default to .zshrc on macOS
if [ ${#SHELL_CONFIGS[@]} -eq 0 ]; then
    SHELL_CONFIGS+=("$HOME/.zshrc")
    touch "$HOME/.zshrc"
fi

for rc in "${SHELL_CONFIGS[@]}"; do
    if grep -q "alias agy=" "$rc"; then
        echo -e "${CYAN}ℹ Alias already exists in:${RESET} $rc"
    else
        echo "" >> "$rc"
        echo "# agy-switch auto-failover wrapper" >> "$rc"
        echo "$ALIAS_LINE" >> "$rc"
        echo -e "${GREEN}✓ Added alias to:${RESET} $rc (${CYAN}$ALIAS_LINE${RESET})"
    fi
done

echo ""
echo -e "${GREEN}🎉 Installation completed successfully!${RESET}"
echo -e "👉 Run this command to apply in your current terminal:"
echo -e "   ${CYAN}source ~/.zshrc${RESET}"
echo -e "   ${CYAN}agy${RESET} (will now automatically run ${CYAN}agy-switch run${RESET} with auto-failover)"
