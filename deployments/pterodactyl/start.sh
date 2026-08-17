#!/bin/bash
# ==============================================================================
# 🎮 BetterLyrics Bete-Node Startup Script for Pterodactyl Panel
# ==============================================================================

# Ensure working directory is /home/container
cd /home/container || exit 1

# Check if binary exists, otherwise build it or download
if [ ! -f "./bete-node" ]; then
    if [ -f "./bete-node/cmd/app/main.go" ]; then
        echo "🔨 Building bete-node binary..."
        cd bete-node && go build -ldflags="-s -w" -o ../bete-node ./cmd/app && cd ..
    elif [ -f "./cmd/app/main.go" ]; then
        echo "🔨 Building bete-node binary..."
        go build -ldflags="-s -w" -o ./bete-node ./cmd/app
    else
        echo "⚠️ bete-node binary not found! Please compile or place the binary in /home/container"
    fi
fi

# Set executable permission
chmod +x ./bete-node 2>/dev/null || true

echo "🚀 Starting BetterLyrics Multi-Node Accelerator..."
exec ./bete-node
