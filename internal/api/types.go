// Package api contains the Beeket HTTP API types (Ollama-compatible wire format).
package api

import "time"

// --- Model management types ---

// PullRequest is the body for POST /api/pull.
type PullRequest struct {
	Name   string `json:"name"`
	Stream *bool  `json:"stream,omitempty"`
}

// PullResponse is one line of the NDJSON stream for pull progress.
type PullResponse struct {
	Status    string `json:"status"`
	Digest    string `json:"digest,omitempty"`
	Total     int64  `json:"total,omitempty"`
	Completed int64  `json:"completed,omitempty"`
}

// DeleteRequest is the body for DELETE /api/delete.
type DeleteRequest struct {
	Name string `json:"name"`
}

// ShowRequest is the body for POST /api/show.
type ShowRequest struct {
	Name string `json:"name"`
}

// ShowResponse is returned by POST /api/show.
type ShowResponse struct {
	Name      string         `json:"name"`
	Details   ModelDetails   `json:"details"`
	ModelInfo map[string]any `json:"model_info,omitempty"`
}

// ModelDetails is embedded in list and show responses.
type ModelDetails struct {
	Family            string `json:"family"`
	ParameterSize     string `json:"parameter_size"`
	QuantizationLevel string `json:"quantization_level"`
	ContextLength     int    `json:"context_length"`
	Format            string `json:"format"`
}

// ModelInfo is one item in the /api/tags list.
type ModelInfo struct {
	Name       string       `json:"name"`
	Model      string       `json:"model"`
	Size       int64        `json:"size"`
	Digest     string       `json:"digest"`
	ModifiedAt time.Time    `json:"modified_at"`
	Details    ModelDetails `json:"details"`
}

// TagsResponse is the response for GET /api/tags.
type TagsResponse struct {
	Models []ModelInfo `json:"models"`
}

// CopyRequest is the body for POST /api/copy.
type CopyRequest struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
}

// --- Inference types ---

// ToolCallFunction is the function portion of a tool call.
type ToolCallFunction struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

// ToolCall is a single tool invocation emitted by the model.
type ToolCall struct {
	Function ToolCallFunction `json:"function"`
}

// ToolFunction describes the callable function inside a tool definition.
type ToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"` // JSON schema
}

// Tool is a function definition provided by the client.
type Tool struct {
	Type     string       `json:"type"` // "function"
	Function ToolFunction `json:"function"`
}

// Message is a chat message (role + content).
type Message struct {
	Role      string     `json:"role"`
	Content   string     `json:"content"`
	Images    []string   `json:"images,omitempty"`     // base64-encoded
	ToolCalls []ToolCall `json:"tool_calls,omitempty"` // populated for assistant tool-call messages
	ToolName  string     `json:"tool_name,omitempty"`  // populated for role=tool result messages
}

// Options holds per-request sampler and runtime overrides.
// Field names and semantics match the Ollama API.
type Options struct {
	// Sampler parameters.
	Temperature float32  `json:"temperature,omitempty"`
	TopK        int32    `json:"top_k,omitempty"`
	TopP        float32  `json:"top_p,omitempty"`
	MinP        float32  `json:"min_p,omitempty"`
	TypicalP    float32  `json:"typical_p,omitempty"`
	TfsZ        float32  `json:"tfs_z,omitempty"` // Tail Free Sampling (no-op: not in yzma v1.13)

	// Repetition / penalty parameters.
	RepeatPenalty    float32 `json:"repeat_penalty,omitempty"`
	RepeatLastN      int32   `json:"repeat_last_n,omitempty"`   // window for repeat penalty; -1 = full context
	FrequencyPenalty float32 `json:"frequency_penalty,omitempty"`
	PresencePenalty  float32 `json:"presence_penalty,omitempty"`
	PenalizeNewline  *bool   `json:"penalize_newline,omitempty"` // accepted for compat; no-op

	// Mirostat parameters (mutually exclusive with TopK/TopP; Mirostat > 0 enables).
	Mirostat    int32   `json:"mirostat,omitempty"`    // 0=off, 1=Mirostat v1, 2=Mirostat v2
	MirostatTau float32 `json:"mirostat_tau,omitempty"` // target entropy (default 5.0)
	MirostatEta float32 `json:"mirostat_eta,omitempty"` // learning rate (default 0.1)

	// Generation limits.
	NumPredict int      `json:"num_predict,omitempty"` // max tokens to generate; -1 = unlimited
	Stop       []string `json:"stop,omitempty"`
	Seed       uint32   `json:"seed,omitempty"`

	// Runtime hints (accepted for Ollama compatibility; applied at session level).
	NumCtx    int    `json:"num_ctx,omitempty"`    // per-request context window (no-op: set at load time)
	NumThread int    `json:"num_thread,omitempty"` // no-op: set at server start
	NumGPU    int    `json:"num_gpu,omitempty"`    // no-op: set at server start
	KeepAlive string `json:"keep_alive,omitempty"` // no-op: handled by scheduler
}

