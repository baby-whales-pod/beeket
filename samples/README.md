# Beeket curl samples

Shell scripts for manually testing the Beeket chat completion API with `curl` and `jq`.

## Prerequisites

- `curl` and `jq` installed
- Beeket server running (`beeket serve` or `beeketd`)
- At least one model pulled (e.g. `beeket pull llama3.2`)

## Environment variables

| Variable       | Default     | Description                          |
|----------------|-------------|--------------------------------------|
| `BEEKET_HOST`  | `localhost` | Hostname or IP of the Beeket server  |
| `BEEKET_PORT`  | `11435`     | Port the Beeket server listens on    |
| `MODEL`        | `llama3.2`  | Model name to use for inference      |

## Scripts

### `chat-simple.sh`

Sends a single user message and prints the full JSON response through `jq`.

```bash
./samples/chat-simple.sh
```

Sample output:

```json
{
  "model": "llama3.2",
  "created_at": "2024-01-01T00:00:00Z",
  "message": {
    "role": "assistant",
    "content": "The sky appears blue because..."
  },
  "done": true,
  ...
}
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

## Custom model or host

```bash
MODEL=mistral ./samples/chat-simple.sh
BEEKET_HOST=192.168.1.10 MODEL=llama3.2 ./samples/chat-system.sh
```

## API reference

All scripts target `POST /api/chat`. The request body follows the Ollama-compatible format:

```json
{
  "model": "<model-name>",
  "stream": false,
  "messages": [
    { "role": "system", "content": "<optional system prompt>" },
    { "role": "user",   "content": "<user message>" }
  ]
}
```

See `docs/spec-v0.1.md` for the full API specification.
