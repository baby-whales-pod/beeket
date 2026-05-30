# Beeket Code Map

> Generated 2026-05-30. Line numbers verified against current `main`.

---

## 1. Repository Tree

```
beeket/
├── cmd/
│   └── beeket/
│       └── main.go              Entry point: root Cobra command, serve/client sub-commands
├── internal/
│   ├── api/
│   │   ├── handlers.go          All HTTP handler functions (Pull, Generate, Chat, Embeddings, …)
│   │   ├── handlers_chat_tools_test.go   Tests for tool-calling flow in Chat handler
│   │   ├── handlers_embed_test.go        Tests for the Embeddings handler
│   │   ├── handlers_format_test.go       Tests for structured-output / format field
│   │   ├── handlers_nothink_test.go      Tests for /no_think injection logic
│   │   ├── ndjson.go            NDJSONWriter helper + writeJSON / writeError helpers
│   │   ├── server.go            Server struct, route registration, WrapWithMetrics
│   │   ├── status.go            GET /api/status handler and its response types
│   │   └── types.go             All public request/response structs for the REST API
│   ├── config/
│   │   ├── config.go            Config struct, Defaults(), Load(), ApplyEnv(), Validate()
│   │   └── config_test.go       Unit tests for config loading and env-override logic
│   ├── download/
│   │   ├── downloader.go        HTTP downloader with resume, SHA-256, Resolve() helper
│   │   └── downloader_test.go   Tests for URL resolution and download logic
│   ├── engine/
│   │   ├── embed_test.go        Integration tests for EmbedSession.Embed()
│   │   ├── engine.go            Engine / Model / Session / EmbedSession; all llama.cpp FFI
│   │   └── sampler_test.go      Unit tests for buildSampler chain construction
│   ├── jsongrammar/
│   │   ├── jsongrammar.go       JSONGrammar constant + ValidateSchema() using jsonschema/v5
│   │   └── jsongrammar_test.go  Tests for schema validation
│   ├── libinstall/
│   │   ├── detect.go            DetectBackend(): picks cpu/cuda/metal/vulkan/rocm
│   │   ├── libinstall.go        Ensure(): installs llama.cpp shared lib via yzma CLI
│   │   └── libinstall_test.go   Tests for Ensure() and backend detection
│   ├── metrics/
│   │   ├── metrics.go           All Prometheus collectors; Register(), SetBuildInfo(), StartUptimeTicker()
│   │   ├── middleware.go        Middleware(): HTTP instrumentation wrapper + request-ID injection
│   │   └── middleware_test.go   Tests for the metrics middleware
│   ├── models/
│   │   ├── alias.go             AliasTable, DefaultAliases(), built-in model shortcut entries
│   │   ├── clean.go             CleanModelRef(): normalises hf.co / HTTPS / short refs
│   │   ├── clean_test.go        Table-driven tests for CleanModelRef()
│   │   ├── gguf.go              GGUFSuffixRe regex + StripGGUFSuffix()
│   │   └── manager.go           Manager: Manifest struct, Get/Save/Delete/List, BlobPath
│   ├── scheduler/
│   │   └── scheduler.go         Scheduler + Worker + EmbedWorker; model loading / eviction / queuing
│   ├── store/
│   │   └── store.go             Store: on-disk layout, atomic blob writes, manifest CRUD
│   ├── tools/
│   │   ├── grammar.go           BuildGrammar(): GBNF grammar synthesis for tool calling
│   │   ├── grammar_test.go      Tests for BuildGrammar()
│   │   ├── parse.go             ParseToolCall(), CleanOutput(): parse tool JSON from model output
│   │   ├── parse_test.go        Tests for ParseToolCall()
│   │   ├── prompt.go            RenderToolPreface(), RewriteToolMessages()
│   │   ├── prompt_test.go       Tests for prompt rendering
│   │   └── types.go             Tool, ToolCall, ToolFunction, Message (tools-package local copies)
│   └── version/
│       └── version.go           Version / Commit / BuildDate vars; String() helper
└── pkg/
    └── client/
        └── client.go            HTTP client: Pull, List, Show, Delete, Generate, PS, Version
```

