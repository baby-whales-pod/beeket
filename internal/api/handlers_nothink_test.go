package api

import (
	"testing"

	"github.com/baby-whales-pod/beeket/internal/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// injectNoThink
// ---------------------------------------------------------------------------

func TestInjectNoThink_NoSystemMsg_InjectsNew(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "hello"},
	}
	opts := engine.GenerateOptions{}
	result := injectNoThink(msgs, &opts, true)

	require.Len(t, result, 2)
	assert.Equal(t, "system", result[0].Role)
	assert.Contains(t, result[0].Content, "/no_think")
	assert.Equal(t, "user", result[1].Role)
}

func TestInjectNoThink_ExistingSystemMsg_Prepends(t *testing.T) {
	msgs := []Message{
		{Role: "system", Content: "Be helpful."},
		{Role: "user", Content: "hello"},
	}
	opts := engine.GenerateOptions{}
	result := injectNoThink(msgs, &opts, true)

	require.Len(t, result, 2, "no new message should be inserted")
	assert.Equal(t, "system", result[0].Role)
	assert.Contains(t, result[0].Content, "/no_think")
	assert.Contains(t, result[0].Content, "Be helpful.")
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
		{Role: "system", Content: "original"},
		{Role: "user", Content: "query"},
	}
	opts := engine.GenerateOptions{}
	result := injectNoThink(original, &opts, true)

	// Result must be a new slice; original unchanged.
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

	// New system message at index 0, then original messages in order.
	require.Len(t, result, 4)
	assert.Equal(t, "system", result[0].Role)
	assert.Equal(t, "first", result[1].Content)
	assert.Equal(t, "second", result[2].Content)
	assert.Equal(t, "third", result[3].Content)
}

// TestInjectNoThink_WithJSONFalse verifies that when withJSON=false (think:false
// without a format field), only "/no_think" is injected — no JSON instruction.
func TestInjectNoThink_WithJSONFalse(t *testing.T) {
	msgs := []Message{{Role: "user", Content: "hi"}}
	opts := engine.GenerateOptions{}
	result := injectNoThink(msgs, &opts, false)

	require.Len(t, result, 2)
	assert.Equal(t, "/no_think", result[0].Content)
	assert.NotContains(t, result[0].Content, "JSON")
}

// TestInjectNoThink_WithJSONTrue verifies that when withJSON=true (format is set),
// the JSON instruction is also injected.
func TestInjectNoThink_WithJSONTrue(t *testing.T) {
	msgs := []Message{{Role: "user", Content: "hi"}}
	opts := engine.GenerateOptions{}
	result := injectNoThink(msgs, &opts, true)

	require.Len(t, result, 2)
	assert.Contains(t, result[0].Content, "/no_think")
	assert.Contains(t, result[0].Content, "JSON")
}
