# Beeket

**Beeket** is a self-hosted, [Ollama](https://ollama.com)-compatible runtime for running GGUF language models locally. Written in Go, powered by [Yzma](https://github.com/hybridgroup/yzma) (pure-Go FFI bindings to llama.cpp).

> **Status:** v0.1 — initial implementation. API is Ollama-compatible for core endpoints.

## Quick Start

```bash
# Install
go install github.com/baby-whales-pod/beeket/cmd/beeket@latest
go install github.com/baby-whales-pod/beeket/cmd/beeket@latest

# Start the server (port 11435)
beeket serve

# Pull a model
beeket pull smollm2:135m

# Run a prompt
beeket run smollm2:135m -p "Are you ready to go?"
```

## API (Ollama-compatible)

| Endpoint | Description |
|---|---|
| `POST /api/pull` | Download a model (streams NDJSON progress) |
| `GET /api/tags` | List installed models |
| `POST /api/show` | Show model details |
| `DELETE /api/delete` | Remove a model |
| `POST /api/copy` | Alias a model |
| `POST /api/generate` | Text generation (streams NDJSON) |
| `POST /api/chat` | Multi-turn chat (streams NDJSON) |
| `POST /api/embeddings` | Embeddings |
| `GET /api/version` | Server version |
| `GET /api/ps` | Loaded models |
| `GET /healthz` | Liveness probe |
| `GET /readyz` | Readiness probe |

## Model References

```bash
beeket pull smollm2:135m                           # built-in alias
beeket pull hf.co/Qwen/Qwen2.5-0.5B-Instruct-GGUF:Q4_K_M
beeket pull https://huggingface.co/.../model.gguf  # direct URL
beeket pull file:///path/to/local/model.gguf       # local file
```

## Configuration

Default config file: `~/.config/beeket/beeket.toml`

```toml
[server]
host = "127.0.0.1"
port = 11435

[runtime]
backend      = "auto"   # auto | cpu | cuda | metal | vulkan | rocm
gpu_layers   = -1       # -1 = offload all layers
num_parallel = 1
max_loaded   = 3
keep_alive   = "5m"
context_size = 4096

[download]
concurrency = 4

[log]
level  = "info"   # debug | info | warn | error
format = "text"   # text | json
```

All settings can also be set via `BEEKET_*` env vars or CLI flags:

```bash
beeket serve --port 11435 --log-level debug --backend cuda
```

## Built-in Aliases

| Alias | Model |
|---|---|
| `smollm2:135m` | QuantFactory/SmolLM2-135M-GGUF Q4_K_M |
| `qwen2.5:0.5b` | Qwen/Qwen2.5-0.5B-Instruct-GGUF Q4_K_M |
| `gemma3:1b` | google/gemma-3-1b-it-GGUF Q4_K_M |
| `nomic-embed-text` | nomic-ai/nomic-embed-text-v1.5-GGUF Q4_K_M |

## Requirements

- Go 1.22+
- A llama.cpp shared library (`libllama.so` / `libllama.dylib` / `llama.dll`).
  Set `YZMA_LIB` or `--lib-dir` to point to it.

## Specification

See [docs/spec-v0.1.md](docs/spec-v0.1.md) for the full design specification.

## License

Apache-2.0