---

## 2. Package Dependency Graph (ASCII)

Arrows represent import dependencies (`A → B` means A imports B).

```
cmd/beeket
  ├── internal/config          (Config, Defaults, Load, ApplyEnv, Validate)
  ├── internal/libinstall      (Ensure, DetectBackend)
  ├── internal/version         (Version, Commit, BuildDate)
  ├── internal/metrics         (Register, SetBuildInfo, StartUptimeTicker)
  ├── internal/store           (Store)
  ├── internal/models          (Manager, Manifest)
  ├── internal/engine          (Engine, Session, EmbedSession)
  ├── internal/scheduler       (Scheduler, Worker, EmbedWorker)
  │    ├── internal/engine     (Model, Session, EmbedSession)
  │    ├── internal/models     (Manager, Manifest)
  │    └── internal/metrics    (ModelsLoaded, ModelLoadDuration, ModelEvictionsTotal)
  ├── internal/api             (Handler, Server, WrapWithMetrics)
  │    ├── internal/scheduler  (Scheduler)
  │    ├── internal/models     (Manager, Manifest)
  │    ├── internal/store      (Store)
  │    ├── internal/engine     (GenerateOptions, SamplerOptions)
  │    ├── internal/download   (Resolve, Get)
  │    ├── internal/tools      (BuildGrammar, ParseToolCall, RenderToolPreface, RewriteToolMessages)
  │    ├── internal/jsongrammar(JSONGrammar, ValidateSchema)
  │    ├── internal/metrics    (Middleware, InferenceRequestsTotal, …)
  │    └── internal/version    (Version)
  └── pkg/client               (Client — CLI sub-commands only)

internal/engine
  └── yzma/pkg/llama           (llama.cpp FFI: Model, Context, Sampler, Vocab, Tokenize, Decode, …)

internal/download
  └── internal/models          (StripGGUFSuffix — for filename guessing)
```

---

## 3. HTTP Endpoint → Handler → Engine Table

All routes are registered in `internal/api/server.go:routes()` (line 34).

| Method | Path | Handler (file:line) | Engine / Scheduler call (file:line) |
|--------|------|---------------------|--------------------------------------|
| `GET` | `/api/status` | `handlers.go:Status()` → `status.go:30` | `sched.LoadedModels()` — `scheduler.go:265` |
| `POST` | `/api/pull` | `handlers.go:Pull()` → `handlers.go:99` | `download.Resolve()` + `download.Get()` — `downloader.go:23,107` |
| `GET` | `/api/tags` | `handlers.go:Tags()` → `handlers.go:199` | `mgr.List()` — `manager.go:84` |
| `POST` | `/api/show` | `handlers.go:Show()` → `handlers.go:213` | `mgr.Get()` — `manager.go:71` |
| `DELETE` | `/api/delete` | `handlers.go:Delete()` → `handlers.go:232` | `mgr.Delete()` — `manager.go:79` |
| `POST` | `/api/copy` | `handlers.go:Copy()` → `handlers.go:247` | `mgr.Get()` + `mgr.Save()` — `manager.go:71,75` |
| `POST` | `/api/generate` | `handlers.go:Generate()` → `handlers.go:274` | `sched.Generate()` — `scheduler.go:107` |
| `POST` | `/api/chat` | `handlers.go:Chat()` → `handlers.go:389` | `sched.Generate()` — `scheduler.go:107` |
| `POST` | `/api/embeddings` | `handlers.go:Embeddings()` → `handlers.go:670` | `embedSched.Embed()` — `scheduler.go:332` |
| `POST` | `/api/embed` | `handlers.go:Embeddings()` → `handlers.go:670` | Same as `/api/embeddings` (Ollama alias) |
| `GET` | `/api/version` | `handlers.go:Version()` → `handlers.go:754` | `version.Version` (no engine call) |
| `GET` | `/api/ps` | `handlers.go:PS()` → `handlers.go:759` | `sched.LoadedModels()` — `scheduler.go:265` |
| `GET` | `/healthz` | `handlers.go:Healthz()` → `handlers.go:773` | none (always 200) |
| `GET` | `/readyz` | `handlers.go:Readyz()` → `handlers.go:779` | none (always 200) |
| `GET` | `/metrics` | `promhttp.Handler()` (registered conditionally) | Prometheus default registry |

