#!/usr/bin/env bash
# chat-simple.sh — send a single user message to the chat completion endpoint
# and display the response with jq.
#
# Usage:
#   ./chat-simple.sh
#   MODEL=mistral ./chat-simple.sh
#   BEEKET_HOST=192.168.1.10 BEEKET_PORT=11435 ./chat-simple.sh
set -euo pipefail

BEEKET_HOST="${BEEKET_HOST:-127.0.0.1}"
BEEKET_PORT="${BEEKET_PORT:-11435}"
MODEL="${MODEL:-smollm2:135m}"

BODY=$(jq -n --arg model "$MODEL" --arg content "Why is the sky blue?" \
  '{"model":$model,"stream":false,"messages":[{"role":"user","content":$content}]}')

curl -sS -X POST "http://${BEEKET_HOST}:${BEEKET_PORT}/api/chat" \
  -H "Content-Type: application/json" \
  -d "$BODY" | jq '.'
