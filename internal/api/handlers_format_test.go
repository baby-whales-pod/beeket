package api

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/baby-whales-pod/beeket/internal/jsongrammar"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---- resolveFormat ----

func TestResolveFormat_Nil(t *testing.T) {
	g, sc, err := resolveFormat(nil)
	require.NoError(t, err)
	assert.Empty(t, g)
	assert.Nil(t, sc)
}

func TestResolveFormat_JsonString(t *testing.T) {
	g, sc, err := resolveFormat("json")
	require.NoError(t, err)
	assert.Equal(t, jsongrammar.JSONGrammar, g)
	assert.Nil(t, sc, "no schema to validate for bare 'json' format")
}

func TestResolveFormat_UnsupportedString(t *testing.T) {
	_, _, err := resolveFormat("xml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported format value")
}

func TestResolveFormat_JSONSchemaMap(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
			"age":  map[string]any{"type": "integer"},
		},
		"required": []any{"name", "age"},
	}
	g, sc, err := resolveFormat(schema)
	require.NoError(t, err)
	// All schema formats now use the canonical JSON grammar.
	assert.Equal(t, jsongrammar.JSONGrammar, g)
	// Schema is returned for post-generation validation.
	require.NotNil(t, sc)
	assert.Equal(t, "object", sc["type"])
}

func TestResolveFormat_InvalidType(t *testing.T) {
	_, _, err := resolveFormat(42)
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

// ---- resolveFormat via roundtrip ----

func TestResolveFormat_FromDecodedRequest(t *testing.T) {
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
	g, sc, err := resolveFormat(req.Format)
	require.NoError(t, err)
	// Always uses canonical JSON grammar.
	assert.Equal(t, jsongrammar.JSONGrammar, g)
	// Schema is passed through for validation.
	require.NotNil(t, sc)
	assert.Equal(t, "object", sc["type"])
}
