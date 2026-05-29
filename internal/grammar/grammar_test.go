package grammar_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/baby-whales-pod/beeket/internal/grammar"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---- FromJSONSchema (raw bytes) ----

func TestFromJSONSchema_Empty(t *testing.T) {
	g, err := grammar.FromJSONSchema(nil)
	require.NoError(t, err)
	assert.Equal(t, grammar.JSONSchemaGrammar, g)
}

func TestFromJSONSchema_Invalid(t *testing.T) {
	_, err := grammar.FromJSONSchema([]byte("not-json"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse JSON schema")
}

func TestFromJSONSchema_SimpleObject(t *testing.T) {
	raw := []byte(`{
		"type": "object",
		"properties": {
			"name": {"type": "string"},
			"age":  {"type": "integer"}
		},
		"required": ["name", "age"]
	}`)
	g, err := grammar.FromJSONSchema(raw)
	require.NoError(t, err)
	assert.Contains(t, g, "root ::=")
	assert.Contains(t, g, `"name"`)
	assert.Contains(t, g, `"age"`)
	assert.Contains(t, g, "string")
	assert.Contains(t, g, "integer")
}

// ---- FromMap ----

func TestFromMap_StringType(t *testing.T) {
	schema := map[string]any{"type": "string"}
	g, err := grammar.FromMap(schema)
	require.NoError(t, err)
	assert.Contains(t, g, "root ::= string")
}

func TestFromMap_NumberType(t *testing.T) {
	schema := map[string]any{"type": "number"}
	g, err := grammar.FromMap(schema)
	require.NoError(t, err)
	assert.Contains(t, g, "root ::= number")
}

func TestFromMap_IntegerType(t *testing.T) {
	schema := map[string]any{"type": "integer"}
	g, err := grammar.FromMap(schema)
	require.NoError(t, err)
	assert.Contains(t, g, "root ::= integer")
}

func TestFromMap_BooleanType(t *testing.T) {
	schema := map[string]any{"type": "boolean"}
	g, err := grammar.FromMap(schema)
	require.NoError(t, err)
	assert.Contains(t, g, "root ::= boolean")
}

func TestFromMap_NullType(t *testing.T) {
	schema := map[string]any{"type": "null"}
	g, err := grammar.FromMap(schema)
	require.NoError(t, err)
	// The null type maps to the named "null" rule (consistent with other primitives).
	assert.Contains(t, g, "root ::= null")
}

func TestFromMap_NoType(t *testing.T) {
	schema := map[string]any{}
	g, err := grammar.FromMap(schema)
	require.NoError(t, err)
	assert.Contains(t, g, "root ::= value")
}

func TestFromMap_ArrayWithItems(t *testing.T) {
	schema := map[string]any{
		"type":  "array",
		"items": map[string]any{"type": "string"},
	}
	g, err := grammar.FromMap(schema)
	require.NoError(t, err)
	assert.Contains(t, g, "root")
	assert.Contains(t, g, "string")
}

func TestFromMap_ArrayWithoutItems(t *testing.T) {
	schema := map[string]any{"type": "array"}
	g, err := grammar.FromMap(schema)
	require.NoError(t, err)
	assert.Contains(t, g, "value")
}

func TestFromMap_ObjectWithAllRequired(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"city":    map[string]any{"type": "string"},
			"country": map[string]any{"type": "string"},
		},
		"required": []any{"city", "country"},
	}
	g, err := grammar.FromMap(schema)
	require.NoError(t, err)
	assert.Contains(t, g, `"city"`)
	assert.Contains(t, g, `"country"`)
	assert.Contains(t, g, "string")
}

func TestFromMap_ObjectWithOptionalProps(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
			"bio":  map[string]any{"type": "string"},
		},
		"required": []any{"name"},
	}
	g, err := grammar.FromMap(schema)
	require.NoError(t, err)
	// Optional property should be wrapped with "?"
	assert.Contains(t, g, "?")
	assert.Contains(t, g, `"bio"`)
}

func TestFromMap_NestedObject(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"address": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"street": map[string]any{"type": "string"},
					"zip":    map[string]any{"type": "string"},
				},
				"required": []any{"street"},
			},
		},
		"required": []any{"address"},
	}
	g, err := grammar.FromMap(schema)
	require.NoError(t, err)
	assert.Contains(t, g, `"address"`)
	assert.Contains(t, g, `"street"`)
	assert.Contains(t, g, `"zip"`)
}

func TestFromMap_Enum(t *testing.T) {
	schema := map[string]any{
		"type": "string",
		"enum": []any{"red", "green", "blue"},
	}
	g, err := grammar.FromMap(schema)
	require.NoError(t, err)
	// String enum values must match as JSON strings (with surrounding quotes).
	// In the grammar text: `"\"red\""` is the GBNF literal for the JSON token `"red"`.
	assert.Contains(t, g, `red`)
	assert.Contains(t, g, `green`)
	assert.Contains(t, g, `blue`)
	// The root rule should be an alternation.
	assert.Contains(t, g, "|")
}

func TestFromMap_EnumMixedTypes(t *testing.T) {
	schema := map[string]any{
		"enum": []any{"yes", false, nil, 42},
	}
	g, err := grammar.FromMap(schema)
	require.NoError(t, err)
	// Each value should appear somewhere in the grammar.
	assert.Contains(t, g, `yes`)
	assert.Contains(t, g, `false`)
	assert.Contains(t, g, `null`)
	assert.Contains(t, g, `42`)
}

