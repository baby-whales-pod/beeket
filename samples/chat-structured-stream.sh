#!/usr/bin/env bash
# chat-structured-stream.sh — send a streaming chat request with a JSON Schema
# format constraint and assemble the streamed tokens into a final JSON object.
#
# Each NDJSON line is printed as it arrives. The final assembled content is
# parsed with jq at the end.
#
# "think": false suppresses chain-of-thought output on Qwen3 and other thinking
# models, which is strongly recommended for structured output so the model
# generates JSON immediately without a <think>…</think> preamble.
#
# Usage:
#   ./samples/chat-structured-stream.sh
#   MODEL=smollm2:135m ./samples/chat-structured-stream.sh
#   BEEKET_HOST=192.168.1.10 BEEKET_PORT=11435 ./samples/chat-structured-stream.sh
set -euo pipefail

BEEKET_HOST="${BEEKET_HOST:-127.0.0.1}"
BEEKET_PORT="${BEEKET_PORT:-11435}"
MODEL="${MODEL:-qwen3.5-0.8b:q4_k_m}"

BODY=$(jq -n \
  --arg model   "$MODEL" \
  --arg content "Extract the capital city and its country: Paris is the capital of France." \
  --argjson format '{
    "type": "object",
    "properties": {
      "capital": {"type": "string"},
      "country": {"type": "string"}
    },
    "required": ["capital", "country"]
  }' \
  --argjson options '{
    "temperature": 0.1,
    "top_p": 0.9,
    "num_predict": 512
  }' \
  '{
    "model":   $model,
    "stream":  true,
    "think":   false,
    "format":  $format,
    "options": $options,
    "messages": [
      {"role": "user", "content": $content}
    ]
  }')

echo "Streaming response chunks:"
echo "---"

# Collect all chunks and print them as they arrive.
FULL_CONTENT=""
while IFS= read -r line; do
  if [ -z "$line" ]; then
    continue
  fi
  echo "$line" | jq -c '.' 2>/dev/null || echo "$line"
  PIECE=$(echo "$line" | jq -r '.message.content // empty' 2>/dev/null || true)
  FULL_CONTENT="${FULL_CONTENT}${PIECE}"
done < <(curl -sS --no-buffer -X POST "http://${BEEKET_HOST}:${BEEKET_PORT}/api/chat" \
  -H "Content-Type: application/json" \
  -d "$BODY")

echo "---"
echo ""
echo "Assembled structured output:"
echo "$FULL_CONTENT" | jq '.'