---

## 4. Key Data Structures

| Struct | Package | File:Line | Description |
|--------|---------|-----------|-------------|
| `Config` | `internal/config` | `config.go:20` | Root config; embeds Server/Paths/Runtime/Download/Log sub-configs |
| `GenerateOptions` | `internal/engine` | `engine.go:226` | Per-request generation knobs: MaxTokens, StopStrings, GrammarStr, Grammar, GrammarLazy, Messages |
| `SamplerOptions` | `internal/engine` | `engine.go:99` | Temperature, TopK, TopP, MinP, TypicalP, Mirostat, repeat-penalties, Seed |
| `ChatRequest` | `internal/api` | `types.go:177` | Ollama `/api/chat` request body: Model, Messages, Tools, Format, Stream, Options |
| `ChatResponse` | `internal/api` | `types.go:188` | Chat response body: Model, Message, Done, timing fields |
| `EmbeddingsRequest` | `internal/api` | `types.go:200` | Embedding request: Model, Input (string or []string), Prompt (legacy) |
| `Manifest` | `internal/models` | `manager.go:26` | On-disk model record: Name, Tag, Digest, Size, Source, ModifiedAt, Details |
| `Session` | `internal/engine` | `engine.go:91` | llama.cpp inference context for one model: model, ctx, sampler, position |
| `EmbedSession` | `internal/engine` | `engine.go:402` | Dedicated embedding context: model, ctx, pooling type, nEmbd dimension |
| `Worker` | `internal/scheduler` | `scheduler.go:34` | Wraps Model+Session with request channel; serves requests sequentially |
| `EmbedWorker` | `internal/scheduler` | `scheduler.go:288` | Wraps Model+EmbedSession with embed-request channel |
| `Scheduler` | `internal/scheduler` | `scheduler.go:54` | Manages all Worker and EmbedWorker maps; enforces maxLoaded; runs eviction |
| `Handler` | `internal/api` | `handlers.go:49` | HTTP handler with sched, mgr, store dependencies; injectable interfaces for testing |
| `Server` | `internal/api` | `server.go:17` | Thin wrapper around `*http.ServeMux`; owns all route registrations |

---

## 5. "Where is X?" Quick Reference

