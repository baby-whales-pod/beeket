# Getting Started with yzma: Local LLM Inference in Go

[yzma](https://github.com/hybridgroup/yzma) is a Go library that provides bindings to
[llama.cpp](https://github.com/ggml-org/llama.cpp), letting you run large and small
language models **locally, inside your own Go process** — no CGo, no external servers,
and with full hardware acceleration (CUDA, Metal, Vulkan, ROCm, …).

This tutorial walks you through everything from a blank directory to a working streaming
chat program.

---

## Table of Contents

1. [What yzma does](#1-what-yzma-does)
2. [Prerequisites](#2-prerequisites)
3. [Create a new Go project](#3-create-a-new-go-project)
4. [Add the yzma dependency](#4-add-the-yzma-dependency)
5. [Install the llama.cpp shared libraries](#5-install-the-llamacpp-shared-libraries)
6. [Download a model](#6-download-a-model)
7. [Load the library and initialise llama.cpp](#7-load-the-library-and-initialise-llamacpp)
8. [Load a model file](#8-load-a-model-file)
9. [Create an inference context](#9-create-an-inference-context)
10. [Build a chat prompt](#10-build-a-chat-prompt)
11. [Tokenize the prompt](#11-tokenize-the-prompt)
12. [Set up a sampler chain](#12-set-up-a-sampler-chain)
13. [Run inference token by token](#13-run-inference-token-by-token)
14. [Decode tokens to text](#14-decode-tokens-to-text)
15. [Clean up resources](#15-clean-up-resources)
16. [Full working example](#16-full-working-example)

---

## 1. What yzma does

yzma uses the [`purego`](https://github.com/ebitengine/purego) and
[`ffi`](https://github.com/JupiterRider/ffi) packages to call the `llama.cpp` shared
library **from within the same OS process**. This gives you near-native throughput
without a C compiler, without CGo, and without a separate model server.

Key properties:

- Builds with plain `go build` / `go run` — no C toolchain needed.
- Works with any GGUF model (the standard format for quantised LLMs on Hugging Face).
- Supports CPU, CUDA, Metal, Vulkan, HIP/ROCm, SYCL, and OpenCL backends.
- Tracks llama.cpp releases closely; tested automatically on every new release.

---

## 2. Prerequisites

| Requirement | Notes |
|---|---|
| Go 1.22+ | Tested with 1.22 and later. |
| `yzma` CLI | Used to install llama.cpp libraries and download models. |
| llama.cpp shared libraries | Installed via `yzma install`. |

### Install the yzma CLI

```bash
go install github.com/hybridgroup/yzma@latest
```

Make sure `$(go env GOPATH)/bin` is in your `PATH`.

---

## 3. Create a new Go project

```bash
mkdir my-llm-app
cd my-llm-app
go mod init github.com/yourname/my-llm-app
```

---

## 4. Add the yzma dependency

```bash
go get github.com/hybridgroup/yzma@latest
```

Your `go.mod` will now reference `github.com/hybridgroup/yzma`.

---

## 5. Install the llama.cpp shared libraries

yzma depends on the `llama.cpp` **shared library** at runtime. Use the `yzma` CLI to
download a pre-built binary for your platform.

### Choose a location and install

```bash
yzma install --lib /path/to/lib
```

Then tell yzma where to find it at runtime via the `YZMA_LIB` environment variable:

```bash
export YZMA_LIB=/path/to/lib
```

Inside your Go program you read this variable and pass it to `llama.Load`:

```go
libPath := os.Getenv("YZMA_LIB")
if err := llama.Load(libPath); err != nil {
    log.Fatal(err)
}
```

Passing an empty string tells yzma to use the OS dynamic-linker search path instead of
a specific directory.

### GPU variants

| Platform | Command |
|---|---|
| CPU only (default) | `yzma install --lib /path/to/lib` |
| CUDA (Linux / Windows) | `yzma install --lib /path/to/lib --processor cuda` |
| ROCm (Linux / AMD GPU) | `yzma install --lib /path/to/lib --processor rocm` |
| Vulkan | `yzma install --lib /path/to/lib --processor vulkan` |
| Metal (macOS) | `yzma install --lib /path/to/lib` (auto-detected) |

> Follow any extra instructions printed to the terminal after `yzma install` (e.g.
> running `ldconfig` on Linux).

---

## 6. Download a model

Models must be in **GGUF format**. This tutorial uses `SmolLM2-135M.Q4_K_M.gguf` — a
tiny but functional model that downloads in seconds and runs on any hardware:

```bash
yzma model get -u https://huggingface.co/QuantFactory/SmolLM2-135M-GGUF/resolve/main/SmolLM2-135M.Q4_K_M.gguf
```

By default, models are stored in `~/models/`. The helper function
`download.DefaultModelsDir()` (from `github.com/hybridgroup/yzma/pkg/download`) returns
that path.

For a larger, instruction-tuned model, replace the URL with one from
<https://huggingface.co/models?library=gguf>.

---

## 7. Load the library and initialise llama.cpp

Create `main.go` and start with the initialisation sequence. Every yzma program must:

1. Call `llama.Load` to open the shared library.
2. Optionally silence the llama.cpp log output.
3. Call `llama.Init` to initialise the GGML backends.

```go
package main

import (
    "log"
    "os"

    "github.com/hybridgroup/yzma/pkg/llama"
)

func main() {
    // 1. Load the llama.cpp shared library.
    //    YZMA_LIB should point to the directory containing libllama.so / llama.dll / libllama.dylib.
    //    An empty string tells yzma to rely on the OS linker search path.
    libPath := os.Getenv("YZMA_LIB")
    if err := llama.Load(libPath); err != nil {
        log.Fatalf("llama.Load: %v", err)
    }

    // 2. Silence the verbose llama.cpp log output (optional but recommended).
    llama.LogSet(llama.LogSilent())

    // 3. Initialise the GGML backends (CPU, CUDA, Metal, …).
    llama.Init()
    defer llama.Close()

    // … rest of your program
}
```

> `llama.Init()` and `llama.Close()` do not return errors. All resource acquisition
> happens via model and context functions that do return errors.

---

## 8. Load a model file

```go
import (
    "log"
    "path/filepath"

    "github.com/hybridgroup/yzma/pkg/download"
    "github.com/hybridgroup/yzma/pkg/llama"
)

const modelFile = "SmolLM2-135M.Q4_K_M.gguf"

// ModelDefaultParams returns sensible defaults:
//   - NGpuLayers = 0  (CPU only; set to 99 to offload everything to GPU)
//   - UseMmap    = true
//   - UseMlock   = false
modelParams := llama.ModelDefaultParams()
// Uncomment to run on GPU:
// modelParams.NGpuLayers = 99

modelPath := filepath.Join(download.DefaultModelsDir(), modelFile)
model, err := llama.ModelLoadFromFile(modelPath, modelParams)
if err != nil {
    log.Fatalf("ModelLoadFromFile: %v", err)
}
defer llama.ModelFree(model)
```

`ModelLoadFromFile` returns an opaque `llama.Model` handle (a `uintptr` under the
hood). It returns an error if the file does not exist or is not valid GGUF.

---

## 9. Create an inference context

The **context** holds the KV-cache and manages decode state. It is created from a
loaded model:

```go
ctxParams := llama.ContextDefaultParams()
// Override context window size if needed (0 = use model default):
// ctxParams.NCtx = 2048

ctx, err := llama.InitFromModel(model, ctxParams)
if err != nil {
    log.Fatalf("InitFromModel: %v", err)
}
defer llama.Free(ctx)
```

Key `ContextParams` fields:

| Field | Default | Meaning |
|---|---|---|
| `NCtx` | from model | Number of tokens in the KV cache |
| `NBatch` | 2048 | Maximum logical batch size |
| `NThreads` | runtime.NumCPU() | Threads used for single-token decode |
| `NThreadsBatch` | runtime.NumCPU() | Threads used for prompt processing |

---

## 10. Build a chat prompt

Instruction-tuned models expect a specific chat format (ChatML, Llama-3, Qwen, …).
yzma wraps the llama.cpp chat templating API so you don't have to format strings by
hand.

```go
// Get the vocabulary handle — needed for tokenisation and chat templating.
vocab := llama.ModelGetVocab(model)

// Build a list of chat messages.
messages := []llama.ChatMessage{
    llama.NewChatMessage("system", "You are a helpful assistant."),
    llama.NewChatMessage("user", "What is the capital of France?"),
}

// Apply the model's built-in chat template.
// Pass "" as template name to use the model's own embedded template.
// Set addAssistantPrompt = true so the model knows it must reply.
buf := make([]byte, 4096)
n := llama.ChatApplyTemplate("", messages, true, buf)
if n <= 0 {
    log.Fatal("ChatApplyTemplate failed")
}
formattedPrompt := string(buf[:n])
```

`ChatApplyTemplate` parameters:

| Parameter | Description |
|---|---|
| `template` | Named template (`"chatml"`, `"llama2"`, …) or `""` to use the model's own |
| `chat` | Slice of `ChatMessage` |
| `addAssistantPrompt` | Appends the assistant opening marker when `true` |
| `buf` | Output buffer — must be large enough for the formatted prompt |

---

## 11. Tokenize the prompt

```go
// Tokenize converts the formatted prompt into a slice of integer token IDs.
// addSpecial=true  → prepend the BOS token
// parseSpecial=true → recognise <|...|> control tokens in the text
tokens := llama.Tokenize(vocab, formattedPrompt, true, true)
if len(tokens) == 0 {
    log.Fatal("tokenisation produced no tokens")
}
fmt.Printf("Prompt: %d tokens\n", len(tokens))
```

---

## 12. Set up a sampler chain

Samplers sit between the raw model logits and the final token choice. You build a
**chain**; each sampler filters or re-weights the probability distribution before the
next one sees it.

```go
// Create an empty chain.
sampler := llama.SamplerChainInit(llama.SamplerChainDefaultParams())
defer llama.SamplerFree(sampler) // frees the chain AND all samplers added to it

// Add samplers in order:

// 1. Top-K: keep only the K most likely tokens.
llama.SamplerChainAdd(sampler, llama.SamplerInitTopK(40))

// 2. Top-P (nucleus sampling): keep the smallest set of tokens whose
//    cumulative probability exceeds P.  The second arg is min_keep (1 = keep
//    at least one token even if P is very tight).
llama.SamplerChainAdd(sampler, llama.SamplerInitTopP(0.95, 1))

// 3. Temperature extended: t=0.8 (lower = more deterministic),
//    delta=0.0 and exponent=1.0 give plain temperature (no dynamic range).
llama.SamplerChainAdd(sampler, llama.SamplerInitTempExt(0.8, 0.0, 1.0))

// 4. Distribution: draw the final token from the filtered distribution.
//    llama.DefaultSeed (0xFFFFFFFF) produces a new random seed each run.
llama.SamplerChainAdd(sampler, llama.SamplerInitDist(llama.DefaultSeed))
```

> **Greedy sampling** (always pick the most probable token, fully deterministic):
>
> ```go
> sampler := llama.SamplerChainInit(llama.SamplerChainDefaultParams())
> llama.SamplerChainAdd(sampler, llama.SamplerInitGreedy())
> ```

---

## 13. Run inference token by token

yzma's generation loop performs three steps per token:

1. **`llama.Decode`** — run the transformer forward pass on a batch, filling the
   KV-cache and computing logits.
2. **`llama.SamplerSample`** — pick the next token from the logits.
3. **`llama.SamplerAccept`** — inform the sampler of the chosen token (needed by
   repetition-penalty and similar samplers).

```go
const maxNewTokens = 200

// Wrap the prompt tokens in a single-sequence batch.
batch := llama.BatchGetOne(tokens)

for range maxNewTokens {
    // Run the forward pass.
    if _, err := llama.Decode(ctx, batch); err != nil {
        log.Fatalf("Decode: %v", err)
    }

    // Sample the next token. idx=-1 means "use the last token's logits".
    token := llama.SamplerSample(sampler, ctx, -1)

    // Stop at any end-of-generation token (EOS, EOT, etc.).
    if llama.VocabIsEOG(vocab, token) {
        break
    }

    // Convert the token ID to its text fragment and stream it.
    piece := make([]byte, 64)
    n := llama.TokenToPiece(vocab, token, piece, 0, true)
    if n > 0 {
        fmt.Print(string(piece[:n]))
    }

    // Inform the sampler of the accepted token.
    llama.SamplerAccept(sampler, token)

    // Feed the new token back as the next single-token batch.
    batch = llama.BatchGetOne([]llama.Token{token})
}
fmt.Println()
```

---

## 14. Decode tokens to text

`llama.TokenToPiece` converts one token ID to its UTF-8 byte fragment:

```go
piece := make([]byte, 64) // 64 bytes is sufficient for any single token
n := llama.TokenToPiece(vocab, token, piece, 0, true)
// n > 0: bytes written; n < 0: buffer too small (increase slice size)
fmt.Print(string(piece[:n]))
```

| Parameter | Description |
|---|---|
| `vocab` | Vocabulary handle from `llama.ModelGetVocab` |
| `token` | Token ID to convert |
| `buf` | Output byte slice |
| `lstrip` | Leading spaces to strip (pass `0`) |
| `special` | Render special tokens as text (`true` is safe for display) |

---

## 15. Clean up resources

Always free resources in reverse order of creation:

```go
llama.SamplerFree(sampler)  // frees the chain + all samplers in it
llama.Free(ctx)             // frees the inference context and KV-cache
llama.ModelFree(model)      // unloads the model weights
llama.Close()               // shuts down GGML backends
```

Using `defer` at creation time is the idiomatic Go approach (as shown in the full
example below).

---

## 16. Full working example

The program below puts every step together. It loads `SmolLM2-135M.Q4_K_M.gguf`,
formats a user message with the model's chat template, and streams the response
token-by-token to stdout.

```go
package main

import (
    "flag"
    "fmt"
    "log"
    "os"
    "path/filepath"

    "github.com/hybridgroup/yzma/pkg/download"
    "github.com/hybridgroup/yzma/pkg/llama"
)

func main() {
    // ---- flags ----
    modelName := flag.String("model", "SmolLM2-135M.Q4_K_M.gguf",
        "GGUF model filename inside ~/models/")
    userPrompt := flag.String("prompt", "What is the capital of France?",
        "User message to send to the model")
    maxTokens := flag.Int("max", 200, "Maximum number of tokens to generate")
    flag.Parse()

    // ---- 1. Load the llama.cpp shared library ----
    libPath := os.Getenv("YZMA_LIB")
    if err := llama.Load(libPath); err != nil {
        log.Fatalf("llama.Load: %v", err)
    }

    // ---- 2. Silence verbose llama.cpp logging ----
    llama.LogSet(llama.LogSilent())

    // ---- 3. Initialise GGML backends ----
    llama.Init()
    defer llama.Close()

    // ---- 4. Load the model ----
    modelPath := filepath.Join(download.DefaultModelsDir(), *modelName)
    modelParams := llama.ModelDefaultParams()
    // To offload all layers to GPU:  modelParams.NGpuLayers = 99

    model, err := llama.ModelLoadFromFile(modelPath, modelParams)
    if err != nil {
        log.Fatalf("ModelLoadFromFile: %v", err)
    }
    defer llama.ModelFree(model)

    // ---- 5. Create inference context ----
    ctxParams := llama.ContextDefaultParams()
    ctx, err := llama.InitFromModel(model, ctxParams)
    if err != nil {
        log.Fatalf("InitFromModel: %v", err)
    }
    defer llama.Free(ctx)

    // ---- 6. Build the chat prompt ----
    vocab := llama.ModelGetVocab(model)

    messages := []llama.ChatMessage{
        llama.NewChatMessage("system", "You are a helpful assistant."),
        llama.NewChatMessage("user", *userPrompt),
    }

    tmplBuf := make([]byte, 8192)
    n := llama.ChatApplyTemplate("", messages, true, tmplBuf)
    if n <= 0 {
        log.Fatal("ChatApplyTemplate failed")
    }
    formattedPrompt := string(tmplBuf[:n])

    // ---- 7. Tokenize ----
    tokens := llama.Tokenize(vocab, formattedPrompt, true, true)
    if len(tokens) == 0 {
        log.Fatal("tokenisation produced no tokens")
    }

    // ---- 8. Set up the sampler chain ----
    sampler := llama.SamplerChainInit(llama.SamplerChainDefaultParams())
    defer llama.SamplerFree(sampler)

    llama.SamplerChainAdd(sampler, llama.SamplerInitTopK(40))
    llama.SamplerChainAdd(sampler, llama.SamplerInitTopP(0.95, 1))
    llama.SamplerChainAdd(sampler, llama.SamplerInitTempExt(0.8, 0.0, 1.0))
    llama.SamplerChainAdd(sampler, llama.SamplerInitDist(llama.DefaultSeed))

    // ---- 9. Inference loop ----
    fmt.Printf("\nUser: %s\n\nAssistant: ", *userPrompt)

    batch := llama.BatchGetOne(tokens)
    for range *maxTokens {
        if _, err := llama.Decode(ctx, batch); err != nil {
            log.Fatalf("Decode: %v", err)
        }

        token := llama.SamplerSample(sampler, ctx, -1)

        if llama.VocabIsEOG(vocab, token) {
            break
        }

        piece := make([]byte, 64)
        n := llama.TokenToPiece(vocab, token, piece, 0, true)
        if n > 0 {
            fmt.Print(string(piece[:n]))
        }

        llama.SamplerAccept(sampler, token)
        batch = llama.BatchGetOne([]llama.Token{token})
    }

    fmt.Println()
}
```

### Project layout

```
my-llm-app/
├── go.mod
├── go.sum
└── main.go
```

### Download the model and run

```bash
# Download the model (only needed once)
yzma model get -u https://huggingface.co/QuantFactory/SmolLM2-135M-GGUF/resolve/main/SmolLM2-135M.Q4_K_M.gguf

# Run
YZMA_LIB=/path/to/lib go run ./main.go

# Or with a custom prompt
YZMA_LIB=/path/to/lib go run ./main.go -prompt "Explain what a goroutine is."

# GPU offload (if you installed the CUDA libraries)
YZMA_LIB=/path/to/lib go run ./main.go
# (set modelParams.NGpuLayers = 99 in the source)
```

Expected output (exact text varies with temperature sampling):

```
User: What is the capital of France?
Assistant: Paris is the capital of France.
```

### Troubleshooting

| Problem | Fix |
|---|---|
| `unable to load library` | Set `YZMA_LIB` to the directory containing the `.so` / `.dylib` / `.dll` file. |
| `context size too large` | Lower `ctxParams.NCtx`. |
| Out of memory | Use a more quantised model (Q4_K_M instead of F16), lower `NCtx`. |
| Slow inference | Install the CUDA / Metal / Vulkan variant of the libraries. |
| `failed to load model` | Check the file path and that the file is a valid GGUF. |
