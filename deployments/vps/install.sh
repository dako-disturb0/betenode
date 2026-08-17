#!/usr/bin/env bash
set -e

# ==============================================================================
# 🚀 BetterLyrics Node & Origin VPS Auto-Installer
# ==============================================================================

INSTALL_DIR="/opt/bete-node"
SERVICE_NAME="bete-node"

echo "=== [1/4] Preparing Installation Directory ==="
mkdir -p "$INSTALL_DIR"
cd "$INSTALL_DIR"

echo "=== [2/4] Building / Copying Bete-Node Binary ==="
if command -v go >/dev/null 2>&1; then
    echo "Found Go compiler. Building from source..."
    go build -ldflags="-s -w" -o "$INSTALL_DIR/bete-node" github.com/betterlyrics/bete-node/cmd/app
else
    echo "Go compiler not found. Please ensure binary is placed at $INSTALL_DIR/bete-node"
fi

chmod +x "$INSTALL_DIR/bete-node"

if [ ! -f "$INSTALL_DIR/.env" ]; then
    echo "Creating default .env file..."
    cat <<EOF > "$INSTALL_DIR/.env"
MODE=auto
PORT=8080
UPSTREAM_URL=https://lyrics.api.dacubeking.com
CACHE_TTL_SECONDS=259200
# NODE1=https://node1.koyeb.app/interconnect
EOF
fi

echo "=== [3/4] Registering Systemd Service ==="
cat <<EOF > /etc/systemd/system/${SERVICE_NAME}.service
[Unit]
Description=BetterLyrics Accelerator Node
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=${INSTALL_DIR}
ExecStart=${INSTALL_DIR}/bete-node
Restart=always
RestartSec=3
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable ${SERVICE_NAME}
systemctl restart ${SERVICE_NAME}

echo "=== [4/4] Installation Complete! ==="
echo "Status: systemctl status ${SERVICE_NAME}"
echo "Logs: journalctl -u ${SERVICE_NAME} -f"
