package jsongrammar_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/baby-whales-pod/beeket/internal/jsongrammar"
)

func TestJSONGrammarNotEmpty(t *testing.T) {
	assert.NotEmpty(t, jsongrammar.JSONGrammar)
	assert.Contains(t, jsongrammar.JSONGrammar, "root")
	assert.Contains(t, jsongrammar.JSONGrammar, "object")
}

func TestValidateSchema_ValidObject(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"capital": map[string]any{"type": "string"},
			"country": map[string]any{"type": "string"},
		},
		"required": []any{"capital", "country"},
	}
	err := jsongrammar.ValidateSchema(schema, `{"capital":"Paris","country":"France"}`)
	require.NoError(t, err)
}

func TestValidateSchema_MissingRequired(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"capital": map[string]any{"type": "string"},
			"country": map[string]any{"type": "string"},
		},
		"required": []any{"capital", "country"},
	}
	err := jsongrammar.ValidateSchema(schema, `{"capital":"Paris"}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "schema validation failed")
}

func TestValidateSchema_WrongType(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"age": map[string]any{"type": "integer"},
		},
		"required": []any{"age"},
	}
	err := jsongrammar.ValidateSchema(schema, `{"age":"not-a-number"}`)
	require.Error(t, err)
}

func TestValidateSchema_InvalidJSON(t *testing.T) {
	schema := map[string]any{"type": "object"}
	err := jsongrammar.ValidateSchema(schema, `{bad json}`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid JSON")
}

func TestValidateSchema_EmptySchema(t *testing.T) {
	// Empty schema accepts anything
	schema := map[string]any{}
	err := jsongrammar.ValidateSchema(schema, `{"anything": 42}`)
	require.NoError(t, err)
}

func TestValidateSchema_FieldOrderIndependent(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"capital": map[string]any{"type": "string"},
			"country": map[string]any{"type": "string"},
		},
		"required": []any{"capital", "country"},
	}
	// Both field orderings must pass validation
	err1 := jsongrammar.ValidateSchema(schema, `{"capital":"Paris","country":"France"}`)
	err2 := jsongrammar.ValidateSchema(schema, `{"country":"France","capital":"Paris"}`)
	require.NoError(t, err1)
	require.NoError(t, err2)
}
