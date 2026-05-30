#!/usr/bin/env bash
# chat-tools.sh — demonstrates tool calling (function calling) in Beeket.
#
# Usage:
#   BEEKET_HOST=http://localhost:11434 bash samples/chat-tools.sh
#
# Requirements:
#   - Beeket server running with a chat model loaded (e.g. qwen2.5:0.5b).
#   - jq installed (required for building the JSON body safely).

set -euo pipefail

if ! command -v jq > /dev/null 2>&1; then
  echo "Error: jq is required but not installed." >&2
  exit 1
fi

HOST="${BEEKET_HOST:-http://localhost:11434}"
MODEL="${MODEL:-qwen3.5-2b:q4_k_m}"

echo "=== Step 1: Send user message with tool definition ==="

STEP1_BODY=$(jq -n \
  --arg model "$MODEL" \
  '{
    model: $model,
    stream: false,
    messages: [
      {role: "user", content: "What is the weather in Paris?"}
    ],
    tools: [
      {
        type: "function",
        function: {
          name: "get_weather",
          description: "Get current weather for a city.",
          parameters: {
            type: "object",
            properties: {
              city: {type: "string", description: "City name"}
            },
            required: ["city"]
          }
        }
      }
    ]
  }')

RESPONSE=$(curl -s "${HOST}/api/chat" \
  -H 'Content-Type: application/json' \
  -d "$STEP1_BODY")

echo "$RESPONSE" | jq .

DONE_REASON=$(echo "$RESPONSE" | jq -r '.done_reason // ""')

if [ "$DONE_REASON" != "tool_calls" ]; then
  echo ""
  echo "Model did not emit a tool call (done_reason=${DONE_REASON})."
  echo "Try a different model or check the response above."
  exit 0
fi

echo ""
echo "=== Step 2: Execute the tool (simulated) and send result back ==="

# In a real application you would inspect the tool_calls array and call the
# actual function. Here we hard-code a fake result.
TOOL_RESULT='{"temperature": 22, "condition": "sunny", "humidity": "60%"}'

STEP2_BODY=$(jq -n \
  --arg model "$MODEL" \
  --arg tool_result "$TOOL_RESULT" \
  '{
    model: $model,
    stream: false,
    messages: [
      {role: "user",      content: "What is the weather in Paris?"},
      {role: "assistant", content: "",
       tool_calls: [{function: {name: "get_weather", arguments: {city: "Paris"}}}]},
      {role: "tool",      content: $tool_result, tool_name: "get_weather"}
    ],
    tools: [
      {
        type: "function",
        function: {
          name: "get_weather",
          description: "Get current weather for a city.",
          parameters: {
            type: "object",
            properties: {
              city: {type: "string"}
            },
            required: ["city"]
          }
        }
      }
    ]
  }')

RESPONSE2=$(curl -s "${HOST}/api/chat" \
  -H 'Content-Type: application/json' \
  -d "$STEP2_BODY")

echo "$RESPONSE2" | jq .
