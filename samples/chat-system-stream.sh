#!/usr/bin/env bash
# chat-system-stream.sh — send a system message + a user message with streaming
# enabled. Each NDJSON chunk is printed as it arrives; jq formats each line.
#
# Usage:
#   ./chat-system-stream.sh
#   MODEL=mistral ./chat-system-stream.sh
#   BEEKET_HOST=192.168.1.10 BEEKET_PORT=11435 ./chat-system-stream.sh

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
        "role": "system",
        "content": "You are a helpful assistant that speaks like a pirate."
      },
      {
        "role": "user",
        "content": "Why is the sky blue?"
      }
    ]
  }' | while IFS= read -r line; do
    echo "${line}" | jq -c '.'
  done
