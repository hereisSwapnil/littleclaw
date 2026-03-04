#!/bin/bash
# Littleclaw Installation Script
# This script will install the Littleclaw Nano-Agent locally.

set -e

echo "🦐 Welcome to the Littleclaw Installer!"
echo "Checks in progress..."

# Check dependencies
if ! command -v git &> /dev/null; then
    echo "❌ Error: 'git' is not installed. Please install git and run the script again."
    exit 1
fi

if ! command -v go &> /dev/null; then
    echo "❌ Error: 'go' is not installed. Littleclaw requires Go 1.21+ to build."
    echo "Please visit https://go.dev/doc/install to install Go."
    exit 1
fi

echo "✅ Dependencies found."

# Create a temporary directory
TMP_DIR=$(mktemp -d)
trap "rm -rf '$TMP_DIR'" EXIT

echo "📥 Cloning Littleclaw repository..."
git clone -q https://github.com/hereisswapnil/littleclaw.git "$TMP_DIR/littleclaw"

cd "$TMP_DIR/littleclaw"

echo "⚙️  Building Littleclaw binary..."
go build -o littleclaw ./cmd/littleclaw/...

INSTALL_DIR="$HOME/.local/bin"
echo "📂 Installing Littleclaw to $INSTALL_DIR..."

mkdir -p "$INSTALL_DIR"
mv littleclaw "$INSTALL_DIR/"

# Make sure it's executable
chmod +x "$INSTALL_DIR/littleclaw"

# Detect shell and update PATH if necessary
SHELL_RC=""
if [ -n "$ZSH_VERSION" ] || [[ "$SHELL" == *"zsh"* ]]; then
    SHELL_RC="$HOME/.zshrc"
elif [ -n "$BASH_VERSION" ] || [[ "$SHELL" == *"bash"* ]]; then
    SHELL_RC="$HOME/.bashrc"
    if [ "$(uname)" = "Darwin" ]; then
        # Mac OS default bash profile
        SHELL_RC="$HOME/.bash_profile"
    fi
fi

if [ -n "$SHELL_RC" ]; then
    if ! grep -q "$INSTALL_DIR" "$SHELL_RC"; then
        echo "export PATH=\"$INSTALL_DIR:\$PATH\"" >> "$SHELL_RC"
        echo "✅ Added $INSTALL_DIR to your PATH in $SHELL_RC"
        echo "⚠️  Please run 'source $SHELL_RC' or restart your terminal to use littleclaw."
    else
        echo "✅ $INSTALL_DIR is already in your $SHELL_RC"
    fi
else
    echo "⚠️  Could not detect shell configuration."
    echo "Please manually add $INSTALL_DIR to your PATH."
fi

echo ""
echo "✅ Littleclaw successfully installed!"
echo ""
echo "🦀 You can now run the setup wizard to configure the agent:"
echo "  littleclaw configure"
echo ""
echo "For more help, run:"
echo "  littleclaw help"
