package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/baby-whales-pod/beeket/internal/engine"
	"github.com/baby-whales-pod/beeket/internal/models"
	"github.com/baby-whales-pod/beeket/internal/scheduler"
	"github.com/baby-whales-pod/beeket/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeScheduler is a test double for generatorScheduler.
// The generate function is set per test to control what the model "returns".
type fakeScheduler struct {
	generateFn func(ctx context.Context, name, tag, prompt string, opts engine.GenerateOptions, out func(string) error) error
}

func (f *fakeScheduler) Generate(ctx context.Context, name, tag, prompt string, opts engine.GenerateOptions, out func(string) error) error {
	return f.generateFn(ctx, name, tag, prompt, opts, out)
}

func (f *fakeScheduler) LoadedModels() []scheduler.LoadedInfo {
	return nil
}

// newTestHandler creates a Handler wired with a tmp store/manager and the given fake scheduler.
func newTestHandler(t *testing.T, sched generatorScheduler) *Handler {
	t.Helper()
	tmpDir := t.TempDir()
	st, err := store.New(tmpDir)
	require.NoError(t, err)
	mgr := models.New(st)
	// Use a no-op prompt builder so tests don't require the llama library.
	fakePromptBuilder := func(msgs []Message) (string, error) {
		var sb strings.Builder
		for _, m := range msgs {
			sb.WriteString(m.Role + ": " + m.Content + "\n")
		}
		return sb.String(), nil
	}
	return &Handler{
		mgr:           mgr,
		store:         st,
		sched:         sched,
		ready:         true,
		startTime:     time.Now(),
		promptBuilder: fakePromptBuilder,
	}
}

// TestChat_ToolCall_StructuredResponse verifies that when the model emits a
// valid tool-call JSON, the response has tool_calls populated, content is
// empty, and done_reason is "tool_calls".
func TestChat_ToolCall_StructuredResponse(t *testing.T) {
	toolCallJSON := `{"name": "get_weather", "arguments": {"city": "Paris"}}`

	sched := &fakeScheduler{
		generateFn: func(ctx context.Context, name, tag, prompt string, opts engine.GenerateOptions, out func(string) error) error {
			// Simulate model emitting a tool-call JSON.
			return out(toolCallJSON)
		},
	}

	h := newTestHandler(t, sched)

	reqBody := ChatRequest{
		Model: "mymodel:latest",
		Messages: []Message{
			{Role: "user", Content: "What is the weather in Paris?"},
		},
		Tools: []Tool{
			{
				Type: "function",
				Function: ToolFunction{
					Name:        "get_weather",
					Description: "Get weather for a city.",
					Parameters: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"city": map[string]any{"type": "string"},
						},
						"required": []any{"city"},
					},
				},
			},
		},
	}

	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/chat", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.Chat(w, req)

	resp := w.Result()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	var chatResp ChatResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&chatResp))

	assert.Equal(t, "tool_calls", chatResp.DoneReason)
	assert.Empty(t, chatResp.Message.Content)
	require.Len(t, chatResp.Message.ToolCalls, 1)
	assert.Equal(t, "get_weather", chatResp.Message.ToolCalls[0].Function.Name)
	assert.Equal(t, "Paris", chatResp.Message.ToolCalls[0].Function.Arguments["city"])
	assert.True(t, chatResp.Done)
}

// TestChat_ToolCall_FallsBackToPlainContent verifies that when tools are
// requested but the model emits prose (no JSON), the response is plain content.
func TestChat_ToolCall_FallsBackToPlainContent(t *testing.T) {
	sched := &fakeScheduler{
		generateFn: func(ctx context.Context, name, tag, prompt string, opts engine.GenerateOptions, out func(string) error) error {
			return out("The weather in Paris is sunny.")
		},
	}

	h := newTestHandler(t, sched)

	reqBody := ChatRequest{
		Model: "mymodel:latest",
		Messages: []Message{
			{Role: "user", Content: "What is the weather in Paris?"},
		},
		Tools: []Tool{
			{
				Type: "function",
				Function: ToolFunction{
					Name:        "get_weather",
					Description: "Get weather for a city.",
					Parameters: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"city": map[string]any{"type": "string"},
						},
					},
				},
			},
		},
	}

	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/chat", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.Chat(w, req)

	var chatResp ChatResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&chatResp))

	assert.Empty(t, chatResp.DoneReason)
	assert.Empty(t, chatResp.Message.ToolCalls)
	assert.Contains(t, chatResp.Message.Content, "sunny")
}

// TestChat_NoTools_StreamsNormally verifies that regular chat (no tools) still works.
func TestChat_NoTools_StreamsNormally(t *testing.T) {
	sched := &fakeScheduler{
		generateFn: func(ctx context.Context, name, tag, prompt string, opts engine.GenerateOptions, out func(string) error) error {
			_ = out("Hello ")
			return out("world!")
		},
	}

	h := newTestHandler(t, sched)
	falseVal := false
	reqBody := ChatRequest{
		Model:    "mymodel:latest",
		Messages: []Message{{Role: "user", Content: "Hi"}},
		Stream:   &falseVal,
	}

	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/chat", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.Chat(w, req)

	// Non-streaming: decode a single final response.
	var chatResp ChatResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&chatResp))

	assert.True(t, chatResp.Done)
	assert.Contains(t, chatResp.Message.Content, "Hello")
}

