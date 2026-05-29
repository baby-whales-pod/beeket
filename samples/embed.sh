#!/usr/bin/env bash
# embed.sh — generate an embedding vector with beeket
#
# Usage:
#   ./samples/embed.sh
#   MODEL=mxbai-embed-large:latest ./samples/embed.sh
#   BEEKET_HOST=0.0.0.0 BEEKET_PORT=11436 ./samples/embed.sh
#
# Environment variables:
#   BEEKET_HOST  — server host  (default: 127.0.0.1)
#   BEEKET_PORT  — server port  (default: 11435)
#   MODEL        — model to use (default: nomic-embed-text:latest)
#   INPUT        — text to embed (default: "The quick brown fox")

set -euo pipefail

HOST="${BEEKET_HOST:-127.0.0.1}"
PORT="${BEEKET_PORT:-11435}"
MODEL="${MODEL:-nomic-embed-text:latest}"
INPUT="${INPUT:-The quick brown fox}"

BASE_URL="http://${HOST}:${PORT}"

echo "Server : ${BASE_URL}"
echo "Model  : ${MODEL}"
echo "Input  : ${INPUT}"
echo ""

RESPONSE=$(curl -s -X POST "${BASE_URL}/api/embeddings" \
  -H "Content-Type: application/json" \
  -d "$(jq -n --arg model "${MODEL}" --arg input "${INPUT}" \
        '{model: $model, input: $input}')")

echo "Response:"
echo "${RESPONSE}" | jq .

# Print the first 8 dimensions and the vector length.
echo ""
echo "First 8 dimensions : $(echo "${RESPONSE}" | jq '.embeddings[0][:8]')"
echo "Vector length       : $(echo "${RESPONSE}" | jq '.embeddings[0] | length')"
echo "Prompt token count  : $(echo "${RESPONSE}" | jq '.prompt_eval_count')"
