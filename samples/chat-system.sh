#!/usr/bin/env bash
# chat-system.sh — send a system message + a user message to the chat completion
# endpoint and display the response with jq.
#
# Usage:
#   ./chat-system.sh
#   MODEL=mistral ./chat-system.sh
#   BEEKET_HOST=192.168.1.10 BEEKET_PORT=11435 ./chat-system.sh

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
        "role": "system",
        "content": "You are a helpful assistant that speaks like a pirate."
      },
      {
        "role": "user",
        "content": "Why is the sky blue?"
      }
    ]
  }' | jq '.'
