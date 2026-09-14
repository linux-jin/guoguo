#!/bin/sh
set -e
PORT="${PORT:-8999}"
DATA_DIR="${JUKU_DATA_DIR:-/data}"
if [ -n "$JUKU_OUTPUT_DIR" ]; then
  OUT_DIR="$JUKU_OUTPUT_DIR"
else
  OUT_DIR="$DATA_DIR/downloads"
fi
mkdir -p "$DATA_DIR" "$OUT_DIR"
exec juku -open=false -listen "0.0.0.0:${PORT}" -data-dir "$DATA_DIR" -out "$OUT_DIR" -ffmpeg "${JUKU_FFMPEG:-ffmpeg}" "$@"
