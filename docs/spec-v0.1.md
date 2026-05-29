# Beeket — Specification v0.1 (Draft)

**Status:** Draft · First specification
**Date:** 2026-05-27
**Audience:** Beeket maintainers and early contributors

---

## 1. Project Overview

**Beeket** is a self-hosted, Ollama-like runtime for running GGUF language models locally. It is written entirely in **Go** and uses [**Yzma**](https://github.com/hybridgroup/yzma) (a pure-Go, FFI-based binding to `llama.cpp`) as its inference backend.

Beeket exposes a friendly **HTTP REST API** for chat, text generation, and embeddings, plus a small **CLI** for managing models and running the server. It is designed to be:

- **A single static Go binary** with no CGo and no Python runtime.
- **Drop-in compatible with the Ollama REST API** wherever it is reasonable, so existing clients (LangChain, OpenWebUI, plugins, etc.) work with minimal changes.
- **Hardware-accelerated** out of the box (CUDA, Metal, Vulkan, ROCm) via Yzma's pluggable backends.
- **Developer-friendly**: easy to embed, easy to fork, easy to read.

### 1.1 Goals

1. Provide an Ollama-compatible local inference server, but implemented as a clean Go application built around the Yzma library.
2. Support **LLM, VLM, SLM, and embedding** workloads using GGUF models from Hugging Face or local disk.
3. Offer **simple model management**: `pull`, `list`, `show`, `rm`, with on-disk caching.
4. Make **multi-request concurrency** correct and predictable (queueing + per-model worker pools).
5. Ship a **single static binary** plus a small loader for the `llama.cpp` shared library.
6. Run on **Linux, macOS (Apple Silicon), and Windows**, with optional GPU acceleration.

### 1.2 Non-Goals (for v0.1)

- Not a training, fine-tuning, or LoRA-merging framework. (LoRA *loading* may be added later; merging is out of scope.)
- Not a distributed inference cluster. Beeket is a single-node server. Horizontal scale is the operator's problem.
- Not an authentication / multi-tenant SaaS. The default binding is `127.0.0.1`. Auth is out of scope for v0.1.
- Not a model marketplace or registry of its own. We consume GGUF artifacts from Hugging Face (and arbitrary URLs); we do not host a registry like `registry.ollama.ai`.
- Not a Python/Node ecosystem. The application and SDKs we ship are Go.

### 1.3 Naming & Conventions

- Server binary: `beeketd`
- CLI binary: `beeket`
- Config dir: `$XDG_CONFIG_HOME/beeket` (default `~/.config/beeket`)
- Data dir:   `$XDG_DATA_HOME/beeket`   (default `~/.local/share/beeket`)
- Default port: `11435` (one above Ollama's `11434`, to avoid collision when both are installed)

---

## 2. Architecture

Beeket is a layered application. From top to bottom:

```
┌───────────────────────────────────────────────────────────────┐
│                          CLI (beeket)                         │
│            pull / run / list / rm / show / serve              │
└───────────────────────────────────────────────────────────────┘
                                │  (HTTP, local socket)
┌───────────────────────────────────────────────────────────────┐
│                       HTTP API (beeketd)                      │
│   /api/chat  /api/generate  /api/embeddings  /api/tags  ...   │
│              Ollama-compatible router + handlers              │
└───────────────────────────────────────────────────────────────┘
                                │
┌────────────────────────┬──────┴────────┬───────────────────────┐
│   Request Scheduler    │ Model Manager │   Download Manager    │
│ (queues, per-model     │ (load/unload, │ (HF resolver, resume,  │
│  workers, streaming)   │  metadata, GC)│  checksum, progress)   │
└────────────────────────┴───────────────┴───────────────────────┘
                                │
┌───────────────────────────────────────────────────────────────┐
│                Inference Engine (internal/engine)             │
│   thin wrapper around Yzma: Model, Context, Vocab, Sampler,   │
│           Batch, Decoder, Chat templates, MTMD                │
└───────────────────────────────────────────────────────────────┘
                                │
┌───────────────────────────────────────────────────────────────┐
│             Yzma library  +  llama.cpp shared lib             │
│        (purego/ffi → libllama.{so,dylib,dll})                 │
└───────────────────────────────────────────────────────────────┘
```

### 2.1 Components

- **CLI (`cmd/beeket`)** — thin client that talks to a running `beeketd` over HTTP, plus subcommands that operate directly on the on-disk store (e.g. `beeket pull` when no daemon is running).
- **Server (`cmd/beeketd`)** — long-running HTTP server. Owns the model registry, the scheduler, and the inference workers.
- **Inference Engine (`internal/engine`)** — wraps Yzma so the rest of the codebase never imports `pkg/llama` directly. Provides Go-friendly types (`Engine`, `Session`, `GenerateOptions`, `EmbedOptions`) and is the only place that touches FFI lifetimes.
- **Model Manager (`internal/models`)** — tracks installed models, their on-disk paths, metadata (size, quantization, family, context length), and current load state.
- **Download Manager (`internal/download`)** — resolves Hugging Face URIs (and arbitrary HTTPS URLs) to GGUF artifacts, streams them to the cache with resume + SHA256 verification, emits progress events.
- **Scheduler (`internal/scheduler`)** — accepts requests, enqueues them per loaded model, dispatches them to worker goroutines, and streams tokens back to handlers.
- **Storage (`internal/store`)** — typed access to the on-disk layout described in §7.

### 2.2 Process Model

Beeket is a single OS process. Concurrency is achieved with goroutines:

- One HTTP server goroutine pool (Go's `net/http`).
- One scheduler goroutine per **loaded** model that owns its `llama.Context` (FFI contexts are not safe to share across goroutines).
- A small pool of download goroutines, bounded by config.
- The Yzma `llama.cpp` shared library is loaded **once at startup** via `llama.Load()` + `llama.Init()`.

---

## 3. API Design

All endpoints accept and return JSON. Streaming endpoints return **newline-delimited JSON (NDJSON)**, matching Ollama's wire format. Base path is `/api`.

Endpoints we plan to be **Ollama wire-compatible** with for v0.1 are marked **[OLM]**.

### 3.1 Model Management

#### `POST /api/pull` **[OLM]**

Download a GGUF model into the local store. Streams progress as NDJSON.

```json
// request
{ "name": "hf.co/QuantFactory/SmolLM2-135M-GGUF:Q4_K_M", "stream": true }
```

```json
// response (NDJSON, one per line)
{"status":"resolving manifest"}
{"status":"downloading","digest":"sha256:...","total":91234567,"completed":1048576}
{"status":"verifying sha256"}
{"status":"success"}
```

Name resolution rules:
- `hf.co/<org>/<repo>[:<quant>]` — Hugging Face GGUF repo. If `<quant>` is omitted, Beeket picks a sensible default (`Q4_K_M` if present, otherwise the smallest `*.gguf` in the repo).
- `https://...gguf` — direct URL.
- Bare name (`smollm2:135m`) — resolved against an in-binary alias table (see §4.3) so that out-of-the-box examples Just Work.

#### `GET /api/tags` **[OLM]**

List installed models.

```json
{
  "models": [
    {
      "name": "smollm2:135m",
      "model": "smollm2:135m",
      "size": 91234567,
      "digest": "sha256:...",
      "modified_at": "2026-05-27T08:00:00Z",
      "details": {
        "family": "llama",
        "parameter_size": "135M",
        "quantization_level": "Q4_K_M",
        "context_length": 8192,
        "format": "gguf"
      }
    }
  ]
}
```

#### `POST /api/show` **[OLM]**

Return full metadata for a single model (GGUF header keys, chat template, modalities).

#### `DELETE /api/delete` **[OLM]**

Remove a model from disk.

```json
{ "name": "smollm2:135m" }
```

#### `POST /api/copy` **[OLM]**

Copy/rename a model (cheap — only updates the alias index, blob is shared by digest).

### 3.2 Inference

#### `POST /api/generate` **[OLM]**

Single-turn text completion.

```json
{
  "model": "smollm2:135m",
  "prompt": "Are you ready to go?",
  "stream": true,
  "options": {
    "temperature": 0.8,
    "top_k": 40,
    "top_p": 0.9,
    "num_predict": 128,
    "seed": 0,
    "stop": ["</s>"]
  }
}
```

Streaming response (NDJSON):

```json
{"model":"smollm2:135m","response":"Yes","done":false}
{"model":"smollm2:135m","response":", ","done":false}
{"model":"smollm2:135m","response":"I'm ready.","done":false}
{"model":"smollm2:135m","response":"","done":true,
 "total_duration":123456789,"prompt_eval_count":7,"eval_count":42}
```

#### `POST /api/chat` **[OLM]**

Multi-turn chat. Supports text-only and VLM inputs (`images` is base64-encoded PNG/JPEG).

```json
{
  "model": "qwen2.5-vl:3b",
  "messages": [
    {"role": "system", "content": "You are helpful."},
    {"role": "user", "content": "What is in this picture?",
     "images": ["<base64>"]}
  ],
  "stream": true,
  "tools": [ /* OpenAI-style tool schemas, optional */ ]
}
```

Chat templating is performed via Yzma's `pkg/template` + GGUF chat-template metadata so model-specific formats (ChatML, Llama-3, Gemma, Qwen) are handled centrally.

#### `POST /api/embeddings` **[OLM]**

Generate embedding vectors. Accepts a single string or an array of strings.

```json
{ "model": "nomic-embed-text", "input": ["hello", "world"] }
```

```json
{ "model": "nomic-embed-text",
  "embeddings": [[0.01, -0.02, ...], [0.03, 0.04, ...]] }
```

### 3.3 Operational

- `GET /api/version` — Beeket and Yzma versions, llama.cpp build tag.
- `GET /api/ps` — currently loaded models, VRAM/RAM footprint, last-used timestamp.
- `POST /api/unload` — explicit unload of a model (Beeket also unloads on idle, see §5.3).
- `GET /healthz` — liveness; `GET /readyz` — readiness (engine initialized).

### 3.4 Compatibility Boundaries

We intentionally **do not** promise compatibility with:
- Ollama's `Modelfile` build pipeline. v0.1 ships **no** `POST /api/create`. A Beeket equivalent may come later.
- Ollama's private registry protocol. Pull is HTTPS / Hugging Face only.
- Any undocumented Ollama internals (`/api/blobs/...`).

OpenAI-style endpoints (`/v1/chat/completions`, `/v1/embeddings`) are a **post-v0.1** goal — see §10.

---

## 4. Model Management

### 4.1 Source Resolution

A *model reference* in Beeket is one of:

| Form | Meaning |
|---|---|
| `hf.co/<org>/<repo>` | Resolve default quantization in the HF repo |
| `hf.co/<org>/<repo>:<quant>` | Specific file by quantization tag |
| `hf.co/<org>/<repo>/<file>.gguf` | Direct file in an HF repo |
| `https://.../model.gguf` | Direct HTTPS download |
| `file:///abs/path/model.gguf` | Pre-existing local file (imported, not copied) |
| `<alias>` | Looked up in the built-in alias table |

Hugging Face URLs are normalized to the `resolve/main/...` form that Yzma's examples already use, and that the `pkg/download` package understands. We reuse Yzma's downloader primitives where possible and wrap them in our own progress + resume logic so HTTP streaming back to the client is easy.

### 4.2 On-Disk Layout

Beeket uses a **content-addressed blob store** plus a **manifest index**, similar in spirit to Ollama but simpler:

```
$DATA_DIR/
├── blobs/
│   └── sha256-<digest>            # raw GGUF file, immutable
├── manifests/
│   └── <name>/<tag>.json          # { "name", "digest", "size", "details", "source" }
├── mmproj/
│   └── sha256-<digest>            # vision projector blobs (for VLMs)
└── tmp/                           # in-flight downloads (resumable)
```

Multiple aliases pointing at the same GGUF share a single blob.

### 4.3 Metadata

On pull (and lazily on first use), Beeket reads the **GGUF header** via Yzma to capture:

- Architecture / family (`llama`, `qwen2`, `gemma3`, `phi3`, ...)
- Parameter count, context length, embedding dimension
- Quantization scheme (`Q4_K_M`, `Q8_0`, `F16`, ...)
- Chat template string (if present)
- Modalities (text / vision / embedding)

This metadata is cached in the manifest JSON to avoid re-opening the file for `/api/tags`.

### 4.4 Built-in Aliases (initial set)

A small, opinionated alias table compiled into the binary so first-time users have something that works:

```
smollm2:135m       → hf.co/QuantFactory/SmolLM2-135M-GGUF:Q4_K_M
qwen2.5:0.5b       → hf.co/Qwen/Qwen2.5-0.5B-Instruct-GGUF:Q4_K_M
qwen2.5-vl:3b      → hf.co/Qwen/Qwen2.5-VL-3B-Instruct-GGUF:Q8_0  (+ mmproj)
gemma3:1b          → hf.co/google/gemma-3-1b-it-GGUF:Q4_K_M
nomic-embed-text   → hf.co/nomic-ai/nomic-embed-text-v1.5-GGUF:Q4_K_M
```

Aliases can be overridden via `~/.config/beeket/aliases.toml`.

---

## 5. Concurrency Model

### 5.1 The FFI Constraint

A `llama.Context` is a stateful FFI handle with its own KV cache. It is **not safe** to call `llama.Decode` on the same context from multiple goroutines simultaneously. Beeket's scheduler is designed around this constraint.

### 5.2 Per-Model Worker

For each currently **loaded** model we run a single dedicated worker goroutine. It owns:

- One `llama.Model` handle (shareable across contexts).
- One or more `llama.Context` handles (one per parallel slot — see below).
- A request channel; the HTTP handler enqueues work and reads streamed tokens from a per-request output channel.

```
HTTP handler ──► requestCh ──► [worker for model M] ──► tokenCh ──► HTTP response
```

### 5.3 Lifecycle of a Loaded Model

1. **Idle**: model not in memory. First request triggers a load (with progress reflected in the response only if it takes longer than `LOAD_GRACE` ≈ 200 ms).
2. **Loaded**: serves requests. `last_used_at` is bumped on every request.
3. **Eviction**: a background goroutine unloads any model idle for longer than `KEEP_ALIVE` (default 5 min, configurable per request via `keep_alive`, matching Ollama).
4. **Memory pressure**: if a new load would exceed `MAX_LOADED_MODELS` or `MAX_RAM_BYTES`, the **least-recently-used** loaded model is evicted first.

### 5.4 Parallel Slots Within a Model

For models that comfortably fit in memory we support **N parallel slots** — i.e. N `llama.Context` instances sharing a single `llama.Model`. The scheduler round-robins requests across slots. This trades RAM for throughput when many short requests arrive concurrently.

- Default `num_parallel = 1`.
- Configurable globally and per-model.
- Streams from each slot are independent.

### 5.5 Request Lifetimes & Cancellation

- Every request carries a `context.Context` plumbed from `net/http`.
- If the HTTP client disconnects, the handler cancels the request. The worker checks for cancellation between `llama.Decode` calls and aborts the generation loop cleanly (freeing the batch, leaving the KV cache for the slot in a consistent state by truncating to the last committed position).

### 5.6 Backpressure

Per-model request queues are bounded (default depth 32). When full, new requests get `429 Too Many Requests` with a `Retry-After` header. This is a deliberate, documented behavior — we prefer fast failure to unbounded latency.

---

## 6. Configuration

Configuration is layered (later layers override earlier ones):

1. Compiled-in defaults.
2. Config file: `~/.config/beeket/beeket.toml` (path overridable with `--config`).
3. Environment variables prefixed `BEEKET_`.
4. Command-line flags.

### 6.1 Key Settings

| Key (TOML)              | Env                          | Flag                  | Default                        |
|-------------------------|------------------------------|-----------------------|--------------------------------|
| `server.host`           | `BEEKET_HOST`                | `--host`              | `127.0.0.1`                    |
| `server.port`           | `BEEKET_PORT`                | `--port`              | `11435`                        |
| `server.origins`        | `BEEKET_ORIGINS`             | `--origins`           | `http://localhost,...`         |
| `paths.data_dir`        | `BEEKET_DATA_DIR`            | `--data-dir`          | XDG data dir                   |
| `paths.lib_dir`         | `BEEKET_LIB_DIR` / `YZMA_LIB`| `--lib-dir`           | auto-detect                    |
| `runtime.backend`       | `BEEKET_BACKEND`             | `--backend`           | `auto` (cuda/metal/vulkan/cpu) |
| `runtime.gpu_layers`    | `BEEKET_GPU_LAYERS`          | `--gpu-layers`        | `-1` (offload all if possible) |
| `runtime.num_parallel`  | `BEEKET_NUM_PARALLEL`        | `--num-parallel`      | `1`                            |
| `runtime.max_loaded`    | `BEEKET_MAX_LOADED_MODELS`   | `--max-loaded-models` | `3`                            |
| `runtime.keep_alive`    | `BEEKET_KEEP_ALIVE`          | `--keep-alive`        | `5m`                           |
| `runtime.context_size`  | `BEEKET_CONTEXT_SIZE`        | `--context-size`      | `4096`                         |
| `download.concurrency`  | `BEEKET_DOWNLOAD_CONCURRENCY`| `--download-conc`     | `4`                            |
| `log.level`             | `BEEKET_LOG_LEVEL`           | `--log-level`         | `info`                         |
| `log.format`            | `BEEKET_LOG_FORMAT`          | `--log-format`        | `text` (`json`)                |

### 6.2 CLI Surface (sketch)

```
beeket serve            # start beeketd
beeket pull <ref>       # download model
beeket list             # alias for /api/tags
beeket show <ref>
beeket rm <ref>
beeket run <ref> [-p "prompt"]   # one-shot generate against local daemon
beeket ps               # currently loaded models
beeket version
```

`beeket` autostarts `beeketd` in the background for `run` if no daemon is reachable (similar to Ollama UX). This is a stretch goal for v0.1.

---

## 7. Data Storage

### 7.1 Filesystem Layout

```
$XDG_DATA_HOME/beeket/                # default: ~/.local/share/beeket
├── blobs/                            # content-addressed GGUF files
│   ├── sha256-aaa...                 # 0644
│   └── sha256-bbb...
├── manifests/
│   └── <name>/
│       └── <tag>.json                # alias → digest + cached metadata
├── mmproj/                           # vision projector blobs
├── tmp/                              # partial downloads (resumable)
└── lib/                              # llama.cpp shared library, if managed by Beeket
    ├── libllama.so / .dylib / .dll
    └── version.txt

$XDG_CONFIG_HOME/beeket/              # default: ~/.config/beeket
├── beeket.toml
└── aliases.toml

$XDG_STATE_HOME/beeket/               # default: ~/.local/state/beeket
├── beeketd.log
└── beeketd.sock                      # optional Unix socket
```

### 7.2 Integrity

- All blobs are content-addressed by SHA-256. The digest is verified on download completion and (optionally) on load.
- Manifests reference blobs by digest; renaming a model never touches the blob.
- `beeketd` takes a file lock on `$DATA_DIR/.lock` at startup to prevent two daemons from racing on the same store.

### 7.3 llama.cpp Library

The `llama.cpp` shared library is **not** statically linked. It must be present at runtime. Resolution order:

1. `--lib-dir` / `BEEKET_LIB_DIR` / `YZMA_LIB`.
2. `$DATA_DIR/lib/`.
3. OS default search paths.
4. If none found and `--auto-install-lib` is set, Beeket invokes Yzma's installer (`pkg/loader`) to fetch a prebuilt library matching the platform/backend.

---

## 8. Technology Stack

- **Go 1.25+** (for `slog`, generics, structured logging, and `net/http` improvements).
- **[Yzma](https://github.com/hybridgroup/yzma) v1.14.x** — inference engine wrapper around llama.cpp.
- **`net/http`** from the standard library — no router framework for v0.1. We may add `chi` later if route fan-out justifies it.
- **`log/slog`** — structured logging.
- **`encoding/json`** — wire format. NDJSON is implemented with a thin wrapper around `json.Encoder`.
- **`github.com/BurntSushi/toml`** — config file parsing.
- **`github.com/spf13/cobra`** + **`pflag`** — CLI scaffolding (already idiomatic in Go).
- **`golang.org/x/sync/errgroup`**, **`semaphore`** — concurrency primitives.
- **`github.com/stretchr/testify`** — tests.
- **`github.com/hashicorp/go-multierror`** *(optional)* — error aggregation for shutdown.

External runtime dependency: the **llama.cpp shared library** (loaded by Yzma at runtime; not vendored into the Go binary).

We deliberately avoid:
- CGo (Yzma is FFI-based; we keep it that way).
- ORM / database libraries (the on-disk store is plain files + JSON).
- Heavyweight web frameworks.

---

## 9. Project Structure

```
beeket/
├── cmd/
│   ├── beeket/                # CLI (cobra root + subcommands)
│   │   └── main.go
│   └── beeketd/               # server entry point
│       └── main.go
├── internal/
│   ├── engine/                # Yzma wrapper: Engine, Session, Generate, Embed
│   │   ├── engine.go
│   │   ├── session.go
│   │   ├── sampler.go
│   │   └── mtmd.go            # vision/multimodal helpers
│   ├── api/                   # HTTP handlers + types
│   │   ├── server.go
│   │   ├── chat.go
│   │   ├── generate.go
│   │   ├── embeddings.go
│   │   ├── models.go
│   │   ├── ndjson.go
│   │   └── types.go
│   ├── scheduler/             # per-model workers, queues, eviction
│   │   ├── scheduler.go
│   │   ├── worker.go
│   │   └── slot.go
│   ├── models/                # registry, manifests, metadata
│   │   ├── manager.go
│   │   ├── manifest.go
│   │   └── alias.go
│   ├── download/              # HF resolver, resumable downloader
│   │   ├── hf.go
│   │   ├── downloader.go
│   │   └── progress.go
│   ├── store/                 # filesystem layout, locking
│   │   ├── store.go
│   │   └── paths.go
│   ├── config/                # layered config (file + env + flags)
│   │   ├── config.go
│   │   └── defaults.go
│   ├── chat/                  # chat templating, prompt construction
│   │   └── template.go
│   └── version/
│       └── version.go
├── pkg/
│   └── client/                # exported Go client for Beeket's HTTP API
│       └── client.go
├── docs/
│   ├── SPEC.md                # this document
│   ├── API.md
│   └── ARCHITECTURE.md
├── scripts/
├── testdata/
├── go.mod
├── go.sum
├── README.md
└── LICENSE                    # Apache-2.0 to match Yzma
```

Conventions:

- Everything that imports Yzma's `pkg/llama` lives **only** in `internal/engine`. The rest of the codebase talks to `engine.Engine` and `engine.Session`. This isolates FFI complexity, makes mocking easy, and limits the blast radius of upstream Yzma API changes.
- `pkg/client` is the only `pkg/` we publish — a small Go client library so other Go programs can call Beeket without rolling their own HTTP wrapper.

---

## 10. MVP Scope (v0.1)

### 10.1 In Scope for v0.1

- `beeketd` HTTP server on `127.0.0.1:11435`.
- Endpoints: `pull`, `tags`, `show`, `delete`, `generate`, `chat`, `embeddings`, `version`, `ps`, `healthz`.
- NDJSON streaming for `pull`, `generate`, `chat`.
- Text LLMs and embedding models. **Vision (VLM) support is a stretch goal** for v0.1; the engine is wired for it, but the CLI/API examples ship text-first.
- Hugging Face URL pulls + direct HTTPS pulls + local `file://` import.
- Content-addressed blob store with resume + SHA-256 verification.
- Per-model worker, LRU eviction, configurable `keep_alive`.
- `beeket` CLI: `serve`, `pull`, `list`, `show`, `rm`, `run`, `ps`, `version`.
- Linux (x86_64, arm64), macOS (arm64), Windows (x86_64).
- CPU backend; **GPU backends auto-detected** through Yzma when available.

### 10.2 Out of Scope for v0.1 (planned)

- **v0.2** — Full VLM endpoints with image upload helpers; tool use / function calling end-to-end; OpenAI-compatible `/v1/chat/completions` + `/v1/embeddings`.
- **v0.3** — Modelfile-equivalent build pipeline; LoRA adapter loading; per-request sampler chains exposed in the API.
- **v0.4** — Optional Unix socket; basic auth + token auth for non-loopback binds; metrics (`/metrics`, Prometheus).
- **v0.5** — Speculative decoding, grammar-constrained sampling, KV-cache reuse between requests with shared prefixes.
- **Later** — Distributed inference, model sharding, custom registry protocol.

### 10.3 Definition of Done for v0.1

- `go install github.com/baby-whales-pod/beeket/cmd/...@v0.1` produces working `beeket` and `beeketd`.
- `beeket pull smollm2:135m && beeket run smollm2:135m -p "Hello"` works on a fresh machine.
- All endpoints in §10.1 have integration tests against a real (tiny) GGUF model.
- README quickstart matches a 5-minute first-run experience.
- CI builds and tests on Linux/amd64, Linux/arm64, macOS/arm64, Windows/amd64.

---

## 11. Non-Functional Requirements

### 11.1 Performance Targets (v0.1)

These are guideposts, not hard SLAs. Numbers are for an idle machine, single request, after the model is loaded.

| Workload                              | Target (Apple M4 Max) | Target (NVIDIA RTX 4090) |
|---------------------------------------|-----------------------|--------------------------|
| 135M LLM, text generation             | ≥ 200 tok/s           | ≥ 400 tok/s              |
| 3B VLM, image + text                  | ≥ 400 tok/s           | ≥ 600 tok/s              |
| Embeddings (1k tokens, batch=8)       | ≥ 8 batches/s         | ≥ 20 batches/s           |
| Time-to-first-token after load (LLM)  | < 200 ms              | < 100 ms                 |
| HTTP overhead per request             | < 5 ms                | < 5 ms                   |

Yzma's published M4 Max benchmark for Qwen3-VL-2B (~700–900 tok/s) provides the upper bound we expect to approach for the 2B-class VLMs.

### 11.2 Resource Behavior

- Idle daemon (no model loaded) uses < 50 MB RSS.
- Loaded model RSS ≈ `gguf_size + KV_cache(context_size, n_parallel) + ~100 MB overhead`. Documented per model in `/api/show`.
- Graceful shutdown on `SIGINT`/`SIGTERM`: stop accepting new requests, wait up to 30 s for in-flight requests to drain, then unload models.

### 11.3 Platform Support

| OS       | Arch    | CPU | GPU backends (via Yzma)          | v0.1 |
|----------|---------|-----|----------------------------------|------|
| Linux    | x86_64  | ✅  | CUDA, Vulkan, ROCm, SYCL         | ✅   |
| Linux    | arm64   | ✅  | CUDA (Jetson), Vulkan            | ✅   |
| macOS    | arm64   | ✅  | Metal                            | ✅   |
| Windows  | x86_64  | ✅  | CUDA, Vulkan, OpenCL, SYCL       | ✅   |
| Raspberry Pi (32/64) | arm | ✅  | CPU only                | best-effort |

### 11.4 Reliability

- A panic in the engine never takes down `beeketd`. The worker goroutine recovers, fails the in-flight request with 500, and (if the panic is from FFI) unloads the model.
- File-store operations are crash-safe: downloads go through `tmp/` and are renamed atomically into `blobs/`.
- Manifests are written via write-temp-then-rename, never in place.

### 11.5 Security (minimum bar for v0.1)

- Default bind is loopback. Binding to non-loopback addresses requires an explicit `--host` flag and prints a warning.
- CORS off by default; configurable `server.origins`.
- No auth in v0.1 — documented limitation, with a roadmap entry for v0.4.

### 11.6 Observability

- `slog`-based structured logs with request IDs.
- Request log lines include: model, endpoint, prompt/eval token counts, total/eval/load durations, status.
- `/api/ps` exposes loaded models and last-used timestamps.
- Prometheus `/metrics` is **post-v0.1**.

---

## 12. Open Questions

Tracked separately so the spec can be merged without resolving them all:

1. Do we adopt Ollama's exact `:tag` semantics for quantization (`llama3:8b-instruct-q4_K_M`) or prefer the cleaner `repo:Q4_K_M` form? *Leaning: support both, prefer the second in docs.*
2. Should `beeket` autostart `beeketd` like `ollama` does? *Leaning: yes for `run`, no for everything else, to keep the daemon explicit.*
3. Default port: `11435` (to coexist with Ollama) vs `11434` (to be a true drop-in for clients that hardcode the port)? *Leaning: `11435` with `--ollama-compat-port` to switch.*
4. Ship a built-in installer for the `llama.cpp` shared library, or require users to run a Yzma command first? *Leaning: ship installer, gated by flag.*
5. How aggressive should LRU eviction be on memory-constrained systems? Needs measurement.

---

## 13. References

- Yzma — https://github.com/hybridgroup/yzma
- llama.cpp — https://github.com/ggml-org/llama.cpp
- GGUF spec — https://github.com/ggml-org/ggml/blob/master/docs/gguf.md
- Ollama API — https://github.com/ollama/ollama/blob/main/docs/api.md (compatibility reference)
- Hugging Face GGUF models — https://huggingface.co/models?library=gguf
