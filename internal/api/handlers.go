// Package api — request handlers.
package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/baby-whales-pod/beeket/internal/download"
	"github.com/baby-whales-pod/beeket/internal/engine"
	"github.com/baby-whales-pod/beeket/internal/jsongrammar"
	"github.com/baby-whales-pod/beeket/internal/metrics"
	"github.com/baby-whales-pod/beeket/internal/models"
	"github.com/baby-whales-pod/beeket/internal/scheduler"
	"github.com/baby-whales-pod/beeket/internal/store"
	"github.com/baby-whales-pod/beeket/internal/tools"
	"github.com/baby-whales-pod/beeket/internal/version"
	"github.com/hybridgroup/yzma/pkg/llama"
)

// embedScheduler is the interface the Embeddings handler needs from the scheduler.
// Using an interface here allows the handler to be tested with a fake.
type embedScheduler interface {
	Embed(ctx context.Context, name, tag, input string) ([]float32, int, error)
}

// mgrResolver is the interface Embeddings needs from the model manager.
type mgrResolver interface {
	Resolve(ref string) (string, string)
}

// generatorScheduler is the interface the Handler uses from the scheduler for
// generation. Using an interface allows Chat/Generate handler tests to inject a fake.
type generatorScheduler interface {
	Generate(ctx context.Context, name, tag, prompt string, opts engine.GenerateOptions, out func(string) error) error
	LoadedModels() []scheduler.LoadedInfo
}

// promptBuilderFunc converts a message list to a prompt string.
// Injectable for testing so tests don't require the llama FFI library.
type promptBuilderFunc func(msgs []Message) (string, error)

// Handler holds all dependencies for API handlers.
type Handler struct {
	mgr           *models.Manager
	embedMgr      mgrResolver // points to mgr unless overridden in tests
	store         *store.Store
	sched         generatorScheduler
	embedSched    embedScheduler // set to sched unless overridden in tests
	ready         bool
	startTime     time.Time
	backend       string
	maxLoaded     int
	numParallel   int
	promptBuilder promptBuilderFunc // defaults to buildChatPrompt; injectable for tests
}

// HandlerConfig carries optional configuration for NewHandler.
type HandlerConfig struct {
	StartTime   time.Time
	Backend     string
	MaxLoaded   int
	NumParallel int
}

// NewHandler creates a Handler.
func NewHandler(mgr *models.Manager, st *store.Store, sched *scheduler.Scheduler) *Handler {
	return NewHandlerWithConfig(mgr, st, sched, HandlerConfig{StartTime: time.Now()})
}

// NewHandlerWithConfig creates a Handler with explicit runtime config for the status endpoint.
func NewHandlerWithConfig(mgr *models.Manager, st *store.Store, sched *scheduler.Scheduler, cfg HandlerConfig) *Handler {
	if cfg.StartTime.IsZero() {
		cfg.StartTime = time.Now()
	}
	return &Handler{
		mgr:           mgr,
		embedMgr:      mgr,
		store:         st,
		sched:         sched,
		embedSched:    sched,
		ready:         true,
		startTime:     cfg.StartTime,
		backend:       cfg.Backend,
		maxLoaded:     cfg.MaxLoaded,
		numParallel:   cfg.NumParallel,
		promptBuilder: buildChatPrompt,
	}
}

// ---- Model management ----

