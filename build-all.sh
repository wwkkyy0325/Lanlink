#!/bin/bash
# Lanlink - Multi-Platform Build (for macOS/Linux hosts)
set -e
cd "$(dirname "$0")"

echo "========================================"
echo "  Lanlink - Multi-Platform Build"
echo "========================================"
echo ""

SUCCESS=0
FAILED=0

# ---- Detect host OS ----
HOST_OS="$(uname -s)"
echo "Host OS: $HOST_OS"
echo ""

# ---- Windows amd64 (cross-compile, CGO disabled) ----
echo "[1/4] Building Windows amd64..."
CGO_ENABLED=0 wails build -platform windows/amd64 2>&1 && {
    echo "  ✓ Windows amd64 built"
    SUCCESS=$((SUCCESS+1))
} || {
    echo "  ✗ Windows amd64 FAILED (cross-compile)"
    FAILED=$((FAILED+1))
}
echo ""

# ---- Linux amd64 ----
echo "[2/4] Building Linux amd64..."
CGO_ENABLED=0 wails build -platform linux/amd64 2>&1 && {
    echo "  ✓ Linux amd64 built"
    SUCCESS=$((SUCCESS+1))
} || {
    echo "  ✗ Linux amd64 FAILED"
    FAILED=$((FAILED+1))
}
echo ""

# ---- macOS amd64 (Intel) ----
echo "[3/4] Building macOS amd64 (Intel)..."
if [ "$HOST_OS" = "Darwin" ]; then
    wails build -platform darwin/amd64 2>&1 && {
        echo "  ✓ macOS amd64 built"
        SUCCESS=$((SUCCESS+1))
    } || {
        echo "  ✗ macOS amd64 FAILED"
        FAILED=$((FAILED+1))
    }
else
    CGO_ENABLED=0 wails build -platform darwin/amd64 2>&1 && {
        echo "  ✓ macOS amd64 built"
        SUCCESS=$((SUCCESS+1))
    } || {
        echo "  ✗ macOS amd64 FAILED (needs macOS host)"
        FAILED=$((FAILED+1))
    }
fi
echo ""

# ---- macOS arm64 (Apple Silicon) ----
echo "[4/4] Building macOS arm64 (Apple Silicon)..."
if [ "$HOST_OS" = "Darwin" ]; then
    wails build -platform darwin/arm64 2>&1 && {
        echo "  ✓ macOS arm64 built"
        SUCCESS=$((SUCCESS+1))
    } || {
        echo "  ✗ macOS arm64 FAILED"
        FAILED=$((FAILED+1))
    }
else
    CGO_ENABLED=0 wails build -platform darwin/arm64 2>&1 && {
        echo "  ✓ macOS arm64 built"
        SUCCESS=$((SUCCESS+1))
    } || {
        echo "  ✗ macOS arm64 FAILED (needs macOS host)"
        FAILED=$((FAILED+1))
    }
fi
echo ""

# ---- Summary ----
echo "========================================"
echo "  Build Summary"
echo "========================================"
echo "  Succeeded: $SUCCESS/4"
echo "  Failed:    $FAILED/4"
echo ""
echo "  Output directory: build/bin/"
echo ""
if [ "$FAILED" -gt 0 ]; then
    echo "NOTE: macOS builds should be done ON a macOS machine."
    echo "      Linux builds from macOS work with CGO_ENABLED=0."
    echo "      Windows builds cross-compile with CGO_ENABLED=0."
fi
echo "========================================"