| I want to… | Look in |
|------------|---------|
| Chat endpoint implementation | `internal/api/handlers.go:Chat()` (line 389) |
| Generate (single-turn) endpoint | `internal/api/handlers.go:Generate()` (line 274) |
| Token generation loop | `internal/engine/engine.go:Session.Generate()` (line 277) |
| Model loading from GGUF | `internal/engine/engine.go:Engine.LoadModel()` (line 62) |
| Inference context creation | `internal/engine/engine.go:Engine.NewSession()` (line 134) |
| Load or reuse a model worker | `internal/scheduler/scheduler.go:getOrLoadWorker()` (line 137) |
| Queue a generation request | `internal/scheduler/scheduler.go:Scheduler.Generate()` (line 107) |
| Embeddings handler | `internal/api/handlers.go:Embeddings()` (line 670) |
| Embed session creation | `internal/engine/engine.go:Engine.NewEmbedSession()` (line 413) |
| Embedding extraction & L2-norm | `internal/engine/engine.go:EmbedSession.Embed()` (line 444) |
| Tool call grammar synthesis | `internal/tools/grammar.go:BuildGrammar()` (line 27) |
| Tool call output parsing | `internal/tools/parse.go:ParseToolCall()` (line 15) |
| Tool preface prompt rendering | `internal/tools/prompt.go:RenderToolPreface()` (line 16) |
| JSON grammar constant (GBNF) | `internal/jsongrammar/jsongrammar.go:JSONGrammar` (line 17) |
| Schema validation post-generate | `internal/jsongrammar/jsongrammar.go:ValidateSchema()` (line 46) |
| Sampler chain construction | `internal/engine/engine.go:buildSampler()` (line 163) |
| Sampler chain with grammar/lazy-trigger | `internal/engine/engine.go:buildSamplerWithGrammar()` (line 518) |
| /no_think injection logic | `internal/api/handlers.go:injectNoThink()` (line 948) |
| Structured output / format field | `internal/api/handlers.go:resolveFormat()` (line 910) |
| Model pull (download) | `internal/api/handlers.go:Pull()` (line 99) |
| URL resolution for pull refs | `internal/download/downloader.go:Resolve()` (line 23) |
| HTTP download with resume + SHA-256 | `internal/download/downloader.go:Get()` (line 107) |
| CLI flags for `beeket serve` | `cmd/beeket/main.go:serveCmd()` (line ~192) |
| Config file loading | `internal/config/config.go:Load()` (line ~106) |
| Environment variable overrides | `internal/config/config.go:ApplyEnv()` (line ~121) |
| Prometheus metric definitions | `internal/metrics/metrics.go` (lines 17–82) |
| HTTP instrumentation middleware | `internal/metrics/middleware.go:Middleware()` (line ~87) |
| Library auto-install entry point | `internal/libinstall/libinstall.go:Ensure()` (line 56) |
| GPU/backend auto-detection | `internal/libinstall/detect.go:DetectBackend()` (line 33) |
| Built-in model alias table | `internal/models/alias.go:DefaultAliases()` (line 12) |
| Model manifest CRUD on disk | `internal/store/store.go:WriteManifest/ReadManifest` (lines 52, 76) |
| Model reference normalisation | `internal/models/clean.go:CleanModelRef()` (line 22) |
| GGUF suffix stripping | `internal/models/gguf.go:StripGGUFSuffix()` (line 21) |
| Chat template application | `internal/engine/engine.go:Session.ApplyChatTemplate()` (line 596) |
| Idle / LRU model eviction | `internal/scheduler/scheduler.go:evictionLoop()` (line 230) |
| HTTP client (CLI-side) | `pkg/client/client.go:Client` (line 16) |

---

## 6. Request Flow Diagrams

### 6a. Chat with Thinking Model (e.g. Qwen3)

```
Client
  │  POST /api/chat  { model, messages, stream:true }
  ▼
api.Handler.Chat()                         handlers.go:389
  │  resolve model name/tag via mgr.Resolve()
  │  detect think:false / /no_think → injectNoThink()    handlers.go:948
  │     └─ appends literal ` /no_think` token to last user message content
  │        (Qwen3 recognizes this token to suppress <think> blocks)
  │        + adds "</think>" safety-net stop string
  │  build engine.GenerateOptions {
  │     Messages: native ChatMessage slice,
  │     StopStrings, MaxTokens, Sampler, … }
  │  sched.Generate(name, tag, prompt="", opts)    scheduler.go:107
  ▼
scheduler.Scheduler.getOrLoadWorker()             scheduler.go:137
  │  load model if not in workers map
  │  engine.LoadModel() → llama.ModelLoadFromFile()
  │  engine.NewSession() → llama.InitFromModel()
  ▼
Worker.run() ← request enqueued to reqCh
  │  session.Generate(ctx, "", opts, out)           engine.go:277
  │    opts.Messages non-nil →
  │      session.ApplyChatTemplate(llamaMsgs)       engine.go:596
  │      → llama.ChatApplyTemplate() → prompt string
  │    build per-request sampler (no grammar)       engine.go:163
  │    tokenise prompt → llama.Tokenize()
  │    decode loop:
  │      llama.Decode() → sample token
  │      check EOG / stop strings / MaxTokens
  │      stream piece via out()
  ▼
NDJSONWriter.Write(ChatResponse{…})                ndjson.go:24
  └─ flushes each token chunk as NDJSON to client
```