// Pull handles POST /api/pull — downloads a model and streams progress.
func (h *Handler) Pull(w http.ResponseWriter, r *http.Request) {
	var req PullRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	stream := req.Stream == nil || *req.Stream
	nw := NewNDJSONWriter(w)

	emit := func(status, digest string, total, completed int64) {
		nw.Write(PullResponse{ //nolint:errcheck
			Status:    status,
			Digest:    digest,
			Total:     total,
			Completed: completed,
		})
	}

	// Resolve model name → clean (name, tag) registry key + download URL.
	// mgr.Resolve now calls CleanModelRef internally, so the result is always
	// slash-free and safe for the manifest store.
	registryName, registryTag := h.mgr.Resolve(req.Name)
	emit("resolving manifest", "", 0, 0)

	var dlURL string
	if entry := h.mgr.AliasLookup(req.Name); entry != nil {
		dlURL = entry.Source
	} else {
		var err error
		dlURL, err = download.Resolve(req.Name)
		if err != nil {
			emit(fmt.Sprintf("error: %v", err), "", 0, 0)
			return
		}
	}

	emit("downloading", "", 0, 0)

	// Download to a tmp path first, then content-address.
	// TmpFilename derives a safe flat filename from the download URL,
	// avoiding double-.gguf extensions and URL-as-path bugs.
	tmpPath := h.store.TmpPath(download.TmpFilename(dlURL))

	progress := func(downloaded, total int64) {
		if stream {
			emit("downloading", "", total, downloaded)
		}
	}

	digest, err := download.Get(r.Context(), dlURL, tmpPath, progress)
	if err != nil {
		emit(fmt.Sprintf("error: %v", err), "", 0, 0)
		return
	}

	// Move blob to content-addressed location.
	finalBlob := h.store.BlobPath(digest)
	if !h.store.BlobExists(digest) {
		if err := os.Rename(tmpPath, finalBlob); err != nil {
			emit(fmt.Sprintf("error renaming blob: %v", err), "", 0, 0)
			return
		}
	} else {
		os.Remove(tmpPath) //nolint:errcheck
	}

	emit("verifying sha256", "sha256:"+digest, 0, 0)

	// Determine file size.
	var blobSize int64
	if fi, err := os.Stat(finalBlob); err == nil {
		blobSize = fi.Size()
	}

	// Save manifest using the clean slash-free registry key.
	mf := &models.Manifest{
		Name:       registryName,
		Tag:        registryTag,
		Digest:     digest,
		Size:       blobSize,
		Source:     dlURL,
		ModifiedAt: time.Now(),
		Details: models.Details{
			Format: "gguf",
		},
	}
	if err := h.mgr.Save(mf); err != nil {
		emit(fmt.Sprintf("error saving manifest: %v", err), "", 0, 0)
		return
	}

	emit("success", "sha256:"+digest, 0, 0)
}

// Tags handles GET /api/tags — lists installed models.
func (h *Handler) Tags(w http.ResponseWriter, r *http.Request) {
	manifests, err := h.mgr.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	resp := TagsResponse{Models: make([]ModelInfo, 0, len(manifests))}
	for _, mf := range manifests {
		resp.Models = append(resp.Models, manifestToInfo(mf))
	}
	writeJSON(w, http.StatusOK, resp)
}

// Show handles POST /api/show — returns details for one model.
func (h *Handler) Show(w http.ResponseWriter, r *http.Request) {
	var req ShowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	name, tag := h.mgr.Resolve(req.Name)
	mf, err := h.mgr.Get(name, tag)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, ShowResponse{
		Name:    mf.FullName(),
		Details: detailsFromManifest(mf),
	})
}

// Delete handles DELETE /api/delete — removes a model.
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	var req DeleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	name, tag := h.mgr.Resolve(req.Name)
	if err := h.mgr.Delete(name, tag); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}

// Copy handles POST /api/copy — creates an alias manifest.
func (h *Handler) Copy(w http.ResponseWriter, r *http.Request) {
	var req CopyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	srcName, srcTag := h.mgr.Resolve(req.Source)
	mf, err := h.mgr.Get(srcName, srcTag)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("source model %q not found", req.Source))
		return
	}
	dstName, dstTag := h.mgr.Resolve(req.Destination)
	newMF := *mf
	newMF.Name = dstName
	newMF.Tag = dstTag
	newMF.ModifiedAt = time.Now()
	if err := h.mgr.Save(&newMF); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}

// ---- Inference ----

