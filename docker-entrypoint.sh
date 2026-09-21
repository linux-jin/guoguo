#!/bin/sh
set -eu

PORT="${PORT:-8998}"
DATA_DIR="${JUKU_DATA_DIR:-/data}"
OUT_DIR="${JUKU_OUTPUT_DIR:-/downloads}"
FFMPEG="${JUKU_FFMPEG:-/usr/bin/ffmpeg}"
CONCURRENCY="${JUKU_CONCURRENCY:-1}"

mkdir -p "$DATA_DIR" "$OUT_DIR"

exec /usr/local/bin/juku \
  -open=false \
  -listen "0.0.0.0:${PORT}" \
  -data-dir "$DATA_DIR" \
  -out "$OUT_DIR" \
  -ffmpeg "$FFMPEG" \
  -c "$CONCURRENCY" \
  "$@"
