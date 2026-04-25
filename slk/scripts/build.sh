#!/bin/bash
set -e

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BUILD="$ROOT/build/linux"
mkdir -p "$BUILD"

echo "═══════════════════════════════════════"
echo "  SLK BUILD SYSTEM"
echo "═══════════════════════════════════════"

# ── 1. BUILD VDF LIBRARY ──
echo "🔨 Building libslkvdf.a..."
cd "$ROOT/race/math"
gcc -O2 -c vdf.c -o vdf.o
ar rcs "$BUILD/libslkvdf.a" vdf.o
echo "✅ libslkvdf.a done"

# ── 2. BUILD RACE ENGINE LIBRARY ──
echo "🔨 Building libslkrace.a..."
cd "$ROOT/race/engine"
g++ -O2 -c race_engine.cpp -o race_engine.o -lpthread
cd "$ROOT/race/sensors"
gcc -O2 -c cpu_temp_wrapper.c -o cpu_temp_wrapper.o
ar rcs "$BUILD/libslkrace.a" \
    "$ROOT/race/engine/race_engine.o" \
    "$ROOT/race/sensors/cpu_temp_wrapper.o"
echo "✅ libslkrace.a done"

# ── 3. BUILD slkd ──
echo "🔨 Building slkd..."
cd "$ROOT"
go build -o "$BUILD/slkd" ./cmd/slkd/
echo "✅ slkd done"

# ── 4. BUILD slkbank ──
echo "🔨 Building slkbank..."
cd "$ROOT"
go build -o "$BUILD/slkbank" ./cmd/slkbank/
echo "✅ slkbank done"

# ── 5. BUILD slkgui ──
echo "🔨 Building slkgui..."
cd "$ROOT"
go build -o "$BUILD/slkgui" ./cmd/slkgui/
echo "✅ slkgui done"

echo ""
echo "═══════════════════════════════════════"
echo "  ✅ BUILD COMPLETE"
ls -lh "$BUILD/"
echo "═══════════════════════════════════════"