// Generate handles POST /api/generate.
func (h *Handler) Generate(w http.ResponseWriter, r *http.Request) {
	var req GenerateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Model == "" {
		writeError(w, http.StatusBadRequest, "model is required")
		return
	}

	stream := req.Stream == nil || *req.Stream
	name, tag := h.mgr.Resolve(req.Model)
	modelKey := name + ":" + tag
	opts := buildGenerateOptions(req.Options)

	// Resolve grammar constraint from the format field.
	grammarStr, schemaToValidateGen, fmtErr := resolveFormat(req.Format)
	if fmtErr != nil {
		writeError(w, http.StatusBadRequest, fmtErr.Error())
		return
	}
	// Grammar constraint is intentionally NOT set on opts.GrammarStr.
	// llama.cpp's GGML_ABORT (triggered when the grammar eliminates all token
	// candidates) sends SIGABRT which cannot be caught by Go's recover().
	// Instead we rely on the system-prompt injection (noThinkWithJSON) to guide
	// the model, and validate the response against the schema after generation.

	// Suppress thinking mode when structured output is requested or think:false.
	// Per Qwen3 docs: /no_think must be appended to the user (prompt) content.
	// For structured output we also inject a JSON-only system instruction.
	needNoThinkGen := (grammarStr != "") || (req.Think != nil && !*req.Think)
	systemMsg := req.System
	if needNoThinkGen && grammarStr != "" {
		// Structured output: JSON-only instruction in system, /no_think in prompt.
		if systemMsg == "" {
			systemMsg = jsonSystemPrompt
		} else {
			systemMsg = jsonSystemPrompt + "\n" + systemMsg
		}
	}
	if needNoThinkGen {
		opts.StopStrings = append(opts.StopStrings, "</think>")
	}

	prompt := req.Prompt
	if needNoThinkGen {
		// Append /no_think to user prompt (Qwen3 docs requirement).
		prompt = prompt + " " + noThinkOnly
	}
	if systemMsg != "" {
		prompt = systemMsg + "\n\n" + prompt
	}

	start := time.Now()
	nw := NewNDJSONWriter(w)
	evalCount := 0
	var firstTokenAt time.Time
	var responseBuilder strings.Builder // collect full response for schema validation

	genErr := h.sched.Generate(r.Context(), name, tag, prompt, opts, func(piece string) error {
		if evalCount == 0 {
			firstTokenAt = time.Now()
		}
		evalCount++
		responseBuilder.WriteString(piece)
		if stream {
			return nw.Write(GenerateResponse{
				Model:     req.Model,
				CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
				Response:  piece,
				Done:      false,
			})
		}
		return nil
	})

	dur := time.Since(start)

	// Record inference metrics.
	outcome := inferenceOutcome(r.Context(), genErr)
	metrics.InferenceRequestsTotal.WithLabelValues(modelKey, "generate", outcome).Inc()
	metrics.InferenceDuration.WithLabelValues(modelKey).Observe(dur.Seconds())
	if !firstTokenAt.IsZero() {
		metrics.InferenceTTFT.WithLabelValues(modelKey).Observe(firstTokenAt.Sub(start).Seconds())
	}
	metrics.InferenceEvalTokensTotal.WithLabelValues(modelKey).Add(float64(evalCount))

	if genErr != nil && r.Context().Err() == nil {
		writeError(w, http.StatusInternalServerError, genErr.Error())
		return
	}

	// Validate response against JSON schema if one was provided.
	if schemaToValidateGen != nil {
		if vErr := jsongrammar.ValidateSchema(schemaToValidateGen, strings.TrimSpace(responseBuilder.String())); vErr != nil {
			slog.Warn("generate: schema validation failed, returning error", "err", vErr)
			writeError(w, http.StatusUnprocessableEntity, "response did not match the requested JSON schema: "+vErr.Error())
			return
		}
	}

	total := dur.Nanoseconds()
	nw.Write(GenerateResponse{ //nolint:errcheck
		Model:         req.Model,
		CreatedAt:     time.Now().UTC().Format(time.RFC3339Nano),
		Response:      "",
		Done:          true,
		TotalDuration: total,
		EvalCount:     evalCount,
		EvalDuration:  total,
	})
}

