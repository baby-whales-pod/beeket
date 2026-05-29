# Sampling Options

All `POST /api/chat` and `POST /api/generate` requests accept an `options`
object that controls sampling behaviour. Fields not provided fall back to
server defaults.

## Complete options reference

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `temperature` | float | 0.8 | Randomness — 0.0 = deterministic, higher = more creative |
| `top_k` | int | 40 | Restrict sampling to the top-K most-likely tokens |
| `top_p` | float | 0.9 | Nucleus sampling — cumulative probability cutoff |
| `min_p` | float | 0.05 | Minimum token probability relative to the most-likely token |
| `typical_p` | float | 0.0 | Locally Typical Sampling (0.0 = disabled; replaces `top_p` when set) |
| `seed` | uint | 0 | RNG seed for reproducibility (0 = random) |
| `num_predict` | int | 0 | Max tokens to generate; 0 = server default, -1 = unlimited |
| `stop` | []string | [] | Stop sequences — generation halts when any sequence is produced |
| `repeat_penalty` | float | 0.0 | Penalise repeated tokens (0.0/1.0 = disabled; try 1.1) |
| `repeat_last_n` | int | 0 | Token window for repeat penalty (0 → 64; -1 = full context) |
| `frequency_penalty` | float | 0.0 | Additive frequency penalty (0.0 = disabled) |
| `presence_penalty` | float | 0.0 | Additive presence penalty (0.0 = disabled) |
| `mirostat` | int | 0 | Mirostat mode: 0 = off, 1 = Mirostat v1, 2 = Mirostat v2 |
| `mirostat_tau` | float | 5.0 | Mirostat target entropy (higher = more diverse) |
| `mirostat_eta` | float | 0.1 | Mirostat learning rate |

> **Note:** When `mirostat > 0`, the `top_k`, `top_p`, `min_p`, `typical_p`,
> and `temperature` samplers are bypassed. Mirostat controls the distribution
> directly.

## Accepted for Ollama compatibility (no effect at request time)

These fields are parsed and ignored. They are provided so that clients written
for Ollama work without modification:

`num_ctx`, `num_thread`, `num_gpu`, `keep_alive`, `penalize_newline`, `tfs_z`

`num_ctx` is set at model-load time (see `--context-size`); it cannot be
changed per-request.

## Example — creative chat

```bash
curl -s -X POST http://127.0.0.1:11435/api/chat \
  -H "Content-Type: application/json" \
  -d '{
    "model": "smollm2:135m",
    "messages": [{"role": "user", "content": "Tell me a short story."}],
    "options": {
      "temperature": 1.2,
      "top_p": 0.95,
      "num_predict": 300,
      "seed": 42
    }
  }' | jq '.message.content'
```

## Example — repetition penalty

```bash
curl -s -X POST http://127.0.0.1:11435/api/generate \
  -H "Content-Type: application/json" \
  -d '{
    "model": "smollm2:135m",
    "prompt": "Write a poem about the sea.",
    "options": {
      "temperature": 0.9,
      "repeat_penalty": 1.1,
      "repeat_last_n": 128,
      "num_predict": 200
    }
  }' | jq '.response'
```

## Example — Mirostat v2

```bash
curl -s -X POST http://127.0.0.1:11435/api/chat \
  -H "Content-Type: application/json" \
  -d '{
    "model": "smollm2:135m",
    "messages": [{"role": "user", "content": "Explain quantum entanglement."}],
    "options": {
      "mirostat": 2,
      "mirostat_tau": 5.0,
      "mirostat_eta": 0.1,
      "num_predict": 256
    }
  }' | jq '.message.content'
```

## Structured output recommendations

When using the `format` field for structured output, low temperature produces
more deterministic JSON. Also set `think: false` on reasoning models:

```bash
curl -s -X POST http://127.0.0.1:11435/api/chat \
  -H "Content-Type: application/json" \
  -d '{
    "model": "qwen3.5-2b:q4_k_m",
    "think": false,
    "format": {
      "type": "object",
      "properties": {"name": {"type": "string"}, "age": {"type": "integer"}},
      "required": ["name", "age"]
    },
    "messages": [{"role": "user", "content": "Alice is 30."}],
    "options": {
      "temperature": 0.1,
      "top_p": 0.9,
      "num_predict": 512
    }
  }' | jq '.message.content | fromjson'
```

See [Structured Output](./structured-output.md) for full schema documentation.