### 6b. Structured Output (`format` field)

```
Client
  │  POST /api/chat  { model, messages, format: { "type": "object", … } }
  ▼
api.Handler.Chat()                                 handlers.go:389
  │  resolveFormat(req.Format)                     handlers.go:910
  │    if map → capture schema (grammar NOT applied in Chat)
  │    if "json" string → no schema
  │  injectNoThink() with withJSON=true            handlers.go:948
  │    ① appends literal ` /no_think` to last user message
  │    ② injects JSON-only system prompt            handlers.go:933
  │    ③ adds "</think>" safety-net stop string
  │  No grammar constraint — prompt-only approach
  │    (Grammar removed: SIGABRT from multi-char token {" issue;
  │     lazy trigger fires mid-token → 0 valid candidates → GGML_ABORT)
  │  opts.StopStrings += ["</think>", "<|im_end|>", "<|im_start|>"]
  │  opts.Messages = engMsgs  (engine calls ApplyChatTemplate)
  ▼
scheduler.Generate() → session.Generate()          engine.go:277
  │  opts.GrammarStr = ""  (empty — prompt guides JSON output)
  │  model outputs JSON guided by system prompt + /no_think
  ▼
handler collects full response string
  │  strip trailing stop string from output
  │  if schema provided → jsongrammar.ValidateSchema()  jsongrammar.go:46
  │    ├─ Valid   → HTTP 200, response.message.content = JSON
  │    └─ Invalid → HTTP 422 (UnprocessableEntity)
  └─ write ChatResponse with JSON content
```

### 6c. Tool Call

```
Client
  │  POST /api/chat  { model, messages, tools: […] }
  ▼
api.Handler.Chat()                                 handlers.go:389
  │  ① tools.RewriteToolMessages(messages)         prompt.go:45
  │       converts role="tool" → role="user" wrappers
  │  ② tools.RenderToolPreface(req.Tools)          prompt.go:16
  │       injects tool definitions into system message
  │       (prepended to existing system msg or inserted as new one)
  │  ③ appends ` /no_think` to last user message   handlers.go:~520
  │       (suppresses <think> preamble on Qwen3/QwQ models;
  │        NOT added to stop strings — model must close </think>
  │        before generating the tool call JSON)
  │  ④ No grammar constraint — prompt-only approach
  │       (SamplerInitGrammarLazyPatterns removed: lazy trigger fires
  │        on multi-char token like {" → 0 valid candidates → SIGABRT)
  │  opts.StopStrings += ["<|im_end|>"]
  │  opts.GrammarStr = ""  (empty)
  │  stream forced to false (tool calls always buffered)
  │  prompt = chatPrompt(effectiveMsgs)  (pre-built, no ApplyChatTemplate)
  ▼
scheduler.Generate() → session.Generate()          engine.go:277
  │  model generates tool call JSON guided by preface prompt
  ▼
handler collects full buffered output
  │  tools.ParseToolCall(output)                   parse.go:15
  │    scans for first balanced JSON matching
  │    {"name": "…", "arguments": {…}}
  │  if found:
  │    ChatResponse.Message.ToolCalls = [ToolCall]
  │    done_reason = "tool_calls"
  │  if not found:
  │    fall through → return as plain content
  └─ write ChatResponse (done=true)
```

### 6d. Embedding

