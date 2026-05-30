# Beeket curl samples

Shell scripts for manually testing the Beeket chat completion API with `curl` and `jq`.

## Prerequisites

- `curl` and `jq` installed
- Beeket server running (`beeket serve`)
- At least one model pulled (e.g. `beeket pull smollm2:135m`)

## Environment variables

| Variable       | Default        | Description                          |
|----------------|----------------|--------------------------------------|
| `BEEKET_HOST`  | `127.0.0.1`    | Hostname or IP of the Beeket server  |
| `BEEKET_PORT`  | `11435`        | Port the Beeket server listens on    |
| `MODEL`        | `smollm2:135m` | Model name to use for inference      |

## Scripts

### `chat-simple.sh`

Sends a single user message and prints the full JSON response through `jq`.

```bash
./samples/chat-simple.sh
```

### `chat-system.sh`

Sends a system prompt followed by a user message. Useful for testing persona / instruction following.

```bash
./samples/chat-system.sh
```

### `chat-simple-stream.sh`

Same as `chat-simple.sh` but with `"stream": true`. Each NDJSON chunk is printed and formatted by `jq` as it arrives.

```bash
./samples/chat-simple-stream.sh
```

### `chat-system-stream.sh`

Same as `chat-system.sh` with streaming enabled.

```bash
./samples/chat-system-stream.sh
```

### `chat-structured.sh`

Sends a chat request with a JSON Schema `format` constraint. The model is forced
to return a JSON object with `name` (string) and `age` (integer) fields.
Includes `think: false` to suppress chain-of-thought on reasoning models, and
low-temperature options for deterministic JSON.

```bash
./samples/chat-structured.sh
MODEL=qwen3.5-2b:q4_k_m ./samples/chat-structured.sh
```

### `chat-structured-stream.sh`

Streaming version of `chat-structured.sh`. Assembles the streamed tokens and
parses the final JSON with `jq`.

Note: if the assembled response does not match the requested JSON Schema,
the server returns an HTTP 422 error chunk at the end of the stream. The
script detects and displays these error chunks on stderr rather than trying
to parse them as content.

```bash
./samples/chat-structured-stream.sh
```

### `chat-tools.sh`

Demonstrates function/tool calling. The model selects and invokes a tool from
a list of provided function definitions.

```bash
./samples/chat-tools.sh
```

### `embed.sh`

Generates an embedding vector for a text input.

```bash
./samples/embed.sh
INPUT="The quick brown fox" ./samples/embed.sh
```

## Custom model or host

```bash
MODEL=mistral ./samples/chat-simple.sh
BEEKET_HOST=192.168.1.10 MODEL=smollm2:135m ./samples/chat-system.sh
```

## Structured output and the `think` parameter

Reasoning models (Qwen3, DeepSeek-R1, QwQ, etc.) generate a `<think>…</think>`
block before their response. When using `format` for structured output, this
preamble disrupts the grammar sampler. Always set `think: false`:

```json
{
  "model": "qwen3.5-2b:q4_k_m",
  "think": false,
  "format": { "..." : "..." },
  "options": {
    "temperature": 0.1,
    "top_p": 0.9,
    "num_predict": 512
  }
}
```

The `chat-structured.sh` and `chat-structured-stream.sh` scripts already
include these parameters.

## Sampling `options`

All scripts accept an `options` object to control sampling. Common parameters:

| Parameter | Type | Description |
|-----------|------|-------------|
| `temperature` | float | Randomness (0.0 = deterministic, 0.8 = default) |
| `top_p` | float | Nucleus sampling threshold |
| `top_k` | int | Restrict to top-K tokens |
| `num_predict` | int | Max tokens to generate |
| `seed` | int | RNG seed for reproducibility |
| `repeat_penalty` | float | Penalise token repetition (1.1 is a good start) |
| `mirostat` | int | Mirostat mode (0 = off, 1 = v1, 2 = v2) |

See [`docs/options.md`](../docs/options.md) for the full reference.

## API reference

All chat scripts target `POST /api/chat`. The request body follows the
Ollama-compatible format:

```json
{
  "model": "<model-name>",
  "stream": false,
  "think": false,
  "messages": [
    { "role": "system", "content": "<optional system prompt>" },
    { "role": "user",   "content": "<user message>" }
  ],
  "options": {
    "temperature": 0.8,
    "num_predict": 512
  }
}
```

See [`docs/spec-v0.1.md`](../docs/spec-v0.1.md) for the full API specification
and [`docs/options.md`](../docs/options.md) for all sampling parameters.