// TestChat_ToolMessages_Rewritten verifies that role=tool messages are
// rewritten to user messages before the prompt is built.
func TestChat_ToolMessages_Rewritten(t *testing.T) {
	var capturedPrompt string
	sched := &fakeScheduler{
		generateFn: func(ctx context.Context, name, tag, prompt string, opts engine.GenerateOptions, out func(string) error) error {
			capturedPrompt = prompt
			return out(`{"name": "done", "arguments": {}}`)
		},
	}

	h := newTestHandler(t, sched)

	reqBody := ChatRequest{
		Model: "mymodel:latest",
		Messages: []Message{
			{Role: "user", Content: "What is the weather?"},
			{Role: "tool", Content: `{"temp": 22}`, ToolName: "get_weather"},
		},
		Tools: []Tool{
			{
				Type: "function",
				Function: ToolFunction{
					Name:       "done",
					Parameters: map[string]any{"type": "object", "properties": map[string]any{}},
				},
			},
		},
	}

	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/chat", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.Chat(w, req)

	// The tool-role message should be rewritten to a user message.
	assert.True(t, strings.Contains(capturedPrompt, "get_weather") || strings.Contains(capturedPrompt, "Tool result"),
		"expected tool result text in prompt, got: %s", capturedPrompt)
}

// TestChat_ToolsInjected_SystemPreface verifies the tool preface is injected
// into the prompt when tools are provided.
func TestChat_ToolsInjected_SystemPreface(t *testing.T) {
	var capturedPrompt string
	sched := &fakeScheduler{
		generateFn: func(ctx context.Context, name, tag, prompt string, opts engine.GenerateOptions, out func(string) error) error {
			capturedPrompt = prompt
			return out(`{"name": "tool_a", "arguments": {}}`)
		},
	}

	h := newTestHandler(t, sched)

	reqBody := ChatRequest{
		Model:    "mymodel:latest",
		Messages: []Message{{Role: "user", Content: "do something"}},
		Tools: []Tool{
			{
				Type: "function",
				Function: ToolFunction{
					Name:        "tool_a",
					Description: "Does something.",
					Parameters:  map[string]any{"type": "object", "properties": map[string]any{}},
				},
			},
		},
	}

	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/chat", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.Chat(w, req)

	assert.Contains(t, capturedPrompt, "tool_a", "expected tool name in prompt")
	assert.Contains(t, capturedPrompt, "You have access to the following tools", "expected tool preface in prompt")
}

// TestChat_InvalidToolSchema_Returns400 verifies that when the tools slice
// contains a schema that causes BuildGrammar to error (e.g. tool name
// collision), the handler returns HTTP 400.
func TestChat_InvalidToolSchema_Returns400(t *testing.T) {
	sched := &fakeScheduler{
		generateFn: func(ctx context.Context, name, tag, prompt string, opts engine.GenerateOptions, out func(string) error) error {
			return nil // should not be reached
		},
	}

	h := newTestHandler(t, sched)

	// Two tool names that sanitize to the same rule name.
	reqBody := ChatRequest{
		Model:    "mymodel:latest",
		Messages: []Message{{Role: "user", Content: "test"}},
		Tools: []Tool{
			{
				Type: "function",
				Function: ToolFunction{
					Name:       "get-weather",
					Parameters: map[string]any{"type": "object", "properties": map[string]any{}},
				},
			},
			{
				Type: "function",
				Function: ToolFunction{
					Name:       "get_weather", // collision: sanitizes to "get-weather"
					Parameters: map[string]any{"type": "object", "properties": map[string]any{}},
				},
			},
		},
	}

	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/chat", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.Chat(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	var errResp ErrorResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&errResp))
	assert.Contains(t, errResp.Error, "collision")
}

// populated in GenerateOptions when tools are provided.
func TestChat_GrammarIsSetWhenToolsPresent(t *testing.T) {
	var capturedOpts engine.GenerateOptions
	sched := &fakeScheduler{
		generateFn: func(ctx context.Context, name, tag, prompt string, opts engine.GenerateOptions, out func(string) error) error {
			capturedOpts = opts
			return out(`{"name": "noop", "arguments": {}}`)
		},
	}

	h := newTestHandler(t, sched)

	reqBody := ChatRequest{
		Model:    "mymodel:latest",
		Messages: []Message{{Role: "user", Content: "test"}},
		Tools: []Tool{
			{
				Type: "function",
				Function: ToolFunction{
					Name:       "noop",
					Parameters: map[string]any{"type": "object", "properties": map[string]any{}},
				},
			},
		},
	}

	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/chat", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.Chat(w, req)

	assert.NotEmpty(t, capturedOpts.Grammar, "expected grammar to be set in options")
	assert.NotEmpty(t, capturedOpts.GrammarLazy, "expected lazy grammar trigger to be set")
}
