#!/usr/bin/env bash
set -euo pipefail

# macOS launch agent installation paths and service label.
SERVICE_LABEL=com.bigbrother.agent
PLIST_PATH=~/Library/LaunchAgents/$SERVICE_LABEL.plist
AGENT_BIN=agent
CONFIG_PATH=~/Library/Application\ Support/BigBrother/config.json
INSTALL_DIR=~/bin

# Install agent binary and ensure config folder exists.
mkdir -p "$INSTALL_DIR"
mkdir -p "$(dirname "$CONFIG_PATH")"
cp "$AGENT_BIN" "$INSTALL_DIR/$AGENT_BIN"

# Generate launchd plist that runs agent at login and keeps it alive.
cat > "$PLIST_PATH" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>$SERVICE_LABEL</string>
    <key>ProgramArguments</key>
    <array>
        <string>$INSTALL_DIR/$AGENT_BIN</string>
        <string>--config</string>
        <string>$CONFIG_PATH</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
</dict>
</plist>
EOF

# Reload service to apply any updates.
launchctl unload "$PLIST_PATH" 2>/dev/null || true
launchctl load "$PLIST_PATH"
echo "Installed launchd agent at $PLIST_PATH"
