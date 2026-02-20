#!/usr/bin/env bash
# Captain's Log — Installer
# Usage: curl -fsSL https://raw.githubusercontent.com/ryan-winkler/captainslog-whisper/main/install.sh | bash
set -euo pipefail

REPO="https://github.com/ryan-winkler/captainslog-whisper.git"
INSTALL_DIR="$HOME/.local/bin"
CONFIG_DIR="$HOME/.config/captainslog"
SERVICE_DIR="$HOME/.config/systemd/user"
CLONE_DIR="$HOME/code/captainslog-whisper"

echo ""
echo "🖖 Captain's Log Installer"
echo "=========================="
echo ""

# --- Check for Go ---
if ! command -v go &>/dev/null; then
    echo "❌ Go is not installed."
    echo ""
    echo "   Install Go first (pick one):"
    echo ""
    echo "   • Download from:  https://go.dev/dl/"
    echo "   • macOS:          brew install go"
    echo "   • Ubuntu/Debian:  sudo apt install golang-go"
    echo "   • Fedora:         sudo dnf install golang"
    echo "   • Arch:           sudo pacman -S go"
    echo ""
    echo "   Then run this installer again."
    exit 1
fi

echo "✅ Go $(go version | awk '{print $3}') found"

# --- Clone or update ---
if [ -d "$CLONE_DIR/.git" ]; then
    echo "📦 Updating existing installation..."
    cd "$CLONE_DIR"
    git pull --ff-only 2>/dev/null || { echo "⚠️  Could not pull updates (local changes?). Continuing with existing code."; }
else
    echo "📦 Downloading Captain's Log..."
    mkdir -p "$(dirname "$CLONE_DIR")"
    git clone "$REPO" "$CLONE_DIR"
    cd "$CLONE_DIR"
fi

# --- Build ---
echo "🔨 Building..."
go build -o captainslog ./cmd/captainslog 2>&1

# --- Install ---
mkdir -p "$INSTALL_DIR"
cp captainslog "$INSTALL_DIR/captainslog"

# Install CLI if it exists
if [ -f captainslog-cli ]; then
    cp captainslog-cli "$INSTALL_DIR/captainslog-cli"
    chmod +x "$INSTALL_DIR/captainslog-cli"
fi

# --- Create config directory ---
mkdir -p "$CONFIG_DIR"
mkdir -p "$CONFIG_DIR/recordings"

# --- Check PATH ---
if [[ ":$PATH:" != *":$INSTALL_DIR:"* ]]; then
    echo ""
    echo "⚠️  $INSTALL_DIR is not in your PATH."
    echo "   Add this to your ~/.bashrc or ~/.zshrc:"
    echo ""
    echo "   export PATH=\"\$HOME/.local/bin:\$PATH\""
    echo ""
fi

# --- Optional: systemd service ---
echo ""
read -rp "🚀 Start Captain's Log on boot? (y/N) " -n 1 REPLY
echo ""
if [[ "$REPLY" =~ ^[Yy]$ ]]; then
    mkdir -p "$SERVICE_DIR"
    if [ -f examples/captainslog.service ]; then
        cp examples/captainslog.service "$SERVICE_DIR/captainslog.service"
    else
        cat > "$SERVICE_DIR/captainslog.service" <<EOF
[Unit]
Description=Captain's Log — Speech-to-Text
After=network.target

[Service]
ExecStart=%h/.local/bin/captainslog
Restart=on-failure
RestartSec=5

[Install]
WantedBy=default.target
EOF
    fi
    systemctl --user daemon-reload
    systemctl --user enable --now captainslog
    echo "✅ Service installed and running!"
else
    echo "   (You can start manually with: captainslog)"
fi

echo ""
echo "════════════════════════════════════════════"
echo "  🖖 Captain's Log installed successfully!"
echo "════════════════════════════════════════════"
echo ""
echo "  Open in your browser:  http://localhost:8090"
echo ""
echo "  ⚠️  You also need a Whisper backend running."
echo "     Easiest way (needs Docker):"
echo ""
echo "     docker run -d -p 5000:5000 ghcr.io/heimoshuiyu/whisper-fastapi:latest"
echo ""
echo "     No GPU? That's fine — it works on CPU too (just slower)."
echo "     Have a GPU? Add --gpus all for fast transcription."
echo ""
echo "  📖 Full docs: https://github.com/ryan-winkler/captainslog-whisper"
echo ""
