package api

import (
	"testing"

	"github.com/baby-whales-pod/beeket/internal/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// injectNoThink — new behaviour (Qwen3 docs: /no_think in last user message)
// ---------------------------------------------------------------------------

func TestInjectNoThink_AppendsToLastUserMsg(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "hello"},
	}
	opts := engine.GenerateOptions{}
	result := injectNoThink(msgs, &opts, false)

	// /no_think must be appended to the last user message.
	require.Len(t, result, 1)
	assert.Equal(t, "user", result[0].Role)
	assert.Contains(t, result[0].Content, noThinkOnly)
	assert.Contains(t, result[0].Content, "hello")
	// /no_think comes AFTER the user content.
	assert.True(t, len(result[0].Content) > len("hello"), "content should be extended")
}

func TestInjectNoThink_LastUserMsgSelected(t *testing.T) {
	// When there are multiple messages, /no_think goes on the last user turn.
	msgs := []Message{
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: "reply"},
		{Role: "user", Content: "second"},
	}
	opts := engine.GenerateOptions{}
	result := injectNoThink(msgs, &opts, false)

	// First user message unchanged.
	assert.Equal(t, "first", result[0].Content)
	// Last user message gets /no_think.
	assert.Contains(t, result[2].Content, noThinkOnly)
	assert.Contains(t, result[2].Content, "second")
}

func TestInjectNoThink_NoSystemMsg_WithJSONFalse(t *testing.T) {
	// withJSON=false: no system message injected, only /no_think in user turn.
	msgs := []Message{{Role: "user", Content: "hi"}}
	opts := engine.GenerateOptions{}
	result := injectNoThink(msgs, &opts, false)

	// No system message should be added.
	for _, m := range result {
		assert.NotEqual(t, "system", m.Role, "no system message should be injected for withJSON=false")
	}
}

func TestInjectNoThink_WithJSONTrue_InjectsSystemMsg(t *testing.T) {
	// withJSON=true: JSON system prompt injected; /no_think in user turn.
	msgs := []Message{{Role: "user", Content: "hi"}}
	opts := engine.GenerateOptions{}
	result := injectNoThink(msgs, &opts, true)

	// A system message should be present.
	var sys Message
	var hasSystem bool
	for _, m := range result {
		if m.Role == "system" {
			sys = m
			hasSystem = true
			break
		}
	}
	require.True(t, hasSystem, "system message must be injected for withJSON=true")
	assert.Contains(t, sys.Content, "JSON")
	// System message must NOT contain /no_think (that goes in the user turn).
	assert.NotContains(t, sys.Content, noThinkOnly,
		"/no_think must be in user turn, not system message")
}

func TestInjectNoThink_WithJSONTrue_ExistingSystemMsg_Prepends(t *testing.T) {
	msgs := []Message{
		{Role: "system", Content: "Be helpful."},
		{Role: "user", Content: "query"},
	}
	opts := engine.GenerateOptions{}
	result := injectNoThink(msgs, &opts, true)

	// System message should be updated with JSON instruction prepended.
	assert.Equal(t, "system", result[0].Role)
	assert.Contains(t, result[0].Content, "JSON")
	assert.Contains(t, result[0].Content, "Be helpful.")
	// /no_think in the user message.
	assert.Contains(t, result[1].Content, noThinkOnly)
	assert.Contains(t, result[1].Content, "query")
}

func TestInjectNoThink_AddsThinkStopString(t *testing.T) {
	msgs := []Message{{Role: "user", Content: "hi"}}
	opts := engine.GenerateOptions{}
	injectNoThink(msgs, &opts, true)
	assert.Contains(t, opts.StopStrings, "</think>")
}

func TestInjectNoThink_NoDuplicateStopString(t *testing.T) {
	msgs := []Message{{Role: "user", Content: "hi"}}
	opts := engine.GenerateOptions{StopStrings: []string{"</think>"}}
	injectNoThink(msgs, &opts, true)

	count := 0
	for _, s := range opts.StopStrings {
		if s == "</think>" {
			count++
		}
	}
	assert.Equal(t, 1, count, "</think> must not be duplicated")
}

func TestInjectNoThink_DoesNotMutateInput(t *testing.T) {
	original := []Message{
		{Role: "user", Content: "original"},
	}
	opts := engine.GenerateOptions{}
	result := injectNoThink(original, &opts, true)

	assert.Equal(t, "original", original[0].Content, "input slice must not be mutated")
	assert.NotEqual(t, original[0].Content, result[0].Content)
}

func TestInjectNoThink_PreservesMessageOrder(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: "second"},
		{Role: "user", Content: "third"},
	}
	opts := engine.GenerateOptions{}
	result := injectNoThink(msgs, &opts, true)

	// withJSON=true adds a system message at front.
	require.GreaterOrEqual(t, len(result), 3)
	// Original message content order preserved (system may be prepended).
	var userMsgs []Message
	for _, m := range result {
		if m.Role == "user" || m.Role == "assistant" {
			userMsgs = append(userMsgs, m)
		}
	}
	assert.Equal(t, "assistant", userMsgs[1].Role)
}
