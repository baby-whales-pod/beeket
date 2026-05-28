# Beeket — Setup Guide

This guide walks you through installing prerequisites, building Beeket from source, configuring it, pulling a model, and running inference — on Linux, macOS, and Windows.

---

## Table of Contents

1. [Prerequisites](#1-prerequisites)
2. [Cloning the repository](#2-cloning-the-repository)
3. [Installing the Yzma / llama.cpp shared library](#3-installing-the-yzma--llamacpp-shared-library)
4. [Building Beeket](#4-building-beeket)
5. [Configuration](#5-configuration)
6. [Downloading a model](#6-downloading-a-model)
7. [Running the server](#7-running-the-server)
8. [Using the CLI](#8-using-the-cli)
9. [Hardware acceleration](#9-hardware-acceleration)
10. [Troubleshooting](#10-troubleshooting)

---

## 1. Prerequisites

### Go

Beeket requires **Go 1.22 or later**.

```bash
# Check your installed version
go version
# Expected output: go version go1.22.x ...
```

Download Go from https://go.dev/dl/ if needed. The `go.mod` declares the minimum version; `go mod tidy` will tell you if yours is too old.

### Git

```bash
git --version   # any recent version is fine
```

### Platform-specific build tools

These are required to compile any native dependencies pulled in transitively.

| Platform | Requirement | Install |
|----------|-------------|---------|
| **Linux** | `gcc`, `make` | `sudo apt install build-essential` (Debian/Ubuntu) · `sudo dnf install gcc make` (Fedora) |
| **macOS** | Xcode Command Line Tools | `xcode-select --install` |
| **Windows** | MinGW-w64 or MSVC | Install [MinGW-w64](https://www.mingw-w64.org/) or [Build Tools for Visual Studio](https://visualstudio.microsoft.com/downloads/#build-tools-for-visual-studio-2022) |

> **Note:** Beeket itself uses no CGo — it talks to `llama.cpp` via Yzma's pure-Go FFI layer. The build tools above are only needed for any cgo-using indirect dependencies.

### Optional: GPU toolkits

Install these only if you want hardware-accelerated inference:

| Backend | Toolkit | Guide |
|---------|---------|-------|
| NVIDIA CUDA | CUDA Toolkit 12+ | https://developer.nvidia.com/cuda-downloads |
| AMD ROCm | ROCm 6+ | https://rocm.docs.amd.com/en/latest/deploy/linux/index.html |
| Vulkan | Vulkan SDK | https://vulkan.lunarg.com/sdk/home |
| Apple Metal | Xcode 15+ (macOS only) | Bundled with Xcode |

---

## 2. Cloning the Repository

```bash
git clone https://github.com/baby-whales-pod/beeket.git
cd beeket
```

All subsequent commands assume you are in the repository root unless stated otherwise.

---

## 3. Installing the Yzma / llama.cpp Shared Library

Beeket uses [Yzma](https://github.com/hybridgroup/yzma) — a pure-Go FFI wrapper around `llama.cpp`. Because it loads the inference engine at runtime via `dlopen` / `LoadLibrary`, **you need the `llama.cpp` shared library on your system** before running `beeketd`.

### Option A — Use the Yzma installer (recommended)

The Yzma project ships a prebuilt library installer for every supported platform and backend.

```bash
# Install the yzma CLI (used only to fetch the library)
go install github.com/hybridgroup/yzma@latest

# Download the right prebuilt library for your platform + processor
# CPU-only (works everywhere):
yzma install --lib /path/to/lib --processor cpu

# CUDA (Linux/Windows, requires CUDA Toolkit):
yzma install --lib /path/to/lib --processor cuda

# Metal (macOS Apple Silicon / Intel):
yzma install --lib /path/to/lib --processor metal

# Vulkan (cross-platform GPU):
yzma install --lib /path/to/lib --processor vulkan

# ROCm (AMD, Linux):
yzma install --lib /path/to/lib --processor rocm
```

Replace `/path/to/lib` with the directory where you want the shared library files installed (e.g. `~/.local/share/yzma/lib`). Alternatively, set the `YZMA_LIB` environment variable to that directory and omit `--lib`:

```bash
export YZMA_LIB=/path/to/lib
yzma install --processor metal
```

### Option B — Build llama.cpp from source

Follow the [llama.cpp build guide](https://github.com/ggml-org/llama.cpp#build) and produce `libllama.so` (Linux), `libllama.dylib` (macOS), or `llama.dll` (Windows). Then point Beeket at it:

```bash
export YZMA_LIB=/path/to/lib   # directory containing libllama.so / libllama.dylib
```

### Option C — Let Beeket auto-install it

Start `beeketd` with `--auto-install-lib` and it will fetch a suitable prebuilt library on first run:

```bash
beeketd --auto-install-lib
```

### Verifying the library is found

```bash
beeketd version
# Expected: beeket 0.1.0-dev (commit ..., built ...)
# If the library is missing you will see: "engine: load llama library: ..."
```

---

## 4. Building Beeket

```bash
# Download all Go dependencies
go mod download

# Build both binaries into the current directory
go build -o beeketd ./cmd/beeketd
go build -o beeket  ./cmd/beeket

# Or install them to $GOPATH/bin (must be on your $PATH)
go install ./cmd/beeketd ./cmd/beeket
```

### Embedding version info at build time

```bash
VERSION=0.1.0
COMMIT=$(git rev-parse --short HEAD)
DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ)

go build \
  -ldflags "-X github.com/baby-whales-pod/beeket/internal/version.Version=${VERSION} \
            -X github.com/baby-whales-pod/beeket/internal/version.Commit=${COMMIT} \
            -X github.com/baby-whales-pod/beeket/internal/version.BuildDate=${DATE}" \
  -o beeketd ./cmd/beeketd
```

### Running tests

```bash
# Unit tests (no llama.cpp library required)
go test ./internal/config/... ./internal/store/... ./internal/models/... \
        ./internal/download/... ./internal/api/... ./internal/version/...

# All tests including integration (requires libllama):
go test ./...

# With race detector:
go test -race ./...

# With coverage:
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

---

## 5. Configuration

### Config file location

Beeket uses [XDG Base Directory](https://specifications.freedesktop.org/basedir-spec/basedir-spec-latest.html) conventions:

| Purpose | Default path |
|---------|--------------|
| Config file | `~/.config/beeket/beeket.toml` |
| Model data | `~/.local/share/beeket/` |
| Logs / socket | `~/.local/state/beeket/` |

Override locations with environment variables:

```bash
export XDG_CONFIG_HOME=/custom/config
export XDG_DATA_HOME=/fast/nvme/data
```

### Example `beeket.toml`

```toml
[server]
host    = "127.0.0.1"   # bind address; change to "0.0.0.0" to expose on network
port    = 11435          # default port (one above Ollama's 11434)
origins = ["http://localhost:11435"]

[paths]
data_dir = ""   # leave blank to use XDG default (~/.local/share/beeket)
lib_dir  = ""   # leave blank to auto-detect libllama; or set to /path/to/dir

[runtime]
backend      = "auto"    # auto | cpu | cuda | metal | vulkan | rocm
gpu_layers   = -1        # -1 = offload all layers to GPU; 0 = CPU only
num_parallel = 1         # parallel inference slots per model
max_loaded   = 3         # max models held in memory simultaneously
keep_alive   = "5m"      # unload a model after this idle period
context_size = 4096      # default context window (tokens)

[download]
concurrency = 4          # parallel download streams

[log]
level  = "info"    # debug | info | warn | error
format = "text"    # text | json
```

### Environment variable overrides

Every config key has a corresponding `BEEKET_` env var:

| Env var | Equivalent TOML | Default |
|---------|-----------------|---------|
| `BEEKET_HOST` | `server.host` | `127.0.0.1` |
| `BEEKET_PORT` | `server.port` | `11435` |
| `BEEKET_DATA_DIR` | `paths.data_dir` | XDG data dir |
| `BEEKET_LIB_DIR` / `YZMA_LIB` | `paths.lib_dir` | auto-detect |
| `BEEKET_BACKEND` | `runtime.backend` | `auto` |
| `BEEKET_GPU_LAYERS` | `runtime.gpu_layers` | `-1` |
| `BEEKET_NUM_PARALLEL` | `runtime.num_parallel` | `1` |
| `BEEKET_MAX_LOADED_MODELS` | `runtime.max_loaded` | `3` |
| `BEEKET_KEEP_ALIVE` | `runtime.keep_alive` | `5m` |
| `BEEKET_CONTEXT_SIZE` | `runtime.context_size` | `4096` |
| `BEEKET_LOG_LEVEL` | `log.level` | `info` |
| `BEEKET_LOG_FORMAT` | `log.format` | `text` |

### CLI flag overrides

```bash
beeketd --port 11436 --backend cuda --log-level debug
```

Run `beeketd --help` for the full flag list.

---

## 6. Downloading a Model

### Using the built-in aliases (quickest)

```bash
# Pull SmolLM2 135M (tiny — great for testing)
beeket pull smollm2:135m

# Pull Qwen 2.5 0.5B
beeket pull qwen2.5:0.5b

# Pull Gemma 3 1B
beeket pull gemma3:1b

# Pull a text embedding model
beeket pull nomic-embed-text
```

### From Hugging Face (HF shorthand)

```bash
# hf.co/<org>/<repo>:<quantization>
beeket pull hf.co/Qwen/Qwen2.5-0.5B-Instruct-GGUF:Q4_K_M

# hf.co/<org>/<repo>/<filename>.gguf
beeket pull hf.co/QuantFactory/SmolLM2-135M-GGUF/SmolLM2-135M.Q4_K_M.gguf
```

### From a direct URL

```bash
beeket pull https://huggingface.co/QuantFactory/SmolLM2-135M-GGUF/resolve/main/SmolLM2-135M.Q4_K_M.gguf
```

### From a local file

```bash
beeket pull file:///path/to/your/model.gguf
```

### List installed models

```bash
beeket list
# NAME              SIZE    QUANT    MODIFIED
# smollm2:135m      87.0 MB Q4_K_M   2026-05-27T10:00:00Z
```

---

## 7. Running the Server

```bash
# Start with defaults (binds to 127.0.0.1:11435)
beeketd

# Custom port, expose on network (be careful — no auth in v0.1)
beeketd --host 0.0.0.0 --port 11436

# JSON structured logs (useful for log aggregation)
beeketd --log-format json

# Verbose debug output
beeketd --log-level debug
```

### Verifying the server is running

```bash
# Liveness
curl http://127.0.0.1:11435/healthz
# ok

# Readiness (engine initialised)
curl http://127.0.0.1:11435/readyz
# ok

# Version
curl http://127.0.0.1:11435/api/version
# {"version":"0.1.0-dev"}

# List loaded models
curl http://127.0.0.1:11435/api/ps
```

### Running as a background service (systemd)

```ini
# ~/.config/systemd/user/beeketd.service
[Unit]
Description=Beeket model server
After=network.target

[Service]
ExecStart=%h/.local/bin/beeketd
Restart=on-failure
Environment=BEEKET_LOG_FORMAT=json

[Install]
WantedBy=default.target
```

```bash
systemctl --user daemon-reload
systemctl --user enable --now beeketd
journalctl --user -fu beeketd
```

---

## 8. Using the CLI

### One-shot generation

```bash
beeket run smollm2:135m -p "Explain quantum entanglement in one sentence."

# Stream tokens as they are generated (default):
beeket run smollm2:135m --stream -p "Write a haiku about Go."
```

### Pull and run in one command

If `beeketd` is running, `beeket run` will automatically pull the model if it is not already installed:

```bash
beeket run gemma3:1b -p "Hello, world!"
```

### List installed models

```bash
beeket list
```

### Show model details

```bash
beeket show smollm2:135m
```

### Remove a model

```bash
beeket rm smollm2:135m
```

### Show loaded models and memory usage

```bash
beeket ps
```

### Point the CLI at a non-default server

```bash
beeket --server http://192.168.1.10:11435 list
```

### Using the HTTP API directly

```bash
# Generate (streaming NDJSON)
curl http://127.0.0.1:11435/api/generate \
  -d '{"model":"smollm2:135m","prompt":"Hello!","stream":true}'

# Chat
curl http://127.0.0.1:11435/api/chat \
  -d '{
    "model": "smollm2:135m",
    "messages": [
      {"role": "system", "content": "You are helpful."},
      {"role": "user",   "content": "What is 2+2?"}
    ],
    "stream": true
  }'

# Embeddings
curl http://127.0.0.1:11435/api/embeddings \
  -d '{"model":"nomic-embed-text","input":["hello","world"]}'
```

---

## 9. Hardware Acceleration

Beeket selects the GPU backend via the `BEEKET_BACKEND` env var or `--backend` flag. The default is `auto`, which delegates to Yzma's auto-detection.

### macOS — Apple Metal

Metal is supported out of the box on Apple Silicon (M1/M2/M3/M4) and Intel Macs with a discrete GPU.

```bash
# Install the Metal-enabled library
yzma install --lib /path/to/lib --processor metal

# Run with Metal
beeketd --backend metal
```

Verify Metal is being used:

```bash
beeketd --backend metal --log-level debug 2>&1 | grep -i metal
```

### Linux — NVIDIA CUDA

1. Install the [CUDA Toolkit](https://developer.nvidia.com/cuda-downloads) (12.x recommended).
2. Install the CUDA-enabled library:
   ```bash
   yzma install --lib /path/to/lib --processor cuda
   ```
3. Run:
   ```bash
   beeketd --backend cuda
   # or
   BEEKET_BACKEND=cuda beeketd
   ```

Control GPU layer offload (default `-1` = offload everything):

```bash
beeketd --backend cuda --gpu-layers 20   # offload only 20 transformer layers
```

### Linux — AMD ROCm

1. Install [ROCm 6+](https://rocm.docs.amd.com/en/latest/deploy/linux/index.html).
2. Install the ROCm library:
   ```bash
   yzma install --lib /path/to/lib --processor rocm
   ```
3. Run:
   ```bash
   BEEKET_BACKEND=rocm beeketd
   ```

### Cross-platform — Vulkan

Vulkan works on NVIDIA, AMD, and Intel GPUs across Linux and Windows.

1. Install the [Vulkan SDK](https://vulkan.lunarg.com/sdk/home).
2. Install the Vulkan library:
   ```bash
   yzma install --lib /path/to/lib --processor vulkan
   ```
3. Run:
   ```bash
   beeketd --backend vulkan
   ```

### Checking which backend is active

```bash
curl -s http://127.0.0.1:11435/api/version | jq .
# {"version":"0.1.0-dev"}

# More detail in the startup logs:
beeketd --log-level debug 2>&1 | head -20
```

---

## 10. Troubleshooting

### `engine: load llama library: ...`

The `libllama` shared library was not found. Fix:

```bash
# Check if the library is installed
ls /path/to/lib/

# Point Beeket at the directory containing the library
export YZMA_LIB=/path/to/lib

# Or auto-install
beeketd --auto-install-lib
```

### `go: requires go >= 1.22`

Update Go to 1.22 or later from https://go.dev/dl/.

### `bind: address already in use`

Port 11435 is taken. Either stop the existing process or use a different port:

```bash
# Find and kill the process using the port
lsof -i :11435
kill <PID>

# Or start on a different port
beeketd --port 11436
```

### `model smollm2:135m not found`

The model has not been pulled yet, or the reference is wrong:

```bash
beeket pull smollm2:135m
beeket list   # confirm it appears
```

### `scheduler: model X:Y queue full, try later`

The per-model request queue (depth 32) is full. This means the model is under heavy concurrent load. Wait and retry, or reduce concurrency.

### Slow generation on CPU

- Set `--gpu-layers -1` to offload as many layers as possible to GPU.
- Use a more aggressively quantized model (e.g. `Q4_K_M` instead of `Q8_0`).
- Reduce `--context-size` if you don't need a long context window.

### `permission denied` writing to data dir

```bash
ls -la ~/.local/share/beeket/
chmod -R u+rwX ~/.local/share/beeket/
```

Or point Beeket at a writable directory:

```bash
BEEKET_DATA_DIR=/tmp/beeket-data beeketd
```

### macOS: `Library not loaded: @rpath/libllama.dylib`

Add the library directory to the dynamic linker search path:

```bash
export DYLD_LIBRARY_PATH=/path/to/dir/containing/libllama.dylib:$DYLD_LIBRARY_PATH
```

### Linux: `error while loading shared libraries: libllama.so`

```bash
export LD_LIBRARY_PATH=/path/to/dir/containing/libllama.so:$LD_LIBRARY_PATH
# or permanently:
echo "/path/to/dir" | sudo tee /etc/ld.so.conf.d/beeket.conf
sudo ldconfig
```

### Getting more information

Start `beeketd` with `--log-level debug` to see detailed startup, model loading, and request processing logs:

```bash
beeketd --log-level debug --log-format json 2>&1 | jq .
```

Open an issue at https://github.com/baby-whales-pod/beeket/issues with the debug log output if you are stuck.
