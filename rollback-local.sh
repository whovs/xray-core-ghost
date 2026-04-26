#!/usr/bin/env bash

# Colors for output
red='\033[0;31m'
green='\033[0;32m'
yellow='\033[0;33m'
blue='\033[0;34m'
plain='\033[0m'

# Xray installation directory (3x-UI location)
XRAY_BIN_DIR="/usr/local/x-ui/bin"
SYS_ARCH=$(uname -m)
SYS_OS=$(uname -s | tr '[:upper:]' '[:lower:]')
# Normalize arch to match Go convention
case "$SYS_ARCH" in
  aarch64) SYS_ARCH="arm64" ;;
  x86_64) SYS_ARCH="amd64" ;;
esac
XRAY_BIN_NAME="xray-${SYS_OS}-${SYS_ARCH}"
XRAY_BIN_PATH="${XRAY_BIN_DIR}/${XRAY_BIN_NAME}"
XRAY_BACKUP_PATH="${XRAY_BIN_DIR}/${XRAY_BIN_NAME}.backup"

# Check if running as root
if [[ $EUID -ne 0 ]]; then
    echo -e "${red}❌ This script must be run as root${plain}"
    exit 1
fi

# Check if backup exists
if [[ ! -f "$XRAY_BACKUP_PATH" ]]; then
    echo -e "${red}❌ No backup found at: $XRAY_BACKUP_PATH${plain}"
    echo -e "${yellow}⚠️  Run deployment first to create a backup${plain}"
    exit 1
fi

echo -e "${blue}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${plain}"
echo -e "${yellow}⚠️  Xray Rollback - Restoring from Backup${plain}"
echo -e "${blue}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${plain}"

# Get the current and backup versions for comparison
echo -e "\n${green}Current versions:${plain}"
if [[ -f "$XRAY_BIN_PATH" ]]; then
    CURRENT_VERSION=$($XRAY_BIN_PATH -version 2>&1 | head -1)
    echo -e "   Working: ${yellow}$CURRENT_VERSION${plain}"
fi

BACKUP_VERSION=$($XRAY_BACKUP_PATH -version 2>&1 | head -1)
echo -e "   Backup:  ${yellow}$BACKUP_VERSION${plain}"

# Confirm action
echo -e "\n${red}⚠️  This will restore the backup binary${plain}"
read -p "Do you want to continue? (yes/no): " -r CONFIRM

if [[ ! "$CONFIRM" =~ ^[Yy][Ee][Ss]$ ]]; then
    echo -e "${yellow}Rollback cancelled${plain}"
    exit 0
fi

# Step 1: Restore backup
echo -e "\n${yellow}[1/2]${plain} Restoring backup binary..."
cp "$XRAY_BACKUP_PATH" "$XRAY_BIN_PATH"
if [[ $? -ne 0 ]]; then
    echo -e "${red}❌ Failed to restore backup${plain}"
    exit 1
fi

chmod +x "$XRAY_BIN_PATH"
echo -e "${green}✅ Backup restored to working binary${plain}"

# Step 2: Verify
echo -e "\n${yellow}[2/2]${plain} Verifying binary..."
RESTORED_VERSION=$($XRAY_BIN_PATH -version 2>&1 | head -1)
if [[ -n "$RESTORED_VERSION" ]]; then
    echo -e "${green}✅ Binary is valid${plain}"
    echo -e "${green}   Version: $RESTORED_VERSION${plain}"
else
    echo -e "${red}❌ Xray did not return version output${plain}"
    exit 1
fi

echo -e "\n${blue}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${plain}"
echo -e "${green}✅ Rollback completed successfully!${plain}"
echo -e "${blue}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${plain}"

exit 0
