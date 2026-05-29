package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Fake embedScheduler
// ---------------------------------------------------------------------------

type fakeEmbedSched struct {
	vec     []float32
	err     error
	lastMsg string
}

func (f *fakeEmbedSched) Embed(_ context.Context, _, _, input string) ([]float32, int, error) {
	f.lastMsg = input
	if f.err != nil {
		return nil, 0, f.err
	}
	return f.vec, len(input), nil
}

// handlerForEmbedTest builds a minimal Handler with only the fields
// Embeddings() needs: embedSched and embedMgr.
func handlerForEmbedTest(es embedScheduler) *Handler {
	return &Handler{
		embedSched: es,
		embedMgr:   &fakeManager{},
		startTime:  time.Now(),
	}
}

// fakeManager satisfies mgrResolver for embed handler tests.
type fakeManager struct{}

func (f *fakeManager) Resolve(ref string) (string, string) { return ref, "latest" }

func doEmbedReq(t *testing.T, h *Handler, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/api/embeddings", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.Embeddings(rr, req)
	return rr
}

// ---------------------------------------------------------------------------
// Input validation tests
// ---------------------------------------------------------------------------

func TestEmbeddingsHandler_StringInput(t *testing.T) {
	fs := &fakeEmbedSched{vec: []float32{0.1, 0.2, 0.3}}
	h := handlerForEmbedTest(fs)

	rr := doEmbedReq(t, h, map[string]any{"model": "test-model", "input": "hello world"})

	assert.Equal(t, http.StatusOK, rr.Code)
	var resp EmbeddingsResponse
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	assert.Len(t, resp.Embeddings, 1)
	assert.Equal(t, []float32{0.1, 0.2, 0.3}, resp.Embeddings[0])
	assert.Equal(t, "hello world", fs.lastMsg)
}

func TestEmbeddingsHandler_SliceInput(t *testing.T) {
	fs := &fakeEmbedSched{vec: []float32{0.5, 0.5}}
	h := handlerForEmbedTest(fs)

	rr := doEmbedReq(t, h, map[string]any{"model": "m", "input": []string{"a", "b", "c"}})

	assert.Equal(t, http.StatusOK, rr.Code)
	var resp EmbeddingsResponse
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	assert.Len(t, resp.Embeddings, 3)
}

func TestEmbeddingsHandler_LegacyPrompt(t *testing.T) {
	fs := &fakeEmbedSched{vec: []float32{1}}
	h := handlerForEmbedTest(fs)

	rr := doEmbedReq(t, h, map[string]any{"model": "m", "prompt": "legacy input"})

	assert.Equal(t, http.StatusOK, rr.Code)
	var resp EmbeddingsResponse
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	assert.Len(t, resp.Embeddings, 1)
	assert.Equal(t, "legacy input", fs.lastMsg)
}

func TestEmbeddingsHandler_InputTakesPrecedenceOverPrompt(t *testing.T) {
	// When both input and prompt are supplied, input wins.
	fs := &fakeEmbedSched{vec: []float32{1}}
	h := handlerForEmbedTest(fs)

	rr := doEmbedReq(t, h, map[string]any{"model": "m", "input": "new input", "prompt": "old prompt"})

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "new input", fs.lastMsg)
}

func TestEmbeddingsHandler_MissingModel(t *testing.T) {
	h := handlerForEmbedTest(&fakeEmbedSched{})
	rr := doEmbedReq(t, h, map[string]any{"input": "hello"})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestEmbeddingsHandler_MissingInput(t *testing.T) {
	h := handlerForEmbedTest(&fakeEmbedSched{})
	rr := doEmbedReq(t, h, map[string]any{"model": "m"})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestEmbeddingsHandler_InvalidInputType(t *testing.T) {
	h := handlerForEmbedTest(&fakeEmbedSched{})
	rr := doEmbedReq(t, h, map[string]any{"model": "m", "input": 42})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestEmbeddingsHandler_InvalidInputArrayItem(t *testing.T) {
	h := handlerForEmbedTest(&fakeEmbedSched{})
	rr := doEmbedReq(t, h, map[string]any{"model": "m", "input": []any{"ok", 99}})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestEmbeddingsHandler_MalformedJSON(t *testing.T) {
	h := handlerForEmbedTest(&fakeEmbedSched{})
	req := httptest.NewRequest(http.MethodPost, "/api/embeddings", bytes.NewBufferString("{bad json"))
	rr := httptest.NewRecorder()
	h.Embeddings(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestEmbeddingsHandler_SchedulerError(t *testing.T) {
	fs := &fakeEmbedSched{err: errors.New("model not found")}
	h := handlerForEmbedTest(fs)
	rr := doEmbedReq(t, h, map[string]any{"model": "m", "input": "text"})
	assert.Equal(t, http.StatusInternalServerError, rr.Code)
}

func TestEmbeddingsHandler_EmptyStringInput(t *testing.T) {
	h := handlerForEmbedTest(&fakeEmbedSched{})
	rr := doEmbedReq(t, h, map[string]any{"model": "m", "input": ""})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestEmbeddingsHandler_EmptyStringInArray(t *testing.T) {
	h := handlerForEmbedTest(&fakeEmbedSched{})
	rr := doEmbedReq(t, h, map[string]any{"model": "m", "input": []string{"ok", ""}})
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestEmbeddingsHandler_ResponseFields(t *testing.T) {
	fs := &fakeEmbedSched{vec: []float32{0.1, 0.2}}
	h := handlerForEmbedTest(fs)

	rr := doEmbedReq(t, h, map[string]any{"model": "nomic-embed", "input": "hi"})

	assert.Equal(t, http.StatusOK, rr.Code)
	var resp EmbeddingsResponse
	require.NoError(t, json.NewDecoder(rr.Body).Decode(&resp))
	assert.Equal(t, "nomic-embed", resp.Model)
	assert.Greater(t, resp.TotalDuration, int64(0))
	assert.Greater(t, resp.PromptEvalCount, 0)
}
