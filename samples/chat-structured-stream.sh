#!/usr/bin/env bash
# chat-structured-stream.sh — send a streaming chat request with a JSON Schema
# format constraint and assemble the streamed tokens into a final JSON object.
#
# Each NDJSON line is printed as it arrives. Error chunks (HTTP 422 from
# schema validation) are detected and displayed separately. The final
# assembled content is searched for the first valid JSON object and pretty-
# printed with jq.
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

# Collect all chunks, print each as it arrives, skip error chunks gracefully.
FULL_CONTENT=""
while IFS= read -r line; do
  if [ -z "$line" ]; then
    continue
  fi

  # Detect error chunks (e.g. HTTP 422 schema validation failure) and warn
  # on stderr instead of trying to parse the error as content.
  if echo "$line" | jq -e '.error' > /dev/null 2>&1; then
    echo "⚠️  Server error: $(echo "$line" | jq -r '.error')" >&2
    continue
  fi

  # Print chunk as pretty-printed JSON; fall back to raw if jq can't parse it.
  echo "$line" | jq -c '.' 2>/dev/null || echo "$line"

  # Accumulate content pieces.
  PIECE=$(echo "$line" | jq -r '.message.content // empty' 2>/dev/null || true)
  FULL_CONTENT="${FULL_CONTENT}${PIECE}"
done < <(curl -sS --no-buffer -X POST "http://${BEEKET_HOST}:${BEEKET_PORT}/api/chat" \
  -H "Content-Type: application/json" \
  -d "$BODY")

echo "---"
echo ""
echo "Assembled content:"
echo "$FULL_CONTENT"
echo ""
echo "Parsed JSON:"

# Extract the first valid JSON object from the assembled content.
# The model may emit a <think>…</think> block or other prose before the JSON;
# python3 scans for the first balanced { … } and validates it.
PARSED=$(echo "$FULL_CONTENT" | python3 -c "
import sys, json, re
content = sys.stdin.read()
match = re.search(r'\{.*\}', content, re.DOTALL)
if match:
    try:
        obj = json.loads(match.group())
        print(json.dumps(obj, indent=2))
        sys.exit(0)
    except json.JSONDecodeError:
        pass
# Fallback: print content as-is so the user can inspect it
sys.stdout.write(content)
sys.exit(1)
" 2>/dev/null) && echo "$PARSED" || {
  # python3 extraction failed — try jq directly as last resort
  echo "$FULL_CONTENT" | jq '.' 2>/dev/null || echo "$FULL_CONTENT"
}
