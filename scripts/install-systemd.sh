#!/usr/bin/env bash
set -euo pipefail

# Linux service/unit settings for installing agent as systemd service.
SERVICE_NAME=big-brother-agent.service
INSTALL_DIR=/usr/local/bin
AGENT_BIN=agent
CONFIG_PATH=/etc/big-brother/config.json
UNIT_PATH=/etc/systemd/system/$SERVICE_NAME

# Guard: require built agent binary in current folder.
if [ ! -f "$AGENT_BIN" ]; then
  echo "Agent binary not found in current directory. Build it first." >&2
  exit 1
fi

# Install binary and write unit file with restart behavior.
sudo mkdir -p $(dirname "$CONFIG_PATH")
sudo cp "$AGENT_BIN" "$INSTALL_DIR/$AGENT_BIN"

sudo tee "$UNIT_PATH" > /dev/null <<EOF
[Unit]
Description=Big Brother Agent
After=network.target

[Service]
Type=simple
ExecStart=$INSTALL_DIR/$AGENT_BIN --config $CONFIG_PATH
Restart=always
RestartSec=5
User=root

[Install]
WantedBy=multi-user.target
EOF

# Reload systemd and start service immediately.
sudo systemctl daemon-reload
sudo systemctl enable "$SERVICE_NAME"
sudo systemctl start "$SERVICE_NAME"

echo "Installed $SERVICE_NAME and started agent."
