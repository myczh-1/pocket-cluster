#!/bin/bash
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
BINARY="$PROJECT_DIR/dist/agent-$(uname -s | tr A-Z a-z)-$(uname -m)"
DATA_DIR="${PC_DATA_DIR:-$HOME/.pocketcluster}"
PORT="${PC_PORT:-7788}"

case "$(uname -s)" in
    Darwin)
        PLIST="$HOME/Library/LaunchAgents/com.pocketcluster.agent.plist"
        cat > "$PLIST" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.pocketcluster.agent</string>
    <key>ProgramArguments</key>
    <array>
        <string>${BINARY}</string>
        <string>-data</string>
        <string>${DATA_DIR}</string>
        <string>-port</string>
        <string>${PORT}</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>${DATA_DIR}/agent.log</string>
    <key>StandardErrorPath</key>
    <string>${DATA_DIR}/agent.err</string>
</dict>
</plist>
EOF
        launchctl unload "$PLIST" 2>/dev/null || true
        launchctl load "$PLIST"
        echo "Installed and started via launchd: $PLIST"
        ;;
    Linux)
        SERVICE="$HOME/.config/systemd/user/pocketcluster-agent.service"
        mkdir -p "$(dirname "$SERVICE")"
        cat > "$SERVICE" <<EOF
[Unit]
Description=PocketCluster Agent
After=network-online.target

[Service]
Type=simple
ExecStart=${BINARY} -data ${DATA_DIR} -port ${PORT}
Restart=always
RestartSec=5

[Install]
WantedBy=default.target
EOF
        systemctl --user daemon-reload
        systemctl --user enable pocketcluster-agent
        systemctl --user start pocketcluster-agent
        echo "Installed and started via systemd: $SERVICE"
        ;;
    *)
        echo "Unsupported platform. Install manually."
        exit 1
        ;;
esac
