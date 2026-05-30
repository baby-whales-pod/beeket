# Beeket — Architecture

> 🇫🇷 French version: [architecture-fr.md](./architecture-fr.md)

---

## 1. Overview

Beeket is an [Ollama](https://ollama.com)-compatible local LLM server written in Go.
It serves a REST API that lets clients pull, manage, and run GGUF-format language models
using [llama.cpp](https://github.com/ggml-org/llama.cpp) as the inference engine.
The llama.cpp C++ library is accessed through
[Yzma](https://github.com/hybridgroup/yzma) (`hybridgroup/yzma`), a thin Go CGo wrapper
that exposes llama.cpp's C API without bundling the shared library inside the binary.

The codebase is a **single binary** (`beeket`) that doubles as a server (`beeket serve`)
and a management CLI (`beeket pull`, `list`, `show`, `rm`, `run`, `ps`). The binary is
kept small by design: the llama.cpp shared library (`.so`/`.dylib`/`.dll`) is loaded at
runtime from a user-supplied directory or auto-installed via `--auto-install-lib`.

The internal package structure reflects three clean separation-of-concerns:

- **One FFI boundary** — only `internal/engine` talks to Yzma/llama.cpp.
- **One concurrency layer** — only `internal/scheduler` manages goroutines and
  model loading.
- **One persistence layer** — only `internal/store` knows the on-disk layout;
  `internal/models` provides the logical registry on top.

---

## 2. High-Level Request Flow

```
HTTP Client
    │
    ▼
┌─────────────────────────────────────────────────┐
│  api.Server  (internal/api/server.go)           │
│  - HTTP mux (Go 1.22 method+path routing)       │
│  - metrics.Middleware (Prometheus counters)      │
│  - request logging (debug)                      │
└──────────────────────┬──────────────────────────┘
                       │
            ┌──────────▼──────────┐
            │   api.Handler       │  internal/api/handlers.go
            │                     │
            │  - injectNoThink    │  ← /no_think injection for thinking models
            │  - resolveFormat    │  ← JSON / JSON Schema grammar selection
            │  - tool rewriting   │  ← system-prompt injection, role rewriting
            │  - buildChatPrompt  │  ← ChatML / native chat template
            └──────┬──────────────┘
                   │
       ┌───────────▼────────────┐    ┌──────────────────────┐
       │  scheduler.Scheduler   │◄──►│  models.Manager       │
       │  (internal/scheduler)  │    │  (internal/models)    │
       │                        │    │                        │
       │  - Worker pool         │    │  - Resolve name:tag   │
       │  - request queue (32)  │    │  - Get/Save manifest   │
       │  - LRU eviction        │    │  - AliasLookup        │
       │  - keep-alive TTL      │    └───────────────────────┘
       │  - EmbedWorker pool    │
       └──────────┬─────────────┘
                  │
       ┌──────────▼─────────────┐    ┌──────────────────────┐
       │  engine.Session        │    │  engine.EmbedSession  │
       │  (internal/engine)     │    │  (internal/engine)    │
       │                        │    │                        │
       │  - llama.cpp via Yzma  │    │  - L2-normalised vecs │
       │  - sampler chain       │    │  - GetEmbeddingsSeq   │
       │  - grammar samplers    │    └──────────────────────-┘
       │  - chat template       │
       └──────────┬─────────────┘
                  │
       ┌──────────▼─────────────┐
       │  llama.cpp (.so/.dylib)│
       │  loaded via Yzma FFI   │
       └────────────────────────┘

Cross-cutting concerns:
  internal/metrics     — Prometheus middleware + collectors on every request
  internal/config      — flags → env vars → TOML config, applied at startup
  internal/libinstall  — optional auto-download of the llama.cpp shared library
  internal/tools       — GBNF grammar build, system-prompt injection, JSON parsing
  internal/jsongrammar — canonical JSON GBNF for structured output (format: "json")
  internal/download    — resumable HTTPS downloader (model pull)
  internal/store       — on-disk blobs + manifests ($XDG_DATA_HOME/beeket)
```

---

## 3. Package Responsibility Table

| Package | Path | Key Types | Responsibility |
|---|---|---|---|
| `cmd/beeket` | `cmd/beeket/main.go` | — | Single binary entry point. Wires `config → libinstall → store → engine → models → scheduler → api`. Provides `serve` and all client subcommands (`pull`, `list`, `show`, `rm`, `run`, `ps`). |
| `internal/api` | `internal/api/` | `Server`, `Handler`, `HandlerConfig` | Ollama-compatible HTTP API. Routes, NDJSON streaming, chat-prompt building, tool-call interception, structured-output grammar selection, metrics recording. |
| `internal/engine` | `internal/engine/engine.go` | `Engine`, `Model`, `Session`, `EmbedSession`, `SamplerOptions`, `GenerateOptions` | Single FFI boundary to Yzma/llama.cpp. Library lifecycle, model loading, inference sessions, sampler chain construction, chat template rendering, embedding extraction. |
| `internal/scheduler` | `internal/scheduler/scheduler.go` | `Scheduler`, `Worker`, `EmbedWorker`, `Config`, `LoadedInfo` | Concurrency layer. One `Worker` goroutine per loaded model with a 32-slot request channel. Enforces `MaxLoaded`, LRU eviction, keep-alive idle eviction. Exposes `Generate`, `Embed`, `LoadedModels`. |
| `internal/models` | `internal/models/` | `Manager`, `Manifest`, `Details` | Logical model registry on top of `store`. Resolves `name[:tag]` references, reads/writes manifests, handles alias table, inspects GGUF metadata. |
| `internal/download` | `internal/download/` | `Get`, `Resolve`, `TmpFilename` | Resumable HTTPS downloader for `.gguf` blobs. Emits progress callbacks consumed by `api.Pull`. Resolves HuggingFace shorthand URLs. |
| `internal/store` | `internal/store/store.go` | `Store` | Content-addressed on-disk store. Owns blob writes, manifest JSON, deletion. The only package that knows the on-disk layout under `$XDG_DATA_HOME/beeket`. |
| `internal/metrics` | `internal/metrics/` | `Middleware`, `Register`, `InferenceRequestsTotal`, `InferenceDuration` | Prometheus registry: build info, uptime, request counters, latency histograms, token-throughput counters, loaded-model gauge. HTTP middleware wraps every response. |
| `internal/tools` | `internal/tools/` | `Tool`, `ToolCall`, `RenderToolPreface`, `RewriteToolMessages`, `ParseToolCall` | Tool-calling pipeline: builds system-prompt preamble from tool schemas, rewrites `tool`-role messages, parses model JSON output into `ToolCall` structs. |
| `internal/jsongrammar` | `internal/jsongrammar/jsongrammar.go` | `JSONGrammar`, `ValidateSchema` | Single source of truth for the canonical JSON GBNF grammar used in `format: "json"` requests. Also validates response JSON against a JSON Schema after generation. |
| `internal/config` | `internal/config/config.go` | `Config`, `Load`, `ApplyEnv`, `Validate` | Config schema, TOML loader, env-var overlay (`BEEKET_*`), CLI-flag overlay, validation, and XDG path resolution. Priority order: defaults → TOML → env → flags. |
| `internal/libinstall` | `internal/libinstall/` | `Ensure`, `Options` | Optional `--auto-install-lib`: detects platform/backend (cpu/cuda/metal/vulkan/rocm), downloads the matching llama.cpp shared library via Yzma into `lib-dir`. |
| `internal/version` | `internal/version/version.go` | `Version`, `Commit`, `BuildDate` | Build-time version variables populated via `-ldflags`. |
| `pkg/client` | `pkg/client/` | `Client` | Public Go client over the HTTP API. Importable by third parties; used internally by CLI subcommands. |

---

## 4. Data Flow Walkthroughs

### 4.1 Chat Request (`POST /api/chat`)

1. **`api.Handler.Chat`** decodes the `ChatRequest` JSON from the HTTP body.
2. If `tools[]` is present, `tools.RewriteToolMessages` converts `tool`-role
   messages to `user` role (yzma's template does not know the `tool` role), and
   `tools.RenderToolPreface` prepends a structured instruction to the system message.
3. `resolveFormat(req.Format)` is called: returns the canonical `jsongrammar.JSONGrammar`
   string and (for JSON Schema objects) the schema map for post-generation validation.
4. If `think: false` or structured output is requested, `injectNoThink` appends
   `/no_think` to the last user message (Qwen3 requirement) and, for JSON mode,
   prepends a JSON-only system prompt. A `</think>` stop string is also added.
5. `buildChatPrompt` applies ChatML (or the model's native template via
   `engine.Session.ApplyChatTemplate`) to produce the prompt string.
6. **`scheduler.Scheduler.Generate`** looks up (or loads) the `Worker` for the
   requested `name:tag` and enqueues a `Request` on the worker's 32-slot channel.
7. **`Worker.run`** dequeues the request and calls **`engine.Session.Generate`**,
   which tokenises the prompt, runs the decode loop, and streams tokens to the
   `out` callback.
8. Each token triggers the `out` callback in `Handler.Chat`, which either streams
   an NDJSON `ChatResponse` chunk or appends to a buffer (non-streaming / tools).
9. After generation: stop-string trimming, optional JSON schema validation
   (`jsongrammar.ValidateSchema`), tool-call parsing (`tools.ParseToolCall`),
   and Prometheus metric recording.
10. The final `ChatResponse` (done: true) is written to the HTTP response.

### 4.2 Embedding Request (`POST /api/embeddings` or `/api/embed`)

1. **`api.Handler.Embeddings`** normalises the `input` field to `[]string`
   (supports string, `[]string`, and legacy `prompt` field for Ollama compat).
2. For each input string, **`scheduler.Scheduler.Embed`** looks up (or loads)
   the `EmbedWorker` for the model. Embed workers are keyed as `name:tag#embed`
   and tracked in a separate map so they don't displace generation workers.
3. **`EmbedWorker.run`** passes the text to **`engine.EmbedSession.Embed`**, which:
   - tokenises the text with `llama.Tokenize`;
   - runs `llama.Decode` on the token batch;
   - reads the pooled embedding vector via `llama.GetEmbeddingsSeq`;
   - copies the vector out of FFI memory and L2-normalises it.
4. The handler collects all per-input vectors and token counts, records Prometheus
   metrics, and writes a single `EmbeddingsResponse` JSON object.

### 4.3 Tool Call

1. The client sends `POST /api/chat` with a `tools[]` array.
2. `Handler.Chat` converts each tool to a `tools.Tool` and calls
   `tools.RenderToolPreface(toolsList)` — this produces a compact textual
   description of all tools with their parameters.
3. The preface is prepended to the system message. `/no_think` is appended to
   the last user message to suppress chain-of-thought.
4. The prompt is built using the existing ChatML template (not the native engine
   template, because tool definitions are injected into the pre-built prompt).
5. Generation runs without a grammar sampler (grammar was disabled due to a
   llama.cpp SIGABRT issue with lazy-trigger grammars on multi-character tokens).
   The model is guided to JSON output solely via the preface prompt and `/no_think`.
6. After generation, `tools.ParseToolCall(output)` scans for the first balanced
   JSON object matching `{"name": "...", "arguments": {...}}`.
7. If found, the response carries `tool_calls` and `done_reason: "tool_calls"`.
   If not found, the response is returned as plain content.

### 4.4 Structured Output (`format: "json"` or JSON Schema)

1. The client sends `POST /api/chat` (or `/api/generate`) with `"format": "json"`
   or `"format": <JSON Schema object>`.
2. `resolveFormat` returns `jsongrammar.JSONGrammar` (always the canonical
   `json.gbnf` grammar from llama.cpp) and, for schema objects, the schema map.
3. `injectNoThink` appends `/no_think` to the last user message and injects
   the JSON-only system prompt: *"Respond ONLY with a valid JSON object…"*.
   `</think>` and end-of-turn tokens (`<|im_end|>`) are added as stop strings.
4. The engine currently relies on prompt engineering rather than grammar-sampler
   constraints (grammar was removed due to SIGABRT on empty NFA states). The
   system prompt + `/no_think` guide the model to produce valid JSON.
5. After generation, if a JSON Schema was provided, `jsongrammar.ValidateSchema`
   validates the response. On mismatch, HTTP 422 is returned.

### 4.5 Model Pull (`POST /api/pull`)

1. **`api.Handler.Pull`** decodes the `PullRequest` (model name/ref).
2. `models.Manager.Resolve` normalises the ref to a `(name, tag)` registry key.
   `AliasLookup` checks the built-in alias table; otherwise `download.Resolve`
   constructs the HTTPS download URL (handles HuggingFace shorthand refs).
3. **`download.Get`** streams the GGUF file to a temp path under
   `$data-dir/tmp/`, computing the SHA-256 digest on the fly and emitting
   progress callbacks. The handler streams NDJSON progress lines to the client.
4. Once complete, the blob is moved atomically (via `os.Rename`) to
   `$data-dir/blobs/sha256-<digest>` (content-addressed storage).
5. **`models.Manager.Save`** writes a JSON manifest to
   `$data-dir/manifests/<name>/<tag>.json` with the digest, size, source URL,
   and any GGUF-extracted metadata.
6. A final NDJSON line `{"status": "success", "digest": "sha256:<digest>"}` is
   sent to the client.

---

## 5. Configuration Reference

Configuration is layered: compiled-in defaults are overridden by the TOML file,
then environment variables, then CLI flags. Only explicitly-set CLI flags override
lower-priority sources.

| CLI Flag | Environment Variable | TOML Key | Default | Description |
|---|---|---|---|---|
| `--host` | `BEEKET_HOST` | `[server] host` | `127.0.0.1` | HTTP bind address |
| `--port` | `BEEKET_PORT` | `[server] port` | `11435` | HTTP listen port |
| `--data-dir` | `BEEKET_DATA_DIR` | `[paths] data_dir` | `$XDG_DATA_HOME/beeket` | Root data directory (blobs, manifests) |
| `--lib-dir` | `BEEKET_LIB_DIR` | `[paths] lib_dir` | `<data-dir>/lib` | Directory containing the llama.cpp shared library |
| `--backend` | `BEEKET_BACKEND` | `[runtime] backend` | `auto` | Compute backend: `auto`, `cpu`, `cuda`, `metal`, `vulkan`, `rocm` |
| `--gpu-layers` | `BEEKET_GPU_LAYERS` | `[runtime] gpu_layers` | `-1` (all) | GPU layers to offload; `-1` means all layers |
| `--num-parallel` | `BEEKET_NUM_PARALLEL` | `[runtime] num_parallel` | `1` | Parallel inference slots per model |
| `--max-loaded-models` | `BEEKET_MAX_LOADED_MODELS` | `[runtime] max_loaded` | `3` | Maximum number of models held in memory simultaneously |
| `--keep-alive` | `BEEKET_KEEP_ALIVE` | `[runtime] keep_alive` | `5m` | Model idle timeout (Go duration string, e.g. `5m`, `1h`) |
| `--context-size` | `BEEKET_CONTEXT_SIZE` | `[runtime] context_size` | `4096` | Default context window in tokens |
| `--auto-install-lib` | `BEEKET_AUTO_INSTALL_LIB` | `[runtime] auto_install_lib` | `false` | Download llama.cpp shared library automatically at startup |
| `--lib-version` | _(none)_ | `[runtime] lib_version` | latest | llama.cpp version to install (only with `--auto-install-lib`) |
| `--lib-upgrade` | _(none)_ | `[runtime] lib_upgrade` | `false` | Force reinstall even if library already present |
| `--lib-install-timeout` | _(none)_ | _(none)_ | `10m` | Maximum time to wait for library download |
| `--metrics-enabled` | `BEEKET_METRICS_ENABLED` | `[runtime] metrics_enabled` | `true` | Expose Prometheus metrics at `/metrics` |
| `--metrics-bind` | `BEEKET_METRICS_BIND` | `[runtime] metrics_bind` | _(disabled)_ | Secondary address for `/metrics` (e.g. `0.0.0.0:11436`) |
| `--log-level` | `BEEKET_LOG_LEVEL` | `[log] level` | `info` | Log verbosity: `debug`, `info`, `warn`, `error` |
| `--log-format` | `BEEKET_LOG_FORMAT` | `[log] format` | `text` | Log format: `text` (human) or `json` (structured) |
| `--config` | _(none)_ | _(n/a)_ | `~/.config/beeket/beeket.toml` | Path to the TOML config file |

**Library directory resolution order** (for `--lib-dir`):

1. `--lib-dir` flag / `[paths] lib_dir` TOML key
2. `BEEKET_LIB_DIR` environment variable
3. `YZMA_LIB` environment variable
4. `<data-dir>/lib`

**TOML config file example:**

```toml
[server]
host = "0.0.0.0"
port = 11435

[paths]
data_dir = "/var/lib/beeket"

[runtime]
backend       = "cuda"
gpu_layers    = -1
num_parallel  = 4
max_loaded    = 5
keep_alive    = "10m"
context_size  = 8192
metrics_enabled = true
metrics_bind  = "0.0.0.0:11436"

[log]
level  = "info"
format = "json"
```

---

## 6. Thinking Model Support

Beeket supports "thinking" models (e.g. Qwen3, QwQ, DeepSeek-R1) that emit
chain-of-thought reasoning inside `<think>…</think>` blocks before the final answer.

### 6.1 The `/no_think` mechanism

Per the [Qwen3 documentation](https://qwen.readthedocs.io/), the control token
`/no_think` must be **appended to the last user message** to suppress the thinking
preamble. Beeket implements this in two code paths:

- **`api.Handler.Generate`** — appends `/no_think` to `req.Prompt` when
  `think: false` or a structured-output format is requested.
- **`api.Handler.Chat`** → **`injectNoThink`** — scans `effectiveMsgs` from the
  end, finds the last `user`-role message, and appends `/no_think` if not already
  present. Guards against double injection with a `strings.HasSuffix` check.

### 6.2 When thinking is suppressed

Thinking suppression is triggered automatically in three situations:

| Trigger | Condition |
|---|---|
| Explicit opt-out | `"think": false` in the request body |
| Structured output | `"format": "json"` or `"format": <JSON Schema>` |
| Tool calling | `"tools": [...]` present in the request body |

### 6.3 Safety net

Even with `/no_think`, a model might still emit a `<think>` block. Beeket adds
`"</think>"` as a stop string whenever thinking is suppressed, so generation halts
immediately if a thinking block appears. For structured output, end-of-turn tokens
(`<|im_end|>`, `<|im_start|>`) are also added as stop strings to prevent context leakage.

### 6.4 Native chat template support

For non-tool-call requests, `engine.Session.Generate` accepts an `opts.Messages`
slice and calls `llama.ChatApplyTemplate` with the **model's own template name**
(read from the GGUF header). This ensures thinking-model-specific template
variables (e.g. `enable_thinking`) are rendered correctly.

---

## 7. Extension Points

Beeket is designed so that common extensions require changes in only one or two packages.

### 7.1 Adding a new HTTP endpoint

1. Add a handler method on `api.Handler` in `internal/api/handlers.go`:
   ```go
   func (h *Handler) MyEndpoint(w http.ResponseWriter, r *http.Request) { … }
   ```
2. Register the route in `api.NewServer` (`internal/api/server.go`):
   ```go
   s.mux.HandleFunc("POST /api/my-endpoint", h.MyEndpoint)
   ```
   The server uses Go 1.22's `method path` pattern syntax.

### 7.2 Replacing the scheduler (alternate concurrency model)

`api.Handler` depends on two small interfaces, not on the concrete `*scheduler.Scheduler`:

```go
type generatorScheduler interface {
    Generate(ctx context.Context, name, tag, prompt string,
             opts engine.GenerateOptions, out func(string) error) error
    LoadedModels() []scheduler.LoadedInfo
}

type embedScheduler interface {
    Embed(ctx context.Context, name, tag, input string) ([]float32, int, error)
}
```

Provide a struct that satisfies both and pass it to `api.NewHandlerWithConfig`.

### 7.3 Adding a new sampler option

1. Add the field to `engine.SamplerOptions` in `internal/engine/engine.go`.
2. Wire it in `buildSampler` (and `buildSamplerWithGrammar`) by calling the
   appropriate `llama.SamplerChainAdd` function.
3. Expose it in the `Options` request type in `internal/api/types.go`.
4. Map it in `buildGenerateOptions` in `internal/api/handlers.go`.
5. Document it in `docs/options.md`.

### 7.4 Adding a new structured-output grammar

1. Create a new package alongside `internal/jsongrammar/` with your grammar constant.
2. Extend `resolveFormat` in `internal/api/handlers.go` to recognise a new
   `format` value and return your grammar string.
3. If post-generation validation is needed, add a `Validate` function similar
   to `jsongrammar.ValidateSchema`.

### 7.5 Adding a new tool-call dialect

The tool pipeline is split into three functions in `internal/tools/`:

- `RenderToolPreface(tools)` — builds the system-prompt injection.
- `RewriteToolMessages(messages)` — normalises `tool`-role messages.
- `ParseToolCall(output)` — extracts the structured JSON from model output.

Replace any of these individually to change how tools are presented to the model
or how the response is interpreted.

### 7.6 Adding a new compute backend

1. Extend `libinstall.Ensure` in `internal/libinstall/` to detect and download the
   new backend's shared library.
2. Add the backend name to `config.Validate`'s `validBackends` map.
3. The `internal/engine` package is entirely backend-agnostic — it passes the
   library path to `llama.Load` and lets Yzma/llama.cpp select the backend.

---

## 8. Glossary

| Term | Definition |
|---|---|
| **GGUF** | *GGML Unified Format* — the file format used by llama.cpp to store quantised model weights, vocabulary, and metadata (including chat templates) in a single binary file. |
| **GBNF** | *GGML BNF* — a BNF-like grammar format understood by llama.cpp's grammar sampler. Used to constrain token generation to valid productions (e.g. JSON objects). |
| **Sampler** | The component that selects the next token from the probability distribution produced by the model. Beeket supports chains of samplers: TopK → TopP / TypicalP → MinP → Temperature → (Grammar) → Distribution. |
| **Context / KV cache** | The key-value cache that llama.cpp maintains for each inference session. Stores the attention keys and values for all previously processed tokens. Size is set by `--context-size`. |
| **Session** | An `engine.Session` is one llama.cpp inference context tied to a loaded model. Beeket creates one session per `Worker`; each request reuses the same session (the context/KV cache resets between unrelated requests). |
| **Worker** | A `scheduler.Worker` owns one `engine.Session` and one goroutine. It processes inference requests sequentially from a 32-slot channel. |
| **EmbedWorker** | A `scheduler.EmbedWorker` owns one `engine.EmbedSession` dedicated to embedding extraction. Keyed separately from generation workers to avoid displacing them. |
| **Keep-alive** | The idle timeout after which an unused model is evicted from memory. Configurable via `--keep-alive` (default `5m`). An eviction loop runs every 30 seconds. |
| **Manifest** | A small JSON file (`$data-dir/manifests/<name>/<tag>.json`) that records a model's digest, size, source URL, and metadata. The on-disk equivalent of a registry tag. |
| **Blob** | The raw GGUF file stored content-addressed at `$data-dir/blobs/sha256-<hex-digest>`. |
| **Backend** | The hardware acceleration backend used by llama.cpp: `cpu`, `cuda`, `metal`, `vulkan`, or `rocm`. Selected at startup; `auto` lets Yzma detect the best available option. |
| **Pooling** | The strategy used to aggregate per-token embeddings into a single vector for a sequence. Beeket uses `GetEmbeddingsSeq` (sequence pooling) rather than per-token embeddings. |
| **L2 normalisation** | Dividing an embedding vector by its Euclidean (L2) norm so that all vectors lie on the unit sphere. Enables cosine similarity to be computed as a simple dot product. |
| **Yzma** | The Go CGo wrapper (`hybridgroup/yzma`) that Beeket uses to call llama.cpp's C API. Yzma loads the shared library at runtime via `llama.Load` rather than linking it statically. |

---

## 9. Cross-References

| Topic | Document |
|---|---|
| API specification (routes, request/response shapes) | [spec-v0.1.md](./spec-v0.1.md) |
| Setup and installation | [SETUP.md](./SETUP.md) |
| Pulling models (URL formats, HuggingFace refs) | [models.md](./models.md) |
| Sampling options reference | [options.md](./options.md) |
| Structured output guide | [structured-output.md](./structured-output.md) |
| Embeddings guide | [embeddings.md](./embeddings.md) |
| Tool calling guide | [tools.md](./tools.md) |
| Monitoring and metrics | [monitoring.md](./monitoring.md) |
