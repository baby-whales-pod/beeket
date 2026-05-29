// Package api — request handlers.
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/baby-whales-pod/beeket/internal/download"
	"github.com/baby-whales-pod/beeket/internal/engine"
	"github.com/baby-whales-pod/beeket/internal/models"
	"github.com/baby-whales-pod/beeket/internal/scheduler"
	"github.com/baby-whales-pod/beeket/internal/store"
	"github.com/baby-whales-pod/beeket/internal/version"
	"github.com/hybridgroup/yzma/pkg/llama"
)

// Handler holds all dependencies for API handlers.
type Handler struct {
	mgr   *models.Manager
	store *store.Store
	sched *scheduler.Scheduler
	ready bool
}

// NewHandler creates a Handler.
func NewHandler(mgr *models.Manager, st *store.Store, sched *scheduler.Scheduler) *Handler {
	return &Handler{mgr: mgr, store: st, sched: sched, ready: true}
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

	// Derive a clean slash-free registry key for the manifest store.
	// models.CleanModelRef normalises hf.co/org/repo:quant and HTTPS URLs
	// into a simple name:tag pair that ListManifests can find.
	registryName, registryTag := models.CleanModelRef(req.Name)
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
	opts := buildGenerateOptions(req.Options)

	prompt := req.Prompt
	if req.System != "" {
		prompt = req.System + "\n\n" + prompt
	}

	start := time.Now()
	nw := NewNDJSONWriter(w)
	evalCount := 0

	genErr := h.sched.Generate(r.Context(), name, tag, prompt, opts, func(piece string) error {
		evalCount++
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

	if genErr != nil && r.Context().Err() == nil {
		writeError(w, http.StatusInternalServerError, genErr.Error())
		return
	}

	total := time.Since(start).Nanoseconds()
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

	prompt, err := buildChatPrompt(req.Messages)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	stream := req.Stream == nil || *req.Stream
	name, tag := h.mgr.Resolve(req.Model)
	opts := buildGenerateOptions(req.Options)

	start := time.Now()
	nw := NewNDJSONWriter(w)
	var sb strings.Builder
	evalCount := 0

	genErr := h.sched.Generate(r.Context(), name, tag, prompt, opts, func(piece string) error {
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

	if genErr != nil && r.Context().Err() == nil {
		writeError(w, http.StatusInternalServerError, genErr.Error())
		return
	}

	total := time.Since(start).Nanoseconds()
	nw.Write(ChatResponse{ //nolint:errcheck
		Model:         req.Model,
		CreatedAt:     time.Now().UTC().Format(time.RFC3339Nano),
		Message:       Message{Role: "assistant", Content: sb.String()},
		Done:          true,
		TotalDuration: total,
		EvalCount:     evalCount,
		EvalDuration:  total,
	})
}

// Embeddings handles POST /api/embeddings.
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

	var inputs []string
	switch v := req.Input.(type) {
	case string:
		inputs = []string{v}
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok {
				inputs = append(inputs, s)
			}
		}
	default:
		writeError(w, http.StatusBadRequest, "input must be a string or array of strings")
		return
	}

	// Embeddings require a dedicated embedding context (NUBatch + pooling).
	// For v0.1 we return a stub response with zero vectors so the API
	// contract is satisfied and callers can integrate.
	resp := EmbeddingsResponse{
		Model:      req.Model,
		Embeddings: make([][]float32, len(inputs)),
	}
	for i := range resp.Embeddings {
		resp.Embeddings[i] = []float32{}
	}
	writeJSON(w, http.StatusOK, resp)
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
	if opts.NumPredict > 0 {
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
	return goOpts
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