```
Client
  │  POST /api/embeddings  { model, input: "text" }
  ▼
api.Handler.Embeddings()                           handlers.go:670
  │  normalise input (string / []string / legacy prompt)
  │  resolve name, tag via embedMgr.Resolve()
  │  for each input text:
  │    embedSched.Embed(ctx, name, tag, text)       scheduler.go:332
  ▼
scheduler.Scheduler.getOrLoadEmbedWorker()         scheduler.go:360
  │  if not in embedWorkers map:
  │    engine.LoadModel() → llama.ModelLoadFromFile()
  │    engine.NewEmbedSession():
  │      llama.InitFromModel(cp)
  │      llama.SetEmbeddings(ctx, true)   (post-init)
  ▼
EmbedWorker.run() ← embedRequest enqueued
  │  session.Embed(ctx, text)                      engine.go:444
  │    llama.Tokenize()
  │    llama.Decode()  (decoder-style model)
  │    llama.GetEmbeddingsSeq(ctx, 0, nEmbd)
  │    copy out of FFI memory
  │    l2Normalize(vec)                            engine.go:486
  │    return vec, nTokens
  ▼
handler accumulates [][]float32
  └─ writeJSON(EmbeddingsResponse{ Embeddings, … })
```

---

## 7. Test Coverage Map

| Package | Test File(s) | What Is Covered |
|---------|-------------|-----------------|
| `internal/api` | `handlers_chat_tools_test.go` | Chat handler with tool definitions; grammar injection; ParseToolCall integration; tool result message rewriting |
| `internal/api` | `handlers_embed_test.go` | Embeddings handler: string input, array input, legacy `prompt` field, empty-input errors |
| `internal/api` | `handlers_format_test.go` | `resolveFormat()`: JSON string, schema map, invalid values; structured-output Chat flow |
| `internal/api` | `handlers_nothink_test.go` | `injectNoThink()`: /no_think prefix detection, system prompt injection, JSON mode combination |
| `internal/config` | `config_test.go` | Defaults(), Load() from TOML file, ApplyEnv() overrides, Validate() error cases |
| `internal/download` | `downloader_test.go` | `Resolve()` for hf.co / HTTPS / file:// refs; `TmpFilename()` derivation; `Get()` with mock server |
| `internal/engine` | `sampler_test.go` | `buildSampler()` chain construction: standard path, Mirostat v1/v2, grammar injection, repeat penalties |
| `internal/engine` | `embed_test.go` | `EmbedSession.Embed()`: integration test against a real GGUF embed model (skipped if no model present) |
| `internal/jsongrammar` | `jsongrammar_test.go` | `ValidateSchema()`: passing schemas, failing schemas, invalid JSON input |
| `internal/libinstall` | `libinstall_test.go` | `Ensure()` fast-path (lib already present), `detectBackend()` with stubbed GPU probes for all GOOS/GOARCH combos |
| `internal/metrics` | `middleware_test.go` | `Middleware()`: request counter increments, duration recording, in-flight tracking, /metrics exclusion, X-Request-Id propagation |
| `internal/models` | `clean_test.go` | `CleanModelRef()`: table-driven tests covering hf.co shortcuts, HTTPS URLs, bare names, colon tags |
| `internal/tools` | `grammar_test.go` | `BuildGrammar()`: single/multi-tool, required/optional fields, enum types, collision detection |
| `internal/tools` | `parse_test.go` | `ParseToolCall()`: valid objects, leading prose, nested JSON, missing fields, malformed input |
| `internal/tools` | `prompt_test.go` | `RenderToolPreface()` output format; `RewriteToolMessages()` role rewriting |
| `internal/scheduler` | _(none — integration only)_ | No unit tests; covered indirectly via API handler tests that inject mock schedulers |
| `internal/store` | _(none)_ | No dedicated tests; exercised transitively by download/manager tests |
| `internal/version` | _(none)_ | Trivial — only build-time variable assignment |
| `pkg/client` | _(none)_ | No unit tests; tested end-to-end via CLI integration |
