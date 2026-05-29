#!/usr/bin/env bash
# chat-structured.sh — send a chat request with a JSON Schema format constraint
# and display the parsed JSON response.
#
# The model is constrained to return a JSON object with "name" (string) and
# "age" (integer) fields. The output is guaranteed to match the schema.
#
# Usage:
#   ./chat-structured.sh
#   MODEL=smollm2:135m ./chat-structured.sh
#   BEEKET_HOST=192.168.1.10 BEEKET_PORT=11435 ./chat-structured.sh
set -euo pipefail

BEEKET_HOST="${BEEKET_HOST:-127.0.0.1}"
BEEKET_PORT="${BEEKET_PORT:-11435}"
MODEL="${MODEL:-smollm2:135m}"

BODY=$(jq -n \
  --arg model   "$MODEL" \
  --arg content "Extract the person's name and age: John Smith is 42 years old." \
  '{
    "model":  $model,
    "stream": false,
    "format": {
      "type": "object",
      "properties": {
        "name": {"type": "string"},
        "age":  {"type": "integer"}
      },
      "required": ["name", "age"]
    },
    "messages": [
      {"role": "user", "content": $content}
    ]
  }')

echo "Request body:"
echo "$BODY" | jq '.'
echo ""
echo "Response (raw):"
RESPONSE=$(curl -sS -X POST "http://${BEEKET_HOST}:${BEEKET_PORT}/api/chat" \
  -H "Content-Type: application/json" \
  -d "$BODY")

echo "$RESPONSE" | jq '.'
echo ""
echo "Extracted structured data:"
echo "$RESPONSE" | jq '.message.content | fromjson'
