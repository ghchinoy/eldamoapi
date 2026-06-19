#!/bin/sh
# install.sh - Install the latest Eldamo MCP Server binary

set -e

# Configuration
REPO="username/eldamo-server"
INSTALL_DIR="/usr/local/bin"

# Get latest version
VERSION=$(curl -s "https://api.github.com/repos/$REPO/releases/latest" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')

echo "Installing $VERSION..."

# Determine OS/Arch
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m | sed 's/x86_64/amd64/' | sed 's/aarch64/arm64/')

# Download and extract
URL="https://github.com/$REPO/releases/download/$VERSION/eldamoapi_${OS}_${ARCH}.tar.gz"
curl -L "$URL" -o /tmp/eldamoapi.tar.gz
tar -xzf /tmp/eldamoapi.tar.gz -C "$INSTALL_DIR" eldamoapi

echo "Successfully installed to $INSTALL_DIR/eldamoapi"
