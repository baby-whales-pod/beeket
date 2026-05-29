package api

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/baby-whales-pod/beeket/internal/grammar"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---- resolveGrammar ----

func TestResolveGrammar_Nil(t *testing.T) {
	g, err := resolveGrammar(nil)
	require.NoError(t, err)
	assert.Empty(t, g)
}

func TestResolveGrammar_JsonString(t *testing.T) {
	g, err := resolveGrammar("json")
	require.NoError(t, err)
	assert.Equal(t, grammar.JSONSchemaGrammar, g)
}

func TestResolveGrammar_UnsupportedString(t *testing.T) {
	_, err := resolveGrammar("xml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported format value")
}

func TestResolveGrammar_JSONSchemaMap(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
			"age":  map[string]any{"type": "integer"},
		},
		"required": []any{"name", "age"},
	}
	g, err := resolveGrammar(schema)
	require.NoError(t, err)
	assert.Contains(t, g, "root ::=")
	assert.Contains(t, g, `"name"`)
	assert.Contains(t, g, `"age"`)
}

func TestResolveGrammar_InvalidType(t *testing.T) {
	_, err := resolveGrammar(42)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "format must be")
}

// ---- ChatRequest JSON decoding with Format field ----

func TestChatRequest_Decode_FormatString(t *testing.T) {
	body := `{"model":"smollm2:135m","messages":[{"role":"user","content":"hi"}],"format":"json"}`
	var req ChatRequest
	err := json.NewDecoder(bytes.NewBufferString(body)).Decode(&req)
	require.NoError(t, err)
	assert.Equal(t, "json", req.Format)
}

func TestChatRequest_Decode_FormatSchema(t *testing.T) {
	body := `{
		"model": "smollm2:135m",
		"messages": [{"role": "user", "content": "Extract info"}],
		"format": {
			"type": "object",
			"properties": {
				"name": {"type": "string"},
				"age":  {"type": "integer"}
			},
			"required": ["name", "age"]
		}
	}`
	var req ChatRequest
	err := json.NewDecoder(bytes.NewBufferString(body)).Decode(&req)
	require.NoError(t, err)
	schemaMap, ok := req.Format.(map[string]any)
	require.True(t, ok, "format should decode to map[string]any")
	assert.Equal(t, "object", schemaMap["type"])
}

func TestChatRequest_Decode_NoFormat(t *testing.T) {
	body := `{"model":"smollm2:135m","messages":[{"role":"user","content":"hi"}]}`
	var req ChatRequest
	err := json.NewDecoder(bytes.NewBufferString(body)).Decode(&req)
	require.NoError(t, err)
	assert.Nil(t, req.Format)
}

// ---- GenerateRequest JSON decoding with Format field ----

func TestGenerateRequest_Decode_FormatString(t *testing.T) {
	body := `{"model":"smollm2:135m","prompt":"hello","format":"json"}`
	var req GenerateRequest
	err := json.NewDecoder(bytes.NewBufferString(body)).Decode(&req)
	require.NoError(t, err)
	assert.Equal(t, "json", req.Format)
}

func TestGenerateRequest_Decode_FormatSchema(t *testing.T) {
	body := `{"model":"smollm2:135m","prompt":"hello","format":{"type":"object","properties":{"x":{"type":"number"}},"required":["x"]}}`
	var req GenerateRequest
	err := json.NewDecoder(bytes.NewBufferString(body)).Decode(&req)
	require.NoError(t, err)
	schemaMap, ok := req.Format.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "object", schemaMap["type"])
}

func TestGenerateRequest_Decode_NoFormat(t *testing.T) {
	body := `{"model":"smollm2:135m","prompt":"hello"}`
	var req GenerateRequest
	err := json.NewDecoder(bytes.NewBufferString(body)).Decode(&req)
	require.NoError(t, err)
	assert.Nil(t, req.Format)
}

// ---- resolveGrammar via roundtrip ----

func TestResolveGrammar_FromDecodedRequest(t *testing.T) {
	// Simulate a real chat request with format as JSON Schema.
	body := `{
		"model": "smollm2:135m",
		"messages": [{"role": "user", "content": "Name a city"}],
		"format": {
			"type": "object",
			"properties": {"city": {"type": "string"}},
			"required": ["city"]
		}
	}`
	var req ChatRequest
	require.NoError(t, json.NewDecoder(bytes.NewBufferString(body)).Decode(&req))
	g, err := resolveGrammar(req.Format)
	require.NoError(t, err)
	assert.Contains(t, g, "root ::=")
	assert.Contains(t, g, `"city"`)
}
