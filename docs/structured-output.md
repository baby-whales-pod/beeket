# Structured Output

Structured output constrains the model to generate tokens that form valid JSON matching a given JSON Schema. This is useful for reliably extracting structured data from free-form text.

## How it works

Beeket uses the llama.cpp grammar sampler to enforce output constraints at the token level. Every sampled token is checked against a GBNF (grammar-based next token filtering) grammar derived from the schema. Tokens that would violate the grammar are masked out before sampling.

The `format` field is accepted on both `/api/chat` and `/api/generate`.

## Usage

### JSON mode (any valid JSON object)

Pass `"format": "json"` to constrain output to any valid JSON value.

```bash
curl -s http://${BEEKET_HOST:-127.0.0.1}:${BEEKET_PORT:-11435}/api/chat \
  -H "Content-Type: application/json" \
  -d '{
    "model": "smollm2:135m",
    "stream": false,
    "format": "json",
    "messages": [
      {"role": "user", "content": "Return a JSON object with a greeting field."}
    ]
  }' | jq '.message.content | fromjson'
```

### JSON Schema mode (constrained to a schema)

Pass a JSON Schema object as `"format"` to constrain the output to matching that schema.

```bash
curl -s http://${BEEKET_HOST:-127.0.0.1}:${BEEKET_PORT:-11435}/api/chat \
  -H "Content-Type: application/json" \
  -d '{
    "model": "smollm2:135m",
    "stream": false,
    "format": {
      "type": "object",
      "properties": {
        "name":  {"type": "string"},
        "age":   {"type": "integer"},
        "email": {"type": "string"}
      },
      "required": ["name", "age"]
    },
    "messages": [
      {"role": "user", "content": "Extract: Alice is 28 years old. Her email is alice@example.com"}
    ]
  }' | jq '.message.content | fromjson'
```

**Example output:**
```json
{
  "name": "Alice",
  "age": 28,
  "email": "alice@example.com"
}
```

### Streaming with structured output

Structured output works with streaming too. Each chunk is a partial token; the final assembled content is valid JSON.

```bash
curl -s http://${BEEKET_HOST:-127.0.0.1}:${BEEKET_PORT:-11435}/api/chat \
  -H "Content-Type: application/json" \
  -d '{
    "model": "smollm2:135m",
    "stream": true,
    "format": {
      "type": "object",
      "properties": {
        "capital": {"type": "string"},
        "country": {"type": "string"}
      },
      "required": ["capital", "country"]
    },
    "messages": [
      {"role": "user", "content": "What is the capital of France?"}
    ]
  }'
```

### Using with `/api/generate`

The `format` field also works on the generate endpoint:

```bash
curl -s http://${BEEKET_HOST:-127.0.0.1}:${BEEKET_PORT:-11435}/api/generate \
  -H "Content-Type: application/json" \
  -d '{
    "model": "smollm2:135m",
    "stream": false,
    "format": {
      "type": "object",
      "properties": {
        "sentiment": {"type": "string", "enum": ["positive", "negative", "neutral"]},
        "score":     {"type": "number"}
      },
      "required": ["sentiment", "score"]
    },
    "prompt": "Classify the sentiment: I love this product!"
  }' | jq '.response | fromjson'
```

## Supported schema features

| Feature | Supported |
|---|---|
| `type: string` | ✅ |
| `type: number` | ✅ |
| `type: integer` | ✅ |
| `type: boolean` | ✅ |
| `type: null` | ✅ |
| `type: object` with `properties` | ✅ |
| `type: array` with `items` | ✅ |
| `required` properties | ✅ |
| Optional properties | ✅ |
| Nested objects | ✅ |
| `enum` (string, number, boolean, null) | ✅ |
| `anyOf` / `oneOf` | ✅ |
| `const` | ✅ |
| `allOf` | ❌ (not supported) |
| `$ref` / `$defs` | ❌ (not supported) |
| `additionalProperties` | ❌ (ignored) |
| String format validators (`date`, `email`, etc.) | ❌ (ignored) |
| `minLength`, `maxLength`, `pattern` | ❌ (ignored) |
| `minimum`, `maximum` | ❌ (ignored) |

## Error handling

| Condition | HTTP status | Error message |
|---|---|---|
| `format` is an unsupported string (not `"json"`) | 400 | `unsupported format value "..."; use "json" or a JSON Schema object` |
| `format` is an invalid JSON Schema | 400 | `invalid JSON Schema in format field: ...` |
| `format` is a non-string, non-object type | 400 | `format must be "json" or a JSON Schema object` |

## Limitations

- **Property ordering**: Properties are emitted in alphabetical order within an object (required properties first). The model may not always fill optional properties.
- **No `additionalProperties: false`**: Extra properties are not blocked; the grammar only enforces declared properties.
- **Context window**: The grammar sampler adds overhead. For deeply nested schemas, the GBNF grammar can be large. Keep schemas shallow.
- **Model quality**: Grammar constraints enforce *structure*, not *correctness*. A low-quality model may produce `{"name": "", "age": 0}` even when constrained.
- **`$ref` and `$defs`** are not resolved; use inline schemas only.
