package tools

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRenderToolPreface_ContainsHeader(t *testing.T) {
	tools := []Tool{
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
	}

	preface := RenderToolPreface(tools)
	assert.Contains(t, preface, "You have access to the following tools")
	assert.Contains(t, preface, "get_weather")
	assert.Contains(t, preface, "Get weather for a city.")
	assert.Contains(t, preface, `{"name": "<tool_name>", "arguments": { ... }}`)
}

func TestRenderToolPreface_MultipleTools(t *testing.T) {
	tools := []Tool{
		{Type: "function", Function: ToolFunction{Name: "tool_one", Description: "First tool.", Parameters: map[string]any{}}},
		{Type: "function", Function: ToolFunction{Name: "tool_two", Description: "Second tool.", Parameters: map[string]any{}}},
	}
	preface := RenderToolPreface(tools)
	assert.Contains(t, preface, "tool_one")
	assert.Contains(t, preface, "tool_two")
}

func TestRenderToolPreface_NoDescriptionOmitted(t *testing.T) {
	tools := []Tool{
		{Type: "function", Function: ToolFunction{Name: "silent_tool", Parameters: map[string]any{}}},
	}
	preface := RenderToolPreface(tools)
	assert.Contains(t, preface, "silent_tool")
	// Description line should not appear when empty.
	assert.NotContains(t, preface, "description:")
}

func TestRewriteToolMessages_PassesThrough(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "Hello"},
		{Role: "assistant", Content: "Hi"},
	}
	out := RewriteToolMessages(msgs)
	assert.Equal(t, msgs, out)
}

func TestRewriteToolMessages_RewritesTool(t *testing.T) {
	msgs := []Message{
		{Role: "tool", Content: `{"temp": 22}`, ToolName: "get_weather"},
	}
	out := RewriteToolMessages(msgs)
	assert.Len(t, out, 1)
	assert.Equal(t, "user", out[0].Role)
	assert.Contains(t, out[0].Content, "get_weather")
	assert.Contains(t, out[0].Content, `{"temp": 22}`)
}

func TestRewriteToolMessages_UnknownToolName(t *testing.T) {
	msgs := []Message{
		{Role: "tool", Content: "ok"},
	}
	out := RewriteToolMessages(msgs)
	assert.Equal(t, "user", out[0].Role)
	assert.Contains(t, out[0].Content, "unknown")
}

func TestRewriteToolMessages_MixedRoles(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "What is the weather?"},
		{Role: "assistant", Content: "Let me check."},
		{Role: "tool", Content: `{"sunny": true}`, ToolName: "get_weather"},
		{Role: "user", Content: "Thanks."},
	}
	out := RewriteToolMessages(msgs)
	assert.Len(t, out, 4)
	assert.Equal(t, "user", out[0].Role)
	assert.Equal(t, "assistant", out[1].Role)
	assert.Equal(t, "user", out[2].Role) // rewritten tool message
	assert.Equal(t, "user", out[3].Role)
	assert.True(t, strings.Contains(out[2].Content, "get_weather"))
}