func TestFromMap_AnyOf(t *testing.T) {
	schema := map[string]any{
		"anyOf": []any{
			map[string]any{"type": "string"},
			map[string]any{"type": "integer"},
		},
	}
	g, err := grammar.FromMap(schema)
	require.NoError(t, err)
	assert.Contains(t, g, "root ::=")
	assert.Contains(t, g, "|")
}

func TestFromMap_OneOf(t *testing.T) {
	schema := map[string]any{
		"oneOf": []any{
			map[string]any{"type": "boolean"},
			map[string]any{"type": "null"},
		},
	}
	g, err := grammar.FromMap(schema)
	require.NoError(t, err)
	assert.Contains(t, g, "|")
}

func TestFromMap_AllRulesPresent(t *testing.T) {
	// Any non-trivial object schema should produce the shared primitives.
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
			"age":  map[string]any{"type": "integer"},
		},
		"required": []any{"name"},
	}
	g, err := grammar.FromMap(schema)
	require.NoError(t, err)
	// Shared primitives are always appended.
	assert.Contains(t, g, "boolean ::=")
	assert.Contains(t, g, "integer ::=")
	assert.Contains(t, g, "number  ::=")
	assert.Contains(t, g, "string  ::=")
	assert.Contains(t, g, "ws      ::=")
}

func TestFromMap_ValidGBNFSyntax(t *testing.T) {
	// Each non-empty line should contain " ::= "
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"x": map[string]any{"type": "number"},
		},
		"required": []any{"x"},
	}
	g, err := grammar.FromMap(schema)
	require.NoError(t, err)
	for _, line := range strings.Split(g, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		assert.Contains(t, line, " ::= ", "every non-empty line should be a GBNF rule")
	}
}

// TestFromMap_ObjectAllOptional is the regression test for B1:
// when no properties are in "required", ALL properties must be wrapped
// as optional ( ... )? — the first alphabetical property must not be
// silently forced to be required.
func TestFromMap_ObjectAllOptional(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"a": map[string]any{"type": "string"},
			"b": map[string]any{"type": "string"},
		},
		// no "required" field
	}
	g, err := grammar.FromMap(schema)
	require.NoError(t, err)

	// Extract just the root rule line for inspection.
	var rootLine string
	for _, line := range strings.Split(g, "\n") {
		if strings.HasPrefix(line, "root ::=") {
			rootLine = line
			break
		}
	}
	require.NotEmpty(t, rootLine, "root rule must be present")

	// Both "a" and "b" must appear inside ( ... )? groups.
	// A simple proxy: both property keys appear and the rule contains "?".
	assert.Contains(t, rootLine, `"a"`, "property a must appear in root rule")
	assert.Contains(t, rootLine, `"b"`, "property b must appear in root rule")
	assert.Contains(t, rootLine, "?", "all-optional object must contain optional markers")

	// Neither property should appear *without* the optional wrapper,
	// i.e., there must not be a bare `"a" ":"` before the opening `(`.
	// We verify by checking the root rule body directly.
	assert.NotContains(t, rootLine, `"{" ws "a"`,
		`property "a" must not appear as a bare required first field`)
	assert.NotContains(t, rootLine, `"{" ws "b"`,
		`property "b" must not appear as a bare required first field`)
}

// TestFromMap_ObjectFirstPropOptional checks that when the first alphabetical
// property is NOT in required but a later one is, the optional one is still
// wrapped correctly.
func TestFromMap_ObjectFirstPropOptional(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"aaa": map[string]any{"type": "string"}, // alphabetically first, NOT required
			"zzz": map[string]any{"type": "string"}, // alphabetically last, required
		},
		"required": []any{"zzz"},
	}
	g, err := grammar.FromMap(schema)
	require.NoError(t, err)

	// The root rule should start with the required property (zzz)
	// directly after the opening brace, and aaa should be optional.
	var rootLine string
	for _, line := range strings.Split(g, "\n") {
		if strings.HasPrefix(line, "root ::=") {
			rootLine = line
			break
		}
	}
	require.NotEmpty(t, rootLine)
	assert.Contains(t, rootLine, `"zzz"`, "required zzz must appear")
	assert.Contains(t, rootLine, `"aaa"`, "optional aaa must appear")
	// aaa must be in an optional group
	assert.Contains(t, rootLine, "?", "optional property must be wrapped")
	// zzz (required) should appear before aaa (optional) in the rule
	assert.Less(t, strings.Index(rootLine, `"zzz"`), strings.Index(rootLine, `"aaa"`),
		"required properties emitted before optional ones")
}

func TestJSONSchemaGrammar_Constant(t *testing.T) {
	// Smoke-test: the constant must contain the root rule and helpers.
	assert.Contains(t, grammar.JSONSchemaGrammar, "root")
	assert.Contains(t, grammar.JSONSchemaGrammar, "object")
	assert.Contains(t, grammar.JSONSchemaGrammar, "string")
	assert.Contains(t, grammar.JSONSchemaGrammar, "number")
}

// ---- roundtrip: ensure generated grammar serialises a known schema ----

func TestFromJSONSchema_RoundTrip_ExtractSchema(t *testing.T) {
	raw := json.RawMessage(`{
		"type": "object",
		"properties": {
			"name":  {"type": "string"},
			"score": {"type": "number"},
			"pass":  {"type": "boolean"}
		},
		"required": ["name", "score", "pass"]
	}`)
	g, err := grammar.FromJSONSchema(raw)
	require.NoError(t, err)
	// Grammar must reference all three property keys.
	assert.Contains(t, g, `"name"`)
	assert.Contains(t, g, `"score"`)
	assert.Contains(t, g, `"pass"`)
}