// Chat handles POST /api/chat.
func (h *Handler) Chat(w http.ResponseWriter, r *http.Request) {
	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Model == "" {
		writeError(w, http.StatusBadRequest, "model is required")
		return
	}

	// Convert tools if present.
	hasTools := len(req.Tools) > 0
	var toolsList []tools.Tool
	if hasTools {
		for _, t := range req.Tools {
			toolsList = append(toolsList, tools.Tool{
				Type: t.Type,
				Function: tools.ToolFunction{
					Name:        t.Function.Name,
					Description: t.Function.Description,
					Parameters:  t.Function.Parameters,
				},
			})
		}
	}

	// Convert messages for tool-role rewriting.
	msgsForTools := make([]tools.Message, len(req.Messages))
	for i, m := range req.Messages {
		msgsForTools[i] = tools.Message{
			Role:     m.Role,
			Content:  m.Content,
			ToolName: m.ToolName,
		}
	}

	// Build the effective message list:
	// - If tools are present: inject tool preface into system message and
	//   rewrite tool-role messages to user-role.
	// - Otherwise: pass through as-is.
	var effectiveMsgs []Message
	if hasTools {
		rewritten := tools.RewriteToolMessages(msgsForTools)
		preface := tools.RenderToolPreface(toolsList)

		// Find (or create) the system message and prepend the tool preface.
		foundSystem := false
		for _, m := range rewritten {
			if m.Role == "system" && !foundSystem {
				effectiveMsgs = append(effectiveMsgs, Message{
					Role:    "system",
					Content: preface + m.Content,
				})
				foundSystem = true
			} else {
				effectiveMsgs = append(effectiveMsgs, Message{Role: m.Role, Content: m.Content})
			}
		}
		if !foundSystem {
			// No system message present — inject one at the front.
			newMsgs := make([]Message, 0, len(effectiveMsgs)+1)
			newMsgs = append(newMsgs, Message{Role: "system", Content: preface})
			newMsgs = append(newMsgs, effectiveMsgs...)
			effectiveMsgs = newMsgs
		}
	} else {
		effectiveMsgs = req.Messages
	}

	// Resolve grammar and thinking suppression BEFORE building the prompt,
	// because injectNoThink modifies effectiveMsgs (adds/prepends system message).
	var chatGrammarStr string
	var schemaToValidateChat map[string]any
	if hasTools {
		// Grammar will be re-resolved below after opts is built; skip here.
	} else {
		g, sc, gErr := resolveFormat(req.Format)
		if gErr != nil {
			writeError(w, http.StatusBadRequest, gErr.Error())
			return
		}
		chatGrammarStr = g
		schemaToValidateChat = sc
	}

	// Suppress thinking mode when:
	//   a) structured output is requested (format != nil), OR
	//   b) the caller explicitly sets think:false.
	// Modifies effectiveMsgs, so must happen before h.chatPrompt().
	needNoThink := (chatGrammarStr != "") || (req.Think != nil && !*req.Think)
	var chatOpts engine.GenerateOptions // built below; passed by pointer to injectNoThink
	if needNoThink {
		// withJSON=true only when structured output is requested, not for bare think:false.
		effectiveMsgs = injectNoThink(effectiveMsgs, &chatOpts, chatGrammarStr != "")
	}

	// Build the message list for the engine's native chat template.
	// When engMsgs is set, the engine calls s.ApplyChatTemplate() using the
	// model's own GGUF template (e.g. "qwen3") which correctly handles
	// thinking-model control variables like enable_thinking.
	// For tool-calling requests, we still build the prompt string via chatPrompt
	// because tool prompts go through a different code path (Grammar + GrammarLazy)
	// that relies on the pre-built prompt string.
	engMsgs := make([]engine.ChatMessage, len(effectiveMsgs))
	for i, m := range effectiveMsgs {
		engMsgs[i] = engine.ChatMessage{Role: m.Role, Content: m.Content}
	}

	// Only call chatPrompt for tool-calling requests (where the pre-built prompt
	// string is still needed) or when engMsgs is empty (fallback).
	// For plain chat and structured output, the engine builds the prompt from
	// opts.Messages via s.ApplyChatTemplate — calling chatPrompt here would be
	// dead work since that result is discarded.
	var prompt string
	if hasTools || len(engMsgs) == 0 {
		var pErr error
		prompt, pErr = h.chatPrompt(effectiveMsgs)
		if pErr != nil {
			writeError(w, http.StatusBadRequest, pErr.Error())
			return
		}
	}

	stream := req.Stream == nil || *req.Stream
	// When tools are present, buffer all tokens and deliver atomically
	// (streaming is not supported for tool calls in v0.1).
	if hasTools {
		stream = false
	}

	name, tag := h.mgr.Resolve(req.Model)
	modelKey := name + ":" + tag
	opts := buildGenerateOptions(req.Options)
	// Merge any stop strings injected by injectNoThink (e.g. "</think>").
	opts.StopStrings = append(opts.StopStrings, chatOpts.StopStrings...)
	opts.Messages = engMsgs

	if hasTools {
		// Tool calling: use Grammar+GrammarLazy (lazy trigger).
		grammarStr, lazyTrigger, gErr := tools.BuildGrammar(toolsList)
		if gErr != nil {
			writeError(w, http.StatusBadRequest, "invalid tool schema: "+gErr.Error())
			return
		}
		opts.Grammar = grammarStr
		opts.GrammarLazy = []string{lazyTrigger}
	}

	start := time.Now()
	nw := NewNDJSONWriter(w)
	var sb strings.Builder
	evalCount := 0
	var firstTokenAt time.Time

	genErr := h.sched.Generate(r.Context(), name, tag, prompt, opts, func(piece string) error {
		if evalCount == 0 {
			firstTokenAt = time.Now()
		}
		evalCount++
		sb.WriteString(piece)
		if stream {
			return nw.Write(ChatResponse{
				Model:     req.Model,
				CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
				Message:   Message{Role: "assistant", Content: piece},
				Done:      false,
			})
		}
		return nil
	})

	dur := time.Since(start)

	// Record inference metrics.
	outcome := inferenceOutcome(r.Context(), genErr)
	metrics.InferenceRequestsTotal.WithLabelValues(modelKey, "chat", outcome).Inc()
	metrics.InferenceDuration.WithLabelValues(modelKey).Observe(dur.Seconds())
	if !firstTokenAt.IsZero() {
		metrics.InferenceTTFT.WithLabelValues(modelKey).Observe(firstTokenAt.Sub(start).Seconds())
	}
	metrics.InferenceEvalTokensTotal.WithLabelValues(modelKey).Add(float64(evalCount))

	if genErr != nil && r.Context().Err() == nil {
		writeError(w, http.StatusInternalServerError, genErr.Error())
		return
	}

	output := sb.String()
	total := dur.Nanoseconds()

	// Validate response against JSON schema if one was provided (non-tool-call path).
	if schemaToValidateChat != nil && !hasTools {
		if vErr := jsongrammar.ValidateSchema(schemaToValidateChat, strings.TrimSpace(output)); vErr != nil {
			slog.Warn("chat: schema validation failed, returning error", "err", vErr)
			writeError(w, http.StatusUnprocessableEntity, "response did not match the requested JSON schema: "+vErr.Error())
			return
		}
	}

	// Attempt to parse a tool call when tools were requested.
	if hasTools {
		if tc, ok := tools.ParseToolCall(output); ok {
			toolCallMsg := Message{
				Role: "assistant",
				ToolCalls: []ToolCall{
					{
						Function: ToolCallFunction{
							Name:      tc.Name,
							Arguments: tc.Arguments,
						},
					},
				},
			}
			nw.Write(ChatResponse{ //nolint:errcheck
				Model:         req.Model,
				CreatedAt:     time.Now().UTC().Format(time.RFC3339Nano),
				Message:       toolCallMsg,
				Done:          true,
				DoneReason:    "tool_calls",
				TotalDuration: total,
				EvalCount:     evalCount,
				EvalDuration:  total,
			})
			return
		}
		// No tool call parsed — fall through and return as plain content.
	}

	nw.Write(ChatResponse{ //nolint:errcheck
		Model:         req.Model,
		CreatedAt:     time.Now().UTC().Format(time.RFC3339Nano),
		Message:       Message{Role: "assistant", Content: output},
		Done:          true,
		TotalDuration: total,
		EvalCount:     evalCount,
		EvalDuration:  total,
	})
}

