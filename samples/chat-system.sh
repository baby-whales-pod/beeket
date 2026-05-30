#!/usr/bin/env bash
# chat-system.sh — send a system message + a user message to the chat completion
# endpoint and display the response with jq.
#
# Usage:
#   ./chat-system.sh
#   MODEL=mistral ./chat-system.sh
#   BEEKET_HOST=192.168.1.10 BEEKET_PORT=11435 ./chat-system.sh
set -euo pipefail

BEEKET_HOST="${BEEKET_HOST:-127.0.0.1}"
BEEKET_PORT="${BEEKET_PORT:-11435}"
MODEL="${MODEL:-qwen3.5-2b:q4_k_m}"

BODY=$(jq -n --arg model "$MODEL" \
  --arg system "You are a helpful assistant that speaks like a pirate." \
  --arg content "Why is the sky blue?" \
  '{"model":$model,"stream":false,"messages":[{"role":"system","content":$system},{"role":"user","content":$content}]}')

curl -sS -X POST "http://${BEEKET_HOST}:${BEEKET_PORT}/api/chat" \
  -H "Content-Type: application/json" \
  -d "$BODY" | jq '.'
