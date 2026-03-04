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

# Add to PATH temporarily for the script, suggest user to add it permanently if missing.
export PATH="$INSTALL_DIR:$PATH"

echo ""
echo "✅ Littleclaw successfully installed!"
echo ""

if ! command -v littleclaw &> /dev/null; then
    echo "⚠️  Note: '$INSTALL_DIR' is not in your PATH."
    echo "Please add the following line to your ~/.bashrc or ~/.zshrc:"
    echo "  export PATH=\"\$HOME/.local/bin:\$PATH\""
    echo ""
    echo "Then restart your terminal."
else
    echo "🦀 You can now run the setup wizard to configure the agent:"
    echo "  littleclaw configure"
fi

echo ""
echo "For more help, run:"
echo "  littleclaw help"