// Embeddings handles POST /api/embeddings and POST /api/embed.
func (h *Handler) Embeddings(w http.ResponseWriter, r *http.Request) {
	var req EmbeddingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Model == "" {
		writeError(w, http.StatusBadRequest, "model is required")
		return
	}

	// Normalise inputs: support string, []string (new style) and legacy "prompt".
	// Note: Truncate, KeepAlive, and Options fields are accepted for Ollama API
	// compatibility but not yet implemented in v0.1. Truncate defaults to true
	// per the Ollama spec, but inputs exceeding the context window will currently
	// return an error from the engine layer rather than being silently truncated.
	var inputs []string
	switch v := req.Input.(type) {
	case string:
		if v == "" {
			writeError(w, http.StatusBadRequest, "input must not be empty")
			return
		}
		inputs = []string{v}
	case []any:
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				writeError(w, http.StatusBadRequest, "input array must contain only strings")
				return
			}
			if s == "" {
				writeError(w, http.StatusBadRequest, "input array must not contain empty strings")
				return
			}
			inputs = append(inputs, s)
		}
	case nil:
		// fall through to legacy prompt below
	default:
		writeError(w, http.StatusBadRequest, "input must be a string or array of strings")
		return
	}
	// Legacy single-input via "prompt" field.
	if len(inputs) == 0 && req.Prompt != "" {
		inputs = []string{req.Prompt}
	}
	if len(inputs) == 0 {
		writeError(w, http.StatusBadRequest, "input or prompt is required")
		return
	}

	name, tag := h.embedMgr.Resolve(req.Model)
	modelKey := name + ":" + tag
	start := time.Now()

	vecs := make([][]float32, 0, len(inputs))
	totalTokens := 0
	for _, text := range inputs {
		vec, n, err := h.embedSched.Embed(r.Context(), name, tag, text)
		if err != nil {
			outcome := inferenceOutcome(r.Context(), err)
			metrics.InferenceRequestsTotal.WithLabelValues(modelKey, "embed", outcome).Inc()
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		vecs = append(vecs, vec)
		totalTokens += n
	}

	metrics.InferenceRequestsTotal.WithLabelValues(modelKey, "embed", metrics.OutcomeSuccess).Inc()
	metrics.InferenceEvalTokensTotal.WithLabelValues(modelKey).Add(float64(totalTokens))

	writeJSON(w, http.StatusOK, EmbeddingsResponse{
		Model:           req.Model,
		Embeddings:      vecs,
		TotalDuration:   time.Since(start).Nanoseconds(),
		PromptEvalCount: totalTokens,
	})
}

