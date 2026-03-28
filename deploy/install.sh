#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_DIR="$(dirname "$SCRIPT_DIR")"
SERVICE_NAME="print-agent"
CONFIG_DIR="${HOME}/.config/${SERVICE_NAME}"
SYSTEMD_DIR="${HOME}/.config/systemd/user"

echo "=== print-agent installer ==="
echo

# 1. Build the binary
echo "[1/5] Building print-agent..."
cd "$REPO_DIR"
go build -o print-agent ./cmd/print-agent
echo "  Built: ${REPO_DIR}/print-agent"

# 2. Create config directory and env file if missing
echo "[2/5] Setting up config directory..."
mkdir -p "$CONFIG_DIR"
if [ ! -f "${CONFIG_DIR}/env" ]; then
    cp "${REPO_DIR}/.env.example" "${CONFIG_DIR}/env"
    echo "  Created: ${CONFIG_DIR}/env (from .env.example)"
    echo "  >>> EDIT THIS FILE with your API credentials before starting the service <<<"
else
    echo "  Config already exists: ${CONFIG_DIR}/env (not overwritten)"
fi

# 3. Install systemd unit
echo "[3/5] Installing systemd service..."
mkdir -p "$SYSTEMD_DIR"
cp "${SCRIPT_DIR}/${SERVICE_NAME}.service" "${SYSTEMD_DIR}/${SERVICE_NAME}.service"
systemctl --user daemon-reload
echo "  Installed: ${SYSTEMD_DIR}/${SERVICE_NAME}.service"

# 4. Enable lingering (start service at boot, even without login)
echo "[4/5] Enabling lingering for user $(whoami)..."
loginctl enable-linger "$(whoami)"
echo "  Linger enabled"

# 5. Enable (but don't start yet — user needs to configure env first)
echo "[5/5] Enabling service..."
systemctl --user enable "$SERVICE_NAME"
echo "  Service enabled (will start at next boot)"

echo
echo "=== Installation complete ==="
echo
echo "Next steps:"
echo "  1. Edit your config:    nano ${CONFIG_DIR}/env"
echo "  2. Start the service:   systemctl --user start ${SERVICE_NAME}"
echo "  3. Check status:        systemctl --user status ${SERVICE_NAME}"
echo "  4. View logs:           journalctl --user -u ${SERVICE_NAME} -f"
