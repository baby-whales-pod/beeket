#!/usr/bin/env bash
# chat-simple.sh — send a single user message to the chat completion endpoint
# and display the response with jq.
#
# Usage:
#   ./chat-simple.sh
#   MODEL=mistral ./chat-simple.sh
#   BEEKET_HOST=192.168.1.10 BEEKET_PORT=11435 ./chat-simple.sh

BEEKET_HOST="${BEEKET_HOST:-localhost}"
BEEKET_PORT="${BEEKET_PORT:-11435}"
MODEL="${MODEL:-llama3.2}"

curl -s "http://${BEEKET_HOST}:${BEEKET_PORT}/api/chat" \
  -X POST \
  -H "Content-Type: application/json" \
  -d '{
    "model": "'"${MODEL}"'",
    "stream": false,
    "messages": [
      {
        "role": "user",
        "content": "Why is the sky blue?"
      }
    ]
  }' | jq '.'