// ---- Operational ----

// Version handles GET /api/version.
func (h *Handler) Version(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, VersionResponse{Version: version.Version})
}

// PS handles GET /api/ps.
func (h *Handler) PS(w http.ResponseWriter, r *http.Request) {
	loaded := h.sched.LoadedModels()
	resp := PSResponse{Models: make([]PSModel, 0, len(loaded))}
	for _, l := range loaded {
		resp.Models = append(resp.Models, PSModel{
			Name:     l.Name,
			Size:     l.Size,
			LastUsed: l.LastUsed,
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

// Healthz handles GET /healthz.
func (h *Handler) Healthz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok")) //nolint:errcheck
}

// Readyz handles GET /readyz.
func (h *Handler) Readyz(w http.ResponseWriter, r *http.Request) {
	if !h.ready {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("not ready")) //nolint:errcheck
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok")) //nolint:errcheck
}

// ---- Helpers ----

func buildGenerateOptions(opts *Options) engine.GenerateOptions {
	goOpts := engine.GenerateOptions{
		Sampler: engine.DefaultSamplerOptions(),
	}
	if opts == nil {
		return goOpts
	}
	if opts.NumPredict != 0 { // 0 = use default; -1 = unlimited
		goOpts.MaxTokens = opts.NumPredict
	}
	if len(opts.Stop) > 0 {
		goOpts.StopStrings = opts.Stop
	}
	if opts.Temperature > 0 {
		goOpts.Sampler.Temperature = opts.Temperature
	}
	if opts.TopK > 0 {
		goOpts.Sampler.TopK = opts.TopK
	}
	if opts.TopP > 0 {
		goOpts.Sampler.TopP = opts.TopP
	}
	if opts.MinP > 0 {
		goOpts.Sampler.MinP = opts.MinP
	}
	if opts.Seed != 0 {
		goOpts.Sampler.Seed = opts.Seed
	}
	// Newly wired options.
	if opts.TypicalP > 0 {
		goOpts.Sampler.TypicalP = opts.TypicalP
	}
	if opts.RepeatPenalty > 0 {
		goOpts.Sampler.RepeatPenalty = opts.RepeatPenalty
	}
	if opts.RepeatLastN != 0 {
		goOpts.Sampler.RepeatLastN = opts.RepeatLastN
	}
	if opts.FrequencyPenalty != 0 {
		goOpts.Sampler.FrequencyPenalty = opts.FrequencyPenalty
	}
	if opts.PresencePenalty != 0 {
		goOpts.Sampler.PresencePenalty = opts.PresencePenalty
	}
	if opts.Mirostat > 0 {
		goOpts.Sampler.Mirostat = opts.Mirostat
	}
	if opts.MirostatTau > 0 {
		goOpts.Sampler.MirostatTau = opts.MirostatTau
	}
	if opts.MirostatEta > 0 {
		goOpts.Sampler.MirostatEta = opts.MirostatEta
	}
	// num_ctx, num_thread, num_gpu, keep_alive, penalize_newline, tfs_z are
	// accepted for Ollama API compatibility but not applied at request time.
	return goOpts
}

// chatPrompt applies the handler's prompt builder (defaults to buildChatPrompt).
func (h *Handler) chatPrompt(msgs []Message) (string, error) {
	if h.promptBuilder != nil {
		return h.promptBuilder(msgs)
	}
	return buildChatPrompt(msgs)
}

// buildChatPrompt formats messages as a ChatML prompt string.
func buildChatPrompt(msgs []Message) (string, error) {
	llamaMsgs := make([]llama.ChatMessage, 0, len(msgs))
	for _, m := range msgs {
		llamaMsgs = append(llamaMsgs, llama.NewChatMessage(m.Role, m.Content))
	}
	buf := make([]byte, 32768)
	n := llama.ChatApplyTemplate("chatml", llamaMsgs, true, buf)
	if n < 0 {
		return "", fmt.Errorf("chat template failed (code %d)", n)
	}
	return string(buf[:n]), nil
}

func manifestToInfo(mf *models.Manifest) ModelInfo {
	return ModelInfo{
		Name:       mf.FullName(),
		Model:      mf.FullName(),
		Size:       mf.Size,
		Digest:     "sha256:" + mf.Digest,
		ModifiedAt: mf.ModifiedAt,
		Details:    detailsFromManifest(mf),
	}
}

func detailsFromManifest(mf *models.Manifest) ModelDetails {
	return ModelDetails{
		Family:            mf.Details.Family,
		ParameterSize:     mf.Details.ParameterSize,
		QuantizationLevel: mf.Details.QuantizationLevel,
		ContextLength:     mf.Details.ContextLength,
		Format:            mf.Details.Format,
	}
}

// inferenceOutcome returns the outcome label value for inference metrics.
func inferenceOutcome(ctx interface{ Err() error }, err error) string {
	if err == nil {
		return metrics.OutcomeSuccess
	}
	if ctx.Err() != nil {
		return metrics.OutcomeCancelled
	}
	return metrics.OutcomeError
}

// resolveFormat parses the request's Format field and returns:
//   - grammar: the GBNF grammar string to constrain generation (always jsongrammar.JSONGrammar when format != nil)
//   - schema: the JSON Schema map to validate against after generation (nil if format is just "json")
//   - err: parse error
//
// Using a single canonical JSON grammar for all format variants avoids the
// persistent NFA crashes that resulted from hand-crafted per-schema GBNF.
func resolveFormat(format any) (grammar string, schema map[string]any, err error) {
	if format == nil {
		return "", nil, nil
	}
	switch v := format.(type) {
	case string:
		if v == "json" {
			return jsongrammar.JSONGrammar, nil, nil
		}
		return "", nil, fmt.Errorf("unsupported format value %q; use \"json\" or a JSON Schema object", v)
	case map[string]any:
		return jsongrammar.JSONGrammar, v, nil
	default:
		return "", nil, fmt.Errorf("format must be \"json\" or a JSON Schema object")
	}
}

// noThinkOnly is the Qwen3/QwQ thinking-suppression control token.
// Per Qwen3 docs, it must be appended to the last USER message, not the system message.
const noThinkOnly = "/no_think"

// jsonSystemPrompt is the system message injected for structured output requests.
// Separated from noThinkOnly because /no_think must go in the user turn.
const jsonSystemPrompt = "You are a JSON extraction API. Respond ONLY with a valid JSON object matching the requested schema. No explanations, no markdown, no text before or after the JSON."

// injectNoThink suppresses thinking-model chain-of-thought and, when withJSON
// is true, also injects a JSON-only system prompt.
//
// Per official Qwen3 documentation, the /no_think control token must be
// APPENDED TO THE LAST USER MESSAGE, not placed in the system message:
//
//	"Extract name and age: John Smith is 42. /no_think"
//
// When withJSON is true, a JSON-only instruction is added as a system message
// (separate from /no_think since it does not need to be in the user turn).
//
// A "</think>" stop string is added as a safety net so generation halts
// immediately if the model emits a thinking block despite /no_think.
func injectNoThink(msgs []Message, opts *engine.GenerateOptions, withJSON bool) []Message {
	result := make([]Message, len(msgs))
	copy(result, msgs)

	// Append /no_think to the last user message (Qwen3 docs requirement).
	for i := len(result) - 1; i >= 0; i-- {
		if result[i].Role == "user" {
			result[i].Content = result[i].Content + " " + noThinkOnly
			break
		}
	}

	// For structured output: inject a JSON-only system prompt.
	if withJSON {
		// Prepend to existing system message, or insert a new one at the front.
		found := false
		for i, m := range result {
			if m.Role == "system" {
				result[i].Content = jsonSystemPrompt + "\n" + m.Content
				found = true
				break
			}
		}
		if !found {
			newMsgs := make([]Message, 0, len(result)+1)
			newMsgs = append(newMsgs, Message{Role: "system", Content: jsonSystemPrompt})
			newMsgs = append(newMsgs, result...)
			result = newMsgs
		}
	}

	// Safety net: stop generation at </think>.
	for _, s := range opts.StopStrings {
		if s == "</think>" {
			return result
		}
	}
	opts.StopStrings = append(opts.StopStrings, "</think>")
	return result
}
