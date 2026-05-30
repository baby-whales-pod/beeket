# Embeddings

`beeket` supports generating embedding vectors via `POST /api/embeddings` (and the Ollama alias `POST /api/embed`).

## What are embeddings?

Embeddings are dense numerical representations of text, useful for:
- Semantic search / similarity ranking
- Retrieval-augmented generation (RAG)
- Clustering and classification

## Recommended models

Use a model designed for embeddings — general-purpose chat models produce low-quality vectors.

| Model | Notes |
|-------|-------|
| `nomic-embed-text:latest` | Good balance of quality and speed; 768-dim |
| `mxbai-embed-large:latest` | High quality; 1024-dim |
| `all-minilm:latest` | Compact; 384-dim, fast on CPU |
| `bge-large:latest` | Strong multilingual support |

Pull a model first:
```bash
beeket pull hf.co/nomic-ai/nomic-embed-text-v1.5-GGUF:Q8_0
```

## API reference

### `POST /api/embeddings` · `POST /api/embed`

**Request:**

```json
{
  "model":  "nomic-embed-text:latest",
  "input":  "The sky is blue"
}
```

`input` accepts a single string **or** an array of strings:

```json
{
  "model": "nomic-embed-text:latest",
  "input": ["The sky is blue", "Grass is green"]
}
```

Legacy single-input style (Ollama v1 compat):
```json
{
  "model": "nomic-embed-text:latest",
  "prompt": "The sky is blue"
}
```

**Response:**

```json
{
  "model": "nomic-embed-text:latest",
  "embeddings": [[0.012, -0.034, ...]],
  "total_duration": 4512345,
  "prompt_eval_count": 6
}
```

- `embeddings` — array of float32 arrays, one per input. Vectors are **L2-normalised**.
- `total_duration` — nanoseconds including model load time.
- `prompt_eval_count` — total tokens processed across all inputs.

## curl examples

### Single input

```bash
curl http://127.0.0.1:11435/api/embeddings \
  -d '{"model":"nomic-embed-text:latest","input":"Hello world"}'
```

### Batch input

```bash
curl http://127.0.0.1:11435/api/embeddings \
  -d '{
    "model": "nomic-embed-text:latest",
    "input": ["cat", "dog", "automobile"]
  }' | jq '.embeddings | length'
```

### Cosine similarity (vectors are already L2-normalised, so dot-product = cosine)

```bash
# Get two vectors and compute dot product with jq
A=$(curl -s http://127.0.0.1:11435/api/embeddings \
      -d '{"model":"nomic-embed-text:latest","input":"king"}' \
    | jq '.embeddings[0]')
B=$(curl -s http://127.0.0.1:11435/api/embeddings \
      -d '{"model":"nomic-embed-text:latest","input":"queen"}' \
    | jq '.embeddings[0]')
# dot product with Python
python3 -c "
import sys, json
a, b = $A, $B
print(sum(x*y for x,y in zip(a,b)))
"
```

## Limitations

- **Truncation is not implemented in v0.1.** If the input exceeds the model's context window, the request returns an error. Truncation (`truncate: true`) will be added in a future release.
- **No batched encoding in v0.1.** Multiple strings in `input` are encoded sequentially. A batched path (single decode with multiple sequence IDs) will be added later.
- **Quality depends on the model.** Chat models technically produce embedding vectors but the quality is poor compared to dedicated embedding models.
- The embedding worker uses a separate context (`Embeddings=1`, `PoolingType=mean`) from the generation worker for the same model. Both count against `--max-loaded-models`.

## Troubleshooting

### "failed to initialize model" error

This usually means either:
1. **Model not pulled**: run `beeket pull nomic-embed-text` first
2. **Context size too large**: start the server with a smaller context: `beeket serve --context-size 512`

The embedding context allocates memory proportional to `--context-size`. For embedding models, a small context (512–1024) is sufficient.
