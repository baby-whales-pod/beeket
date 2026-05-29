# Beeket — Pulling Models

This document describes how to use `beeket pull` to download GGUF models and explains all supported URL formats.

---

## Table of Contents

1. [Quick start](#1-quick-start)
2. [URL formats](#2-url-formats)
   - [Built-in alias](#21-built-in-alias)
   - [Short format with quantization tag](#22-short-format-with-quantization-tag-recommended)
   - [Explicit filename format](#23-explicit-filename-format)
   - [Direct HuggingFace URL](#24-direct-huggingface-url)
   - [Local file](#25-local-file)
3. [Filename guessing (short format)](#3-filename-guessing-short-format)
4. [Available quantizations](#4-available-quantizations)
5. [Managing pulled models](#5-managing-pulled-models)

---

## 1. Quick start

```bash
# Pull a model using the built-in alias (easiest)
beeket pull smollm2:135m

# Pull from Hugging Face with a quantization tag (recommended for HF models)
beeket pull hf.co/unsloth/Qwen3.5-0.8B-MTP-GGUF:Q4_K_M

# List what you have
beeket list
```

---

## 2. URL formats

### 2.1 Built-in alias

```
beeket pull <alias>
```

Beeket ships a small table of curated aliases so common models work out of the box.
Aliases point to **pre-verified exact download URLs** and bypass filename guessing entirely — using an alias is not the same as using the equivalent `hf.co/<org>/<repo>:<QUANT>` shorthand, which goes through the guesser.

| Alias | Model |
|---|---|
| `smollm2:135m` | QuantFactory/SmolLM2-135M-GGUF — Q4_K_M |
| `qwen2.5:0.5b` | Qwen/Qwen2.5-0.5B-Instruct-GGUF — Q4_K_M |
| `gemma3:1b` | google/gemma-3-1b-it-GGUF — Q4_K_M |
| `nomic-embed-text` | nomic-ai/nomic-embed-text-v1.5-GGUF — Q4_K_M |

Custom aliases can be defined in `~/.config/beeket/aliases.toml`.

---

### 2.2 Short format with quantization tag (recommended)

```
beeket pull hf.co/<org>/<repo>:<QUANT>
```

**Example:**

```bash
beeket pull hf.co/unsloth/Qwen3.5-0.8B-MTP-GGUF:Q4_K_M
```

Beeket takes the `<repo>` name, strips trailing format descriptors and the `-GGUF` suffix (see [§3](#3-filename-guessing-short-format)), and constructs the filename automatically:

```
hf.co/unsloth/Qwen3.5-0.8B-MTP-GGUF:Q4_K_M
  → repo:  Qwen3.5-0.8B-MTP-GGUF
  → base:  Qwen3.5-0.8B          (strips -MTP-GGUF)
  → file:  Qwen3.5-0.8B-Q4_K_M.gguf
  → URL:   https://huggingface.co/unsloth/Qwen3.5-0.8B-MTP-GGUF/resolve/main/Qwen3.5-0.8B-Q4_K_M.gguf
```

This is the **recommended format** for Hugging Face models — it is concise and lets you switch quantizations easily by changing only the tag.

---

### 2.3 Explicit filename format

```
beeket pull hf.co/<org>/<repo>/<filename>.gguf
```

**Example:**

```bash
beeket pull hf.co/unsloth/Qwen3.5-0.8B-MTP-GGUF/Qwen3.5-0.8B-Q4_K_M.gguf
```

Use this when you know the exact filename in the repository, for example when the filename doesn't match the pattern that Beeket's guesser produces, or when you want to pull a specific revision.

Resolves to:

```
https://huggingface.co/unsloth/Qwen3.5-0.8B-MTP-GGUF/resolve/main/Qwen3.5-0.8B-Q4_K_M.gguf
```

---

### 2.4 Direct HuggingFace URL

```
beeket pull https://huggingface.co/<org>/<repo>/resolve/main/<filename>.gguf
```

**Example:**

```bash
beeket pull https://huggingface.co/unsloth/Qwen3.5-0.8B-MTP-GGUF/resolve/main/Qwen3.5-0.8B-Q4_K_M.gguf
```

Any `https://` or `http://` URL pointing to a `.gguf` file is accepted. Copy the "Download" link from the Hugging Face model card directly.

---

### 2.5 Local file

> ⚠️ **Not yet implemented** — `file://` import is planned but not functional in the current version.

```
beeket pull file:///absolute/path/to/model.gguf
```

---

## 3. Filename guessing (short format)

When you use the `hf.co/<org>/<repo>:<QUANT>` short format, Beeket guesses the filename inside the repository by stripping trailing descriptors from the repo name.

**Rules:**

- The trailing `-GGUF` suffix is always removed.
- An optional purely-alphabetic segment immediately before `-GGUF` is also removed (e.g. `-MTP`, `-Instruct`, `-Chat`).
- Segments that contain digits are **preserved** — they encode the model size or version (e.g. `-135M`, `-0.8B`, `-3B`).
- The quantization tag is appended with a `-` separator, followed by `.gguf`.

**Examples:**

| Repo name | Quant | Stripped base | Resulting filename |
|---|---|---|---|
| `Qwen3.5-0.8B-MTP-GGUF` | `Q4_K_M` | `Qwen3.5-0.8B` | `Qwen3.5-0.8B-Q4_K_M.gguf` |
| `SmolLM2-135M-GGUF` | `Q4_K_M` | `SmolLM2-135M` | `SmolLM2-135M-Q4_K_M.gguf` |
| `Qwen2.5-0.5B-Instruct-GGUF` | `Q8_0` | `Qwen2.5-0.5B` | `Qwen2.5-0.5B-Q8_0.gguf` |
| `Mistral-7B-v0.2-GGUF` | `Q4_K_M` | `Mistral-7B-v0.2` | `Mistral-7B-v0.2-Q4_K_M.gguf` |

> **Known limitation:** the guesser strips any purely-alphabetic segment immediately before `-GGUF` (e.g. `-it`, `-instruct`, `-chat`). If the actual filename on HuggingFace retains that segment, the guessed URL will 404. In that case, use the [explicit filename format](#23-explicit-filename-format) or a [direct URL](#24-direct-huggingface-url) instead.

---

## 4. Available quantizations

Common quantization schemes available on Hugging Face GGUF repos. Lower bit-widths use less RAM at the cost of some quality.

| Quantization | Bits | Quality | ~Size (0.8B model) | Recommended use |
|---|---|---|---|---|
| `BF16` / `F16` | 16 | Highest | ~1.6 GB | Reference / evaluation (repo-dependent: some ship BF16, others F16) |
| `Q8_0` | 8 | Very high | ~850 MB | When RAM allows |
| `Q6_K` | 6 | High | ~660 MB | High-quality, moderate RAM |
| `Q5_K_M` | 5 | Good | ~610 MB | Good balance |
| **`Q4_K_M`** | **4** | **Good** | **~550 MB** | **Recommended default** |
| `Q4_K_S` | 4 | Slightly lower | ~520 MB | Tighter memory budgets |
| `Q3_K_M` | 3 | Acceptable | ~430 MB | Very constrained RAM |

> **Tip:** `Q4_K_M` is the default when no quantization is specified (`hf.co/<org>/<repo>` with no `:<QUANT>` tag). It offers a good quality-to-size trade-off on most hardware.

Not all quantizations are available for every model. Check the model's Hugging Face repository for the files it ships.

---

## 5. Managing pulled models

```bash
# List all installed models
beeket list

# Show details for one model (size, quantization, context length, …)
beeket show smollm2:135m

# Remove a model from disk
beeket rm smollm2:135m
```

> **Note:** Aliasing and copying models is not yet implemented.

Models are stored as content-addressed blobs under `~/.local/share/beeket/blobs/`.

See the [Setup Guide](./SETUP.md) for server configuration and hardware-acceleration options.
