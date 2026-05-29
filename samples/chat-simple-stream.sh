#!/usr/bin/env bash
# chat-simple-stream.sh — send a single user message with streaming enabled.
# Each NDJSON chunk is printed as it arrives; jq formats each line individually.
#
# Usage:
#   ./chat-simple-stream.sh
#   MODEL=mistral ./chat-simple-stream.sh
#   BEEKET_HOST=192.168.1.10 BEEKET_PORT=11435 ./chat-simple-stream.sh
set -euo pipefail

BEEKET_HOST="${BEEKET_HOST:-127.0.0.1}"
BEEKET_PORT="${BEEKET_PORT:-11435}"
MODEL="${MODEL:-qwen3.5-0.8b:q4_k_m}"

BODY=$(jq -n --arg model "$MODEL" --arg content "Why is the sky blue?" \
  '{"model":$model,"stream":true,"messages":[{"role":"user","content":$content}]}')

curl -sS --no-buffer -X POST "http://${BEEKET_HOST}:${BEEKET_PORT}/api/chat" \
  -H "Content-Type: application/json" \
  -d "$BODY" | while IFS= read -r line; do
    [[ -z "$line" ]] && continue
    echo "${line}" | jq -c '.'
  done
