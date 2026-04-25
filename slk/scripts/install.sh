#!/bin/bash
set -e

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BUILD="$ROOT/build/linux"

echo "═══════════════════════════════════════"
echo "  SLK INSTALL"
echo "═══════════════════════════════════════"

sudo cp "$BUILD/slkd" /usr/local/bin/slkd
sudo cp "$BUILD/slkbank" /usr/local/bin/slkbank
sudo cp "$BUILD/slkgui" /usr/local/bin/slkgui
sudo chmod +x /usr/local/bin/slkd /usr/local/bin/slkbank /usr/local/bin/slkgui

echo "✅ Installed!"
echo "  Run node:  slkd"
echo "  Run bank:  slkbank"
echo "  Run GUI:   slkgui"
echo "═══════════════════════════════════════"
