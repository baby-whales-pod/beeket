#!/usr/bin/env bash
# chat-simple-stream.sh — send a single user message with streaming enabled.
# Each NDJSON chunk is printed as it arrives; jq formats each line individually.
#
# Usage:
#   ./chat-simple-stream.sh
#   MODEL=mistral ./chat-simple-stream.sh
#   BEEKET_HOST=192.168.1.10 BEEKET_PORT=11435 ./chat-simple-stream.sh

BEEKET_HOST="${BEEKET_HOST:-localhost}"
BEEKET_PORT="${BEEKET_PORT:-11435}"
MODEL="${MODEL:-llama3.2}"

curl -s --no-buffer "http://${BEEKET_HOST}:${BEEKET_PORT}/api/chat" \
  -X POST \
  -H "Content-Type: application/json" \
  -d '{
    "model": "'"${MODEL}"'",
    "stream": true,
    "messages": [
      {
        "role": "user",
        "content": "Why is the sky blue?"
      }
    ]
  }' | while IFS= read -r line; do
    echo "${line}" | jq -c '.'
  done
