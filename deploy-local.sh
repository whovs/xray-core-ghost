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
# Automatic Path Detection
PROJECT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
XRAY_BIN_NAME="xray-${SYS_OS}-${SYS_ARCH}"
XRAY_BIN_PATH="${XRAY_BIN_DIR}/${XRAY_BIN_NAME}"
XRAY_BACKUP_PATH="${XRAY_BIN_DIR}/${XRAY_BIN_NAME}.backup"
XRAY_BUILD_PATH="${PROJECT_DIR}/xray-${SYS_OS}-${SYS_ARCH}"

# Check if running as root
if [[ $EUID -ne 0 ]]; then
    echo -e "${red}❌ This script must be run as root${plain}"
    exit 1
fi

echo -e "${blue}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${plain}"
echo -e "${green}🚀 Starting Xray Build & Deployment${plain}"
echo -e "${blue}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${plain}"

# Step 1: Build binaries for multiple architectures
if [[ "$1" == "--deploy-to" ]]; then
    # Skip build if we are only deploying
    echo -e "${blue}⏩ Skipping build phase, proceeding to deployment...${plain}"
else
    echo -e "\n${yellow}[1/4]${plain} Building Xray binaries for multiple architectures..."
    export PATH=$PATH:/usr/local/go/bin
    if command -v go >/dev/null 2>&1; then
        GO_CMD="go"
    elif [[ -x "/usr/local/go/bin/go" ]]; then
        GO_CMD="/usr/local/go/bin/go"
    else
        echo -e "${red}❌ Go compiler not found. Please install Go.${plain}"
        exit 1
    fi

    echo -e "${blue}   → Using Go: $(${GO_CMD} version)${plain}"
    cd "$PROJECT_DIR" || exit 1

    # List of architectures to build
    ARCHS=("amd64" "arm64" "386")
    OS_TARGET="linux"

    for ARCH in "${ARCHS[@]}"; do
        OUTPUT_PATH="${PROJECT_DIR}/xray-${OS_TARGET}-${ARCH}"
        echo -e "${blue}   → Building for ${OS_TARGET}/${ARCH}...${plain}"

        GOOS=$OS_TARGET GOARCH=$ARCH CGO_ENABLED=0 ${GO_CMD} build \
          -o "$OUTPUT_PATH" \
          -trimpath \
          -buildvcs=false \
          -ldflags="-s -w -buildid=" \
          ./main

        if [[ $? -ne 0 ]]; then
            echo -e "${red}❌ Build failed for ${ARCH}${plain}"
            exit 1
        fi
        echo -e "${green}   ✅ Build successful: $OUTPUT_PATH${plain}"
    done
fi

if [[ "$1" == "--build-only" ]]; then
    echo -e "${green}✅ Build only phase complete.${plain}"
    exit 0
fi

# Step 2: Determine Deployment Target
TARGET_BIN_PATH=""
if [[ "$1" == "--deploy-to" ]]; then
    TARGET_BIN_PATH="$2"
else
    # Default legacy path
    TARGET_BIN_PATH="${XRAY_BIN_DIR}/${XRAY_BIN_NAME}"
fi

TARGET_DIR=$(dirname "$TARGET_BIN_PATH")
TARGET_NAME=$(basename "$TARGET_BIN_PATH")
ORIGINAL_BACKUP="${TARGET_BIN_PATH}.original_backup"

echo -e "\n${yellow}[2/4]${plain} Managing target and backup..."
echo -e "${blue}   → Target: $TARGET_BIN_PATH${plain}"

if [[ -f "$TARGET_BIN_PATH" ]]; then
    if [[ ! -f "$ORIGINAL_BACKUP" ]]; then
        echo -e "${blue}   → Creating ONE-TIME original backup...${plain}"
        cp "$TARGET_BIN_PATH" "$ORIGINAL_BACKUP"
        echo -e "${green}   ✅ Original backup created: $ORIGINAL_BACKUP${plain}"
    else
        echo -e "${blue}   → Original backup already exists. Skipping.${plain}"
    fi
    # Remove existing to avoid "Text file busy"
    rm -f "$TARGET_BIN_PATH"
else
    echo -e "${yellow}⚠️  No existing binary found at: $TARGET_BIN_PATH. Creating new.${plain}"
fi

# Step 3: Deploy new binary
echo -e "\n${yellow}[3/4]${plain} Deploying and renaming to '$TARGET_NAME'..."

cp "$XRAY_BUILD_PATH" "$TARGET_BIN_PATH"
if [[ $? -ne 0 ]]; then
    echo -e "${red}❌ Failed to copy binary${plain}"
    exit 1
fi

chmod +x "$TARGET_BIN_PATH"
echo -e "${green}✅ Binary deployed: $TARGET_BIN_PATH${plain}"

# Step 4: Verify and Restart
echo -e "\n${yellow}[4/4]${plain} Verifying deployment..."
XRAY_VERSION=$($TARGET_BIN_PATH -version 2>&1 | head -1)
if [[ -n "$XRAY_VERSION" ]]; then
    echo -e "${green}✅ Binary is valid: $XRAY_VERSION${plain}"
else
    echo -e "${red}❌ Xray did not return version output${plain}"
    exit 1
fi

# Detect and Restart service
SVC_NAME=$(systemctl list-units --type=service --all | grep -E "xray|x-ui" | grep -v "api" | grep -v "listener" | head -n1 | awk '{print $1}')
if [[ -n "$SVC_NAME" ]]; then
    echo -e "${blue}🔄 Restarting service $SVC_NAME...${plain}"
    systemctl restart "$SVC_NAME"
fi

echo -e "\n${blue}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${plain}"
echo -e "${green}✅ Deployment completed successfully!${plain}"
echo -e "${blue}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${plain}"

exit 0
