package tools

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseToolCall_PureJSON(t *testing.T) {
	input := `{"name": "get_weather", "arguments": {"city": "Paris"}}`
	tc, ok := ParseToolCall(input)
	require.True(t, ok)
	assert.Equal(t, "get_weather", tc.Name)
	assert.Equal(t, "Paris", tc.Arguments["city"])
}

func TestParseToolCall_LeadingProse(t *testing.T) {
	input := `Sure! Here is the tool call: {"name": "send_email", "arguments": {"to": "alice@example.com"}}`
	tc, ok := ParseToolCall(input)
	require.True(t, ok)
	assert.Equal(t, "send_email", tc.Name)
	assert.Equal(t, "alice@example.com", tc.Arguments["to"])
}

func TestParseToolCall_TrailingJunk(t *testing.T) {
	input := `{"name": "ping", "arguments": {}} some trailing text`
	tc, ok := ParseToolCall(input)
	require.True(t, ok)
	assert.Equal(t, "ping", tc.Name)
}

func TestParseToolCall_NoJSON(t *testing.T) {
	input := "The weather in Paris is sunny and 22°C."
	_, ok := ParseToolCall(input)
	assert.False(t, ok)
}

func TestParseToolCall_MissingName(t *testing.T) {
	input := `{"arguments": {"x": 1}}`
	_, ok := ParseToolCall(input)
	assert.False(t, ok)
}

func TestParseToolCall_MissingArguments(t *testing.T) {
	// Arguments field absent — should still succeed (arguments may be empty).
	input := `{"name": "get_time"}`
	tc, ok := ParseToolCall(input)
	// The parser accepts this: name is present, arguments is nil (empty map).
	require.True(t, ok)
	assert.Equal(t, "get_time", tc.Name)
}

func TestParseToolCall_NestedObjects(t *testing.T) {
	input := `{"name": "update_settings", "arguments": {"config": {"theme": "dark", "font": 14}}}`
	tc, ok := ParseToolCall(input)
	require.True(t, ok)
	assert.Equal(t, "update_settings", tc.Name)
	config, ok2 := tc.Arguments["config"].(map[string]any)
	require.True(t, ok2)
	assert.Equal(t, "dark", config["theme"])
}

func TestParseToolCall_EscapedStringInJSON(t *testing.T) {
	input := `{"name": "echo", "arguments": {"msg": "he said \"hello\""}}`
	tc, ok := ParseToolCall(input)
	require.True(t, ok)
	assert.Equal(t, "echo", tc.Name)
	assert.Contains(t, tc.Arguments["msg"], "hello")
}

func TestParseToolCall_MultipleJSONObjects_TakesFirst(t *testing.T) {
	// Two JSON objects in the text — parser takes the first valid tool call.
	input := `{"name": "first_tool", "arguments": {}} and {"name": "second_tool", "arguments": {}}`
	tc, ok := ParseToolCall(input)
	require.True(t, ok)
	assert.Equal(t, "first_tool", tc.Name)
}

func TestParseToolCall_EmptyString(t *testing.T) {
	_, ok := ParseToolCall("")
	assert.False(t, ok)
}

func TestParseToolCall_UnbalancedBrace(t *testing.T) {
	input := `{"name": "broken", "arguments": {`
	_, ok := ParseToolCall(input)
	assert.False(t, ok)
}

func TestFindBalancedEnd_Simple(t *testing.T) {
	s := `{"key": "value"}`
	end := findBalancedEnd(s, 0)
	assert.Equal(t, len(s)-1, end)
}

func TestFindBalancedEnd_Nested(t *testing.T) {
	s := `{"outer": {"inner": 1}}`
	end := findBalancedEnd(s, 0)
	assert.Equal(t, len(s)-1, end)
}

func TestFindBalancedEnd_WithArray(t *testing.T) {
	s := `{"list": [1, 2, 3]}`
	end := findBalancedEnd(s, 0)
	assert.Equal(t, len(s)-1, end)
}

func TestFindBalancedEnd_NotFound(t *testing.T) {
	s := `{"unclosed": "value"`
	end := findBalancedEnd(s, 0)
	assert.Equal(t, -1, end)
}

func TestCleanOutput_RemovesToolCallJSON(t *testing.T) {
	tc := &ToolCall{Name: "get_weather", Arguments: map[string]any{"city": "Paris"}}
	input := `{"name": "get_weather", "arguments": {"city": "Paris"}}`
	result := CleanOutput(input, tc)
	assert.Empty(t, result)
}

func TestCleanOutput_NilToolCall(t *testing.T) {
	input := "Some prose output."
	result := CleanOutput(input, nil)
	assert.Equal(t, input, result)
}

func TestCleanOutput_ProseAndJSON(t *testing.T) {
	tc := &ToolCall{Name: "ping", Arguments: map[string]any{}}
	input := `Calling tool: {"name": "ping", "arguments": {}}`
	result := CleanOutput(input, tc)
	assert.Contains(t, result, "Calling tool:")
	assert.NotContains(t, result, `"name"`)
}
