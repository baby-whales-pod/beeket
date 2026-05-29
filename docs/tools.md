# Tool Calling in Beeket

Beeket supports tool calling (function calling) via a grammar-constrained generation pipeline. When `tools` are provided in a `/api/chat` request, Beeket:

1. Injects a system-message preamble listing the available tools.
2. Builds a GBNF grammar derived from the tool JSON schemas.
3. Runs generation with a lazy-trigger grammar sampler (`{` activates the grammar).
4. Parses the model output: if a valid `{"name": ..., "arguments": {...}}` object is found, it surfaces it as `tool_calls` in the response.

---

## Wire format (Ollama-compatible)

### Request

```json
POST /api/chat
{
  "model": "qwen2.5:0.5b",
  "messages": [
    {"role": "user", "content": "What is the weather in Paris?"}
  ],
  "tools": [
    {
      "type": "function",
      "function": {
        "name": "get_weather",
        "description": "Get current weather for a city.",
        "parameters": {
          "type": "object",
          "properties": {
            "city": {"type": "string"}
          },
          "required": ["city"]
        }
      }
    }
  ]
}
```

### Response — tool call detected

```json
{
  "model": "qwen2.5:0.5b",
  "created_at": "...",
  "message": {
    "role": "assistant",
    "content": "",
    "tool_calls": [
      {
        "function": {
          "name": "get_weather",
          "arguments": {"city": "Paris"}
        }
      }
    ]
  },
  "done": true,
  "done_reason": "tool_calls"
}
```

### Response — no tool call (model responded with prose)

```json
{
  "model": "qwen2.5:0.5b",
  "created_at": "...",
  "message": {
    "role": "assistant",
    "content": "I don't know the current weather."
  },
  "done": true
}
```

---

## Tool result messages

After receiving a `tool_calls` response, call the tool and send the result back as a `role: "tool"` message:

```json
{
  "model": "qwen2.5:0.5b",
  "messages": [
    {"role": "user",      "content": "What is the weather in Paris?"},
    {"role": "assistant", "content": "", "tool_calls": [{"function": {"name": "get_weather", "arguments": {"city": "Paris"}}}]},
    {"role": "tool",      "content": "{\"temperature\": 22, \"condition\": \"sunny\"}", "tool_name": "get_weather"}
  ],
  "tools": [...]
}
```

Beeket rewrites `role: "tool"` messages into `role: "user"` messages before applying the chat template, since yzma's `ChatApplyTemplate` does not expose a native tool role.

---

## Supported JSON-schema types

| Schema type | Support |
|---|---|
| `object` with `properties` | ✅ |
| `string` | ✅ |
| `integer` | ✅ |
| `number` | ✅ |
| `boolean` | ✅ |
| `null` | ✅ |
| `array` (of supported types) | ✅ |
| `enum` (string values) | ✅ |
| nested `object` | ✅ |
| `anyOf`, `oneOf`, `allOf` | ⚠️ Falls back to unconstrained `json-value` |
| `$ref` | ⚠️ Falls back to unconstrained `json-value` |

Unknown schema constructs fall back to a permissive JSON-value rule. A warning is logged.

---

## Limitations

- **No native tool-template support** — yzma v1.13.0's `ChatApplyTemplate` does not support tool roles. Beeket renders its own system-message preface. Quality depends on the model. A future yzma update adding tool-aware template rendering would allow swapping in the model's native prompt.
- **Streaming is buffered** — when `tools` are present, token streaming is disabled. The response is delivered atomically once generation completes. Plain chat (no tools) continues to stream normally.
- **Single tool call per turn** — the grammar produces a single JSON object. Parallel tool calls (an array) are not supported in v0.1.
- **Grammar rebuilds per request** — a fresh sampler chain is built and freed for each tool-calling request. Performance overhead is small but benchmarkable.

---

## curl examples

See [`samples/chat-tools.sh`](../samples/chat-tools.sh) for a runnable example.

```bash
curl -s http://localhost:11434/api/chat \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "qwen2.5:0.5b",
    "messages": [{"role":"user","content":"What is the weather in Paris?"}],
    "tools": [{
      "type": "function",
      "function": {
        "name": "get_weather",
        "description": "Get current weather for a city.",
        "parameters": {
          "type": "object",
          "properties": {"city": {"type": "string"}},
          "required": ["city"]
        }
      }
    }]
  }'
```