// GenerateRequest is the body for POST /api/generate.
// Format, when set, constrains the model output to valid JSON.
//   - "json" (string): any valid JSON object.
//   - A JSON Schema object: output is constrained to match the schema.
type GenerateRequest struct {
	Model   string   `json:"model"`
	Prompt  string   `json:"prompt"`
	System  string   `json:"system,omitempty"`
	Stream  *bool    `json:"stream,omitempty"`
	Options *Options `json:"options,omitempty"`
	Format  any      `json:"format,omitempty"`
}

// GenerateResponse is one NDJSON line for /api/generate.
type GenerateResponse struct {
	Model           string `json:"model"`
	CreatedAt       string `json:"created_at"`
	Response        string `json:"response"`
	Done            bool   `json:"done"`
	TotalDuration   int64  `json:"total_duration,omitempty"`
	LoadDuration    int64  `json:"load_duration,omitempty"`
	PromptEvalCount int    `json:"prompt_eval_count,omitempty"`
	EvalCount       int    `json:"eval_count,omitempty"`
	EvalDuration    int64  `json:"eval_duration,omitempty"`
}

// ChatRequest is the body for POST /api/chat.
// Format, when set, constrains the model output to valid JSON.
//   - "json" (string): any valid JSON object.
//   - A JSON Schema object: output is constrained to match the schema.
type ChatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Tools    []Tool    `json:"tools,omitempty"`
	Stream   *bool     `json:"stream,omitempty"`
	Format   any       `json:"format,omitempty"` // "json" or JSON schema
	Options  *Options  `json:"options,omitempty"`
}

// ChatResponse is one NDJSON line for /api/chat.
type ChatResponse struct {
	Model         string  `json:"model"`
	CreatedAt     string  `json:"created_at"`
	Message       Message `json:"message"`
	Done          bool    `json:"done"`
	DoneReason    string  `json:"done_reason,omitempty"`
	TotalDuration int64   `json:"total_duration,omitempty"`
	EvalCount     int     `json:"eval_count,omitempty"`
	EvalDuration  int64   `json:"eval_duration,omitempty"`
}

// EmbeddingsRequest is the body for POST /api/embeddings and POST /api/embed.
type EmbeddingsRequest struct {
	Model     string   `json:"model"`
	Input     any      `json:"input,omitempty"`    // string or []string (new style)
	Prompt    string   `json:"prompt,omitempty"`   // legacy single-input
	Truncate  *bool    `json:"truncate,omitempty"` // default true (not implemented in v0.1)
	KeepAlive string   `json:"keep_alive,omitempty"`
	Options   *Options `json:"options,omitempty"`
}

// EmbeddingsResponse is returned by POST /api/embeddings / POST /api/embed.
type EmbeddingsResponse struct {
	Model           string      `json:"model"`
	Embeddings      [][]float32 `json:"embeddings"`
	TotalDuration   int64       `json:"total_duration,omitempty"`
	LoadDuration    int64       `json:"load_duration,omitempty"`
	PromptEvalCount int         `json:"prompt_eval_count,omitempty"`
}

// --- Operational types ---

// VersionResponse is returned by GET /api/version.
type VersionResponse struct {
	Version string `json:"version"`
}

// PSModel is one entry in /api/ps.
type PSModel struct {
	Name     string    `json:"name"`
	Size     int64     `json:"size"`
	LastUsed time.Time `json:"expires_at"`
}

// PSResponse is returned by GET /api/ps.
type PSResponse struct {
	Models []PSModel `json:"models"`
}

// ErrorResponse is the standard error body.
type ErrorResponse struct {
	Error string `json:"error"`
}
