# Contributing to Beeket

Practical playbook for reading, modifying, fixing, and extending the codebase.
Architecture and API spec live in [docs/spec-v0.1.md](./spec-v0.1.md) — read that first
for the big-picture layering and dependency rules.

> **Line numbers** in this document are accurate as of the time it was written.
> Search by function name if a reference drifts after a refactor.

---

## Table of contents

1. [Getting started](#1-getting-started)
2. [How to add a new API endpoint](#2-how-to-add-a-new-api-endpoint)
3. [How to fix a bug](#3-how-to-fix-a-bug)
4. [How to add a new sampler option](#4-how-to-add-a-new-sampler-option)
5. [How to support a new model type](#5-how-to-support-a-new-model-type)
6. [How to add a new CLI flag](#6-how-to-add-a-new-cli-flag)
7. [Testing](#7-testing)
8. [Common pitfalls](#8-common-pitfalls)
9. [PR process](#9-pr-process)

---

## 1. Getting started

See [docs/SETUP.md](./SETUP.md) for the full installation and configuration guide
(library install, GPU back-ends, config file format).

**Minimum dev-loop:**

```bash
# Build everything
go build ./...

# Run all tests
go test ./...

# Run tests with race detector (required before pushing)
go test -race ./...

# Vet
go vet ./...

# Format check
gofmt -l ./...

# Apply formatting
gofmt -w ./...
```

**Run the server against a local model directory:**

```bash
beeket serve \
  --data-dir ~/.local/share/beeket \
  --log-level debug \
  --port 11435
```

Logs are written to **stderr** via `log/slog`. The handler is set up in
`cmd/beeket/main.go:newLogger` (~line 442). Use `--log-level debug` to see
per-request HTTP logs, scheduler load/evict events, and engine token-loop
tracing.

**Quick smoke test:**

```bash
curl -s http://127.0.0.1:11435/api/version | jq .
```

---

## 2. How to add a new API endpoint

Worked example: `POST /api/tokenize` — returns the token IDs for a prompt.

### Step 1 — Add types in `internal/api/types.go`

```go
// TokenizeRequest is the body for POST /api/tokenize.
type TokenizeRequest struct {
    Model  string `json:"model"`
    Prompt string `json:"prompt"`
}

// TokenizeResponse is returned by POST /api/tokenize.
type TokenizeResponse struct {
    Tokens []int `json:"tokens"`
}
```

Keep field names and `json` tags consistent with the Ollama wire format where
applicable. DTOs live entirely in `types.go`; no logic goes here.

### Step 2 — Add a handler method in `internal/api/handlers.go`

Add a method on `*Handler`. Follow the `Generate` handler (~line 274) as the
canonical template:

```go
// Tokenize handles POST /api/tokenize.
func (h *Handler) Tokenize(w http.ResponseWriter, r *http.Request) {
    var req TokenizeRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        writeError(w, http.StatusBadRequest, "invalid request body")
        return
    }
    if req.Model == "" {
        writeError(w, http.StatusBadRequest, "model is required")
        return
    }

    name, tag := h.mgr.Resolve(req.Model)
    tokens, err := h.sched.Tokenize(r.Context(), name, tag, req.Prompt)
    if err != nil {
        writeError(w, http.StatusInternalServerError, err.Error())
        return
    }
    writeJSON(w, http.StatusOK, TokenizeResponse{Tokens: tokens})
}
```

Key rules:
- Decode → validate → call scheduler (never call engine directly from a handler).
- Use `writeError` / `writeJSON` helpers (already in `handlers.go`).
- Streaming responses use `NewNDJSONWriter` — see `Generate` / `Chat` for the pattern.

### Step 3 — Register the route in `internal/api/server.go`

Add one line inside `routes()` (around line 38):

```go
s.mux.HandleFunc("POST /api/tokenize", h.Tokenize)
```

Go 1.22+ pattern syntax (`METHOD /path`) is used throughout — keep it consistent.

### Step 4 — Add a scheduler method (if needed)

If the operation needs engine access, route it through `internal/scheduler/scheduler.go`.
The scheduler owns all model loading, worker dispatch, and LRU eviction. **Do not
call `engine.*` directly from a handler** — this is the key dependency rule from
spec-v0.1.md §2.4.

Add a method on `*Scheduler` that acquires a worker and calls the engine:

```go
func (s *Scheduler) Tokenize(ctx context.Context, name, tag, text string) ([]int, error) {
    w, err := s.getOrLoadWorker(ctx, name, tag)
    if err != nil {
        return nil, err
    }
    return w.model.Tokenize(text), nil
}
```

### Step 5 — Add an engine method (if needed)

If the scheduler method requires a new capability, add it to
`internal/engine/engine.go`. Keep all `llama.*` FFI calls confined to this
package.

### Step 6 — Write tests

Add a `*_test.go` file next to the handler, following the table-driven HTTP
test pattern in `internal/api/handlers_nothink_test.go` or the format test
in `internal/api/handlers_format_test.go`.

The handler tests use `httptest.NewRecorder()` and inject a fake scheduler
via the interface — no real model is required. See the `generatorScheduler`
interface in `handlers.go` (~line 37) for the injection point.

### Step 7 — Update `docs/`

- If the route is part of the public API, add it to `docs/spec-v0.1.md`.
- Update `docs/code-map.md` endpoint table if it exists.
- Update `pkg/client/client.go` if the endpoint should be callable from the
  Go SDK.

---

## 3. How to fix a bug

### Reproduce first

```bash
# Hit the endpoint directly
curl -s -X POST http://127.0.0.1:11435/api/chat \
  -H 'Content-Type: application/json' \
  -d '{"model":"qwen2.5:0.5b","messages":[{"role":"user","content":"hello"}]}' | jq .
```

### Enable debug logging

```bash
beeket serve --log-level debug
```

Every HTTP request gets a `request-id` header added by
`internal/metrics/middleware.go:Middleware` (~line 61). The same ID appears in
all downstream log lines for that request — use it to correlate logs.

### Key log observation points by layer

| Layer | Where to look | What you'll see |
|-------|---------------|-----------------|
| **HTTP** | `internal/api/server.go:ServeHTTP` | method, path, status, duration |
| **Scheduler** | `internal/scheduler/scheduler.go:getOrLoadWorker` (~line 137) | model load/hit/evict events |
| **Scheduler** | `internal/scheduler/scheduler.go:evictLRULocked` (~line 208) | which model was evicted and why |
| **Engine** | `internal/engine/engine.go:Session.Generate` (~line 277) | per-token decode errors, sampler panics |
| **Engine** | `engine.New` | library load path and version |

### Reading stack traces

A **SIGABRT** (not a Go panic) means llama.cpp called `GGML_ABORT`. This is
unrecoverable — the process terminates. Common causes and fixes:

- Grammar eliminates all token candidates → see [pitfall #1](#8-common-pitfalls).
- Context overflow at batch decode → reduce `--context-size` or input length.

A normal **goroutine dump** (from `SIGQUIT` or `runtime/debug`) shows the token
loop at `engine.(*Session).Generate`. Look for the `safeSamplerSample` and
`safeSamplerAccept` frames — panics from cgo are recovered there and converted
to Go errors.

### Common error messages

| Message | Cause | Fix |
|---------|-------|-----|
| `engine: load llama library: …` | Library not found at startup | Set `--lib-dir` or run with `--auto-install-lib` |
| `engine: grammar rejected all tokens` | Grammar + prompt produced 0 valid candidates | Don't set `GrammarStr` directly; use prompt-only guidance |
| `engine: init context: …` | Context params rejected by llama.cpp | Check `--context-size`; for embed models see pitfall #7 |
| `model not found` | Manifest missing | Run `beeket pull <model>` first |
| `response did not match the requested JSON schema` | Model output valid JSON but wrong shape | Loosen schema or use `"json"` format instead |

### Run a targeted test

```bash
go test -run TestName ./internal/api -v
go test -run TestEmbed ./internal/engine -v
```

Use Prometheus metrics at `GET /metrics` for production triage — see
[docs/monitoring.md](./monitoring.md) for the metric names.

---

## 4. How to add a new sampler option

This is the cleanest end-to-end change in the codebase: one field flows through
four files. Concrete example: adding `min_p` (already done — use it as the
reference) or adding a hypothetical `dry_multiplier`.

### Step 1 — Add field to `api.Options` in `internal/api/types.go`

```go
// Options holds per-request sampler and runtime overrides.
type Options struct {
    // ... existing fields ...
    DryMultiplier float32 `json:"dry_multiplier,omitempty"`
}
```

Field names must match the Ollama API where a corresponding option exists.

### Step 2 — Add field to `engine.SamplerOptions` in `internal/engine/engine.go`

Add the field (~line 100) and a sensible default in `DefaultSamplerOptions`
(~line 121):

```go
type SamplerOptions struct {
    // ... existing fields ...
    DryMultiplier float32 // 0 = disabled
}

func DefaultSamplerOptions() SamplerOptions {
    return SamplerOptions{
        // ... existing defaults ...
        // DryMultiplier: 0 (disabled by default)
    }
}
```

### Step 3 — Wire it in `buildGenerateOptions()` in `internal/api/handlers.go`

`buildGenerateOptions` (~line 791) maps `api.Options` → `engine.GenerateOptions`.
Add the mapping inside the function:

```go
if opts.DryMultiplier > 0 {
    goOpts.Sampler.DryMultiplier = opts.DryMultiplier
}
```

If the option is chat-specific (affects how messages are assembled rather than
the sampler), wire it in the `Chat` handler body instead.

### Step 4 — Apply it in `buildSampler()` and `buildSamplerWithGrammar()` in `engine.go`

`buildSampler` (~line 163) builds the standard sampler chain.
`buildSamplerWithGrammar` (~line 518) builds the chain for tool-call and
structured-output paths. Both must be updated:

```go
// Inside buildSampler and buildSamplerWithGrammar, after TempExt:
if opts.DryMultiplier > 0 {
    llama.SamplerChainAdd(chain, llama.SamplerInitDry(
        vocab, opts.DryMultiplier, /* other params */,
    ))
}
```

Consult the yzma `pkg/llama` package for the exact `SamplerInit*` function
signature.

### Step 5 — Write a unit test in `internal/engine/sampler_test.go`

Add a test that constructs `SamplerOptions` with your new field set, calls
`buildSampler`, and verifies no error is returned (and, where possible, that
the sampler produces expected output for a fixed seed):

```go
func TestBuildSampler_DryMultiplier(t *testing.T) {
    opts := DefaultSamplerOptions()
    opts.DryMultiplier = 0.8
    // buildSampler requires a vocab — use a nil vocab stub if the
    // sampler doesn't use it, or skip if FFI not available in CI.
    _, err := buildSampler(opts, 0, "")
    require.NoError(t, err)
}
```

See the existing tests in `sampler_test.go` for the patterns used.

### Step 6 — Document in `docs/options.md`

Add a row to the options table in [docs/options.md](./options.md) with the
field name, type, default, and a one-line description.

---

## 5. How to support a new model type

### What affects compatibility

| Factor | Where it lives | Notes |
|--------|---------------|-------|
| Chat template | GGUF metadata → `engine.Model.ChatTemplate()` (`engine.go` ~line 72) | Falls back to `"chatml"` if unset |
| Context window | GGUF metadata, overridable via `--context-size` | Set at session creation in `engine.NewSession` |
| Pooling type | `engine.EmbedSession.pooling` — read via `llama.GetPoolingType` post-init | Affects embedding extraction path |
| Vocabulary / tokenizer | `engine.Model.vocab` (llama.Vocab) — exposed via yzma | Token-ID arithmetic (e.g. EOG, BOS) handled by yzma |

### Adding a built-in alias in `internal/models/alias.go`

Add an entry to `builtinAliases` (~line 54):

```go
{
    Name:   "llama3",
    Tag:    "8b",
    Source: "https://huggingface.co/QuantFactory/Meta-Llama-3-8B-Instruct-GGUF/resolve/main/Meta-Llama-3-8B-Instruct.Q4_K_M.gguf",
},
```

The alias is automatically registered by `DefaultAliases()`. Users can then
run `beeket pull llama3:8b` without knowing the HuggingFace URL.

Provide the direct `.gguf` file URL (not the HF repo page). If the model has a
separate vision projector, also set `MMProjURL`.

### Testing embeddings with a new model

The diagnostic pattern in `engine.NewEmbedSession` (engine.go ~line 395)
captures the lessons learned from nomic-embed-text on Metal:

1. Use `llama.ContextDefaultParams()` with only `cp.NCtx = 0` overridden.
2. Call `llama.SetEmbeddings(ctx, true)` **after** successful context creation
   (not at init time).
3. Use `llama.GetEmbeddingsSeq(ctx, 0, nEmbd)` — not `GetEmbeddingsIth` (returns
   zeros for decoder-style models).

When testing a new embedding model, run `internal/engine/embed_test.go` with
the model path set via an environment variable or skip tag. Check that the
returned vector has the expected dimension (`nEmbd`) and that `l2Normalize`
produces a unit vector.

### When to add a new session type vs reuse existing

- **Reuse `Session`**: causal LM (text generation, chat, tool calling).
- **Reuse `EmbedSession`**: any model where embeddings are extracted from the
  final layer — most encoder-style or dual-encoder models.
- **New session type**: only if the model requires a fundamentally different
  decode loop (e.g. encoder-decoder / seq2seq). Discuss in an issue first.

### Multimodal (VLM) models

The mmproj path is stored in `store.Store.MMProjPath` and the loading wiring
is a planned future feature (see spec-v0.1.md §4). Do not implement VLM loading
without coordinating with the maintainers.

### Tool-calling capable architectures

Ensure the GBNF lazy-trigger token matches the model's tool-call open token.
For Qwen-family models this is `<tool_call>`. See
`internal/tools/grammar.go:BuildGrammar` (~line 27) for how the trigger is
constructed and how to add a new architecture's trigger token.

---

## 6. How to add a new CLI flag

Cobra pattern used throughout `cmd/beeket/main.go`.

### Step 1 — Identify the subcommand

Most server configuration goes on `serveCmd`. Client flags go on their
respective subcommands (`pullCmd`, `runCmd`, etc.).

### Step 2 — Add to `serveFlagValues` and bind the flag

`serveFlagValues` (~line 129) is the struct that holds cobra flag bindings for
`serveCmd`. Add a field:

```go
type serveFlagValues struct {
    // ... existing fields ...
    maxTokens int
}
```

Then bind it inside `serveCmd()` (~line 173):

```go
cmd.Flags().IntVar(&fv.maxTokens, "max-tokens", 512, "default max tokens per generation")
```

### Step 3 — Apply it in `applyServeFlags()` to `config.Runtime.*`

`applyServeFlags` (~line 376) overlays explicit flag values onto `*config.Config`.
Use `cmd.Flags().Changed("flag-name")` so only explicitly-set flags override
config-file or env values:

```go
if cmd.Flags().Changed("max-tokens") {
    cfg.Runtime.MaxTokens = fv.maxTokens
}
```

You'll need to add `MaxTokens int` to the `RuntimeConfig` struct in
`internal/config/config.go` first.

### Step 4 — Add env-var support (optional)

If the flag should also be settable via an environment variable, extend
`config.ApplyEnv` (~line 151 of `config.go`):

```go
if v := os.Getenv("BEEKET_MAX_TOKENS"); v != "" {
    if n, err := strconv.Atoi(v); err == nil {
        cfg.Runtime.MaxTokens = n
    }
}
```

### Step 5 — Validate it

Add a check in `config.Validate` (~line 213 of `config.go`):

```go
if cfg.Runtime.MaxTokens < -1 {
    return fmt.Errorf("max-tokens must be >= -1")
}
```

### Step 6 — Document in `docs/SETUP.md`

Add the flag to the **Configuration** section of [docs/SETUP.md](./SETUP.md)
alongside its env-var equivalent (if any).

**Concrete example — `--max-tokens` flag end-to-end:**

```
serveFlagValues.maxTokens  →  cmd.Flags().IntVar(...)
applyServeFlags: cfg.Runtime.MaxTokens = fv.maxTokens
config.Validate: MaxTokens >= -1
scheduler.Config: passes MaxTokens to engine.GenerateOptions.MaxTokens default
```

---

## 7. Testing

### Test file locations and naming

Tests are colocated with their source — `*_test.go` files sit next to the
package they test. No separate `test/` directory.

| Test file | What it covers |
|-----------|---------------|
| `internal/api/handlers_nothink_test.go` | `injectNoThink` logic (table-driven, no FFI) |
| `internal/api/handlers_format_test.go` | `resolveFormat` — JSON / schema parsing |
| `internal/api/handlers_chat_tools_test.go` | Chat handler tool-call path (fake scheduler) |
| `internal/api/handlers_embed_test.go` | Embeddings handler (fake scheduler) |
| `internal/engine/sampler_test.go` | `buildSampler` / `buildSamplerWithGrammar` |
| `internal/engine/embed_test.go` | `EmbedSession.Embed` (skipped in CI — requires a real model) |
| `internal/config/config_test.go` | Config load, env overlay, validation |
| `internal/download/downloader_test.go` | HTTP downloader (uses `httptest.Server`) |
| `internal/models/clean_test.go` | `CleanModelRef` parsing edge cases |
| `internal/jsongrammar/jsongrammar_test.go` | Schema → GBNF generation and validation |
| `internal/tools/grammar_test.go` | Tool GBNF builder |
| `internal/tools/parse_test.go` | `ParseToolCall` envelope detection |
| `internal/tools/prompt_test.go` | `RenderToolPreface` rendering |
| `internal/metrics/middleware_test.go` | HTTP middleware request-ID injection |
| `internal/libinstall/libinstall_test.go` | Library install path resolution |

### Running tests

```bash
# All tests
go test ./...

# With race detector (required before pushing)
go test -race ./...

# Single package
go test ./internal/api -v

# Single test by name
go test -run TestInjectNoThink ./internal/api -v

# With coverage
go test ./... -coverprofile=cover.out && go tool cover -html=cover.out
```

### How to add a test

Copy the table-driven pattern from `handlers_nothink_test.go`:

```go
func TestMyFunc_DescribeScenario(t *testing.T) {
    cases := []struct {
        name  string
        input SomeType
        want  SomeType
    }{
        {"happy path", SomeType{...}, SomeType{...}},
        {"edge case", SomeType{...}, SomeType{...}},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            got := MyFunc(tc.input)
            assert.Equal(t, tc.want, got)
        })
    }
}
```

For HTTP handler tests, use `httptest.NewRecorder()` and inject a fake
scheduler via the interface. See `handlers_embed_test.go` for the full setup.

**Note:** there are no engine-level integration tests that run against a real
GGUF file in CI. Tests that require a real model use `t.Skip()` with an env
guard. If you add such a test, follow that convention.

### CI

Two workflows run on every push and PR:

- **`test.yml`** — runs `go test ./... -v -race -count=1` on `ubuntu-latest`.
- **`qlty.yml`** — runs `qlty check --all` for code quality; posts a severity
  breakdown as a PR comment.

Both must be green before merge. See [docs/ci.md](./ci.md) for details.

---

## 8. Common pitfalls

### 1. SIGABRT from grammar — grammar eliminates all candidates

**Symptom:** process crashes mid-generation with `SIGABRT` / `GGML_ABORT`; no
Go stack trace.

**Cause:** the grammar sampler's NFA reduces the valid token set to zero.
llama.cpp calls `GGML_ABORT` which sends `SIGABRT` — uncatchable by Go's
`recover()`.

**Fix:** do not set `GenerateOptions.GrammarStr` directly. The `Generate` and
`Chat` handlers intentionally comment this out and use prompt-only guidance
(`jsonSystemPrompt` + `/no_think`) with post-generation schema validation
instead. See `resolveFormat` in `handlers.go` (~line 910) and the long comment
above it.

### 2. llama.cpp library mismatch — wrong yzma version vs lib build

**Symptom:** `engine: load llama library: …` error at startup, or silent
model-load failures.

**Cause:** the installed libllama was built for a different yzma ABI version.

**Fix:** reinstall with `yzma install --upgrade`, or pass `--lib-upgrade` to
`beeket serve --auto-install-lib --lib-upgrade`. Check the loaded library
path via `GET /api/status`.

### 3. Metal-specific failures — PoolingType incompatibility

**Symptom:** `EmbedSession` init fails or returns zero vectors on Apple Silicon.

**Cause:** overriding `cp.Embeddings` or `cp.PoolingType` at context creation
time causes `llama_init_from_model` to fail for some models on Metal.

**Fix:** follow the diagnostic pattern in `engine.NewEmbedSession` (engine.go
~line 395): use `ContextDefaultParams()` with only `cp.NCtx = 0`, then call
`llama.SetEmbeddings(ctx, true)` post-init. Use `GetEmbeddingsSeq`, not
`GetEmbeddingsIth`.

### 4. KV cache contamination — session reuse

**Symptom:** outputs bleed context from a previous request.

**Cause:** a `Session` accumulates position state (`s.pos`). If the same
session is reused across requests without reset, the KV cache carries stale
tokens.

**Fix:** this is handled by design — `scheduler.Worker.run` creates a fresh
`Session` per worker slot. If you see contamination, check that no code path
is manually caching sessions outside the scheduler.

### 5. `/no_think` ignored — system message doesn't work for Qwen3

**Symptom:** Qwen3/QwQ model still emits `<think>…</think>` blocks despite
`think: false` in the request.

**Cause:** per Qwen3 official docs, the `/no_think` control token must be
appended to the **last user message**, not placed in the system message.

**Fix:** `injectNoThink` in `handlers.go` (~line 948) handles this correctly
for chat requests. For `POST /api/generate`, the token is appended to the
`prompt` field. Do not add `/no_think` to the system message — it is silently
ignored there.

### 6. Tool call not detected — model doesn't emit JSON

**Symptom:** `tool_calls` is empty in the response even though the model is
supposed to call a tool.

**Cause:** the model did not emit the expected `<tool_call>…</tool_call>`
envelope, or emitted markdown fences instead.

**Fix:** check that the model supports tool calling (Qwen2.5/Qwen3 family is
well-tested). Inspect the raw output by temporarily logging `sb.String()` in
the `Chat` handler. `tools.ParseToolCall` in `internal/tools/parse.go` only
recognises the `<tool_call>` envelope; if your model uses a different format,
update `RenderToolPreface` in `internal/tools/prompt.go` and `ParseToolCall`
to match.

### 7. Embedding init failure — wrong context params

**Symptom:** `engine: init embed context: …` error when loading an embedding
model.

**Cause:** passing `cp.Embeddings = 1` or setting `cp.PoolingType` at init
time causes `llama_init_from_model` to throw for some models.

**Fix:** use the established pattern in `engine.NewEmbedSession`: construct
context with default params (only `cp.NCtx = 0`), then call
`llama.SetEmbeddings(ctx, true)` after the context is successfully created.

### 8. Pull resume on restart — partial download persists

**Symptom:** `beeket pull` resumes a partial download after a restart but the
model is corrupted or checksum fails.

**Cause:** `download.Get` writes to a `tmp/` path inside the store and renames
to the content-addressed blob path on completion. On an interrupted download,
the partial file stays in `tmp/`.

**Fix:** the downloader retries automatically on restart (resumable HTTP GET).
If the file is genuinely corrupted, delete it from `<data-dir>/tmp/` manually
and re-pull.

### 9. Model eviction too aggressive — models keep unloading

**Symptom:** every request reloads the model from disk, causing long first-token
latency.

**Cause:** `--num-parallel` is set too high (each slot creates a session,
consuming memory), causing the LRU eviction loop to evict frequently, or
`--keep-alive` is too short.

**Fix:** decrease `--num-parallel` to 1 (default) for single-user setups, or
increase `--max-loaded-models`. The eviction loop runs every 30 seconds
(`scheduler.go:evictionLoop`). Check the `models_loaded` Prometheus gauge.

### 10. `go test` fails after a new PR — likely formatting

**Symptom:** CI fails with `gofmt` diff or `go vet` finding.

**Cause:** qlty enforces gofmt; a stray unformatted file causes the check to
fail.

**Fix:**

```bash
gofmt -w ./...
go vet ./...
git diff  # verify only formatting changes
```

---

## 9. PR process

### Branch from `main`

```bash
git fetch origin
git checkout -b feat/my-feature origin/main
# ... make changes ...
git push origin feat/my-feature
```

Branch naming convention: `<topic>/<short-desc>` — e.g. `feat/tokenize-endpoint`,
`fix/grammar-sigabrt`, `docs/options-update`. No strict enforcement; follow
existing PRs in the repo.

### Pre-push checklist

- [ ] `go test ./...` passes
- [ ] `go test -race ./...` passes
- [ ] `go vet ./...` is clean
- [ ] `gofmt -l ./...` produces no output
- [ ] `docs/` updated for any user-visible change
- [ ] `docs/spec-v0.1.md` updated for any new or changed API route

### CI requirements

Two workflows must both be green before merge:

| Workflow | What it runs | Config |
|----------|-------------|--------|
| **test** | `go test ./... -v -race -count=1` on ubuntu-latest | `.github/workflows/test.yml` |
| **qlty** | `qlty check --all` (code quality) | `.github/workflows/qlty.yml` |

Both run on push to `main` and on every PR. See [docs/ci.md](./ci.md).

### PR style

- **Title**: `<type>: short imperative description` — e.g. `feat: add tokenize endpoint`,
  `fix: recover from grammar NFA crash`, `docs: update sampler options`.
- **Body**: what changed, why, and testing notes. Keep it under ~150 words.
  No headers needed. Link the issue if one exists: `Closes #N`.
- No emoji, no AI disclaimers.

### What reviewers check

- Correctness: does the change do what it says?
- Tests: is there a test? Does it cover the interesting cases?
- Docs: is `docs/` updated for user-visible changes?
- Formatting: `gofmt -l` must produce no output.
- Architecture: no handler calling `engine.*` directly (must go through scheduler).
- Dependency rules: no new import cycles (see spec-v0.1.md §2.4).

### Merge

Merge decision (squash or merge commit) is made by the maintainer. Do not
merge your own PR.

Do not bump `internal/version/version.go` in feature PRs — version bumps are
reserved for release commits.
