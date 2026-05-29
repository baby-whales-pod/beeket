package tools

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildGrammar_EmptyTools(t *testing.T) {
	_, _, err := BuildGrammar(nil)
	require.Error(t, err)
	_, _, err = BuildGrammar([]Tool{})
	require.Error(t, err)
}

// TestBuildGrammar_PropertyKeyIsStringLiteral verifies the critical fix: property
// keys must appear as GBNF string literals ("\"city\""), not as bare rule
// references (city). A bare rule reference would cause a grammar parse error at
// runtime because no such rule is defined.
func TestBuildGrammar_PropertyKeyIsStringLiteral(t *testing.T) {
	tools := []Tool{
		{
			Type: "function",
			Function: ToolFunction{
				Name: "get_weather",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"city": map[string]any{"type": "string"},
					},
					"required": []any{"city"},
				},
			},
		},
	}

	grammar, _, err := BuildGrammar(tools)
	require.NoError(t, err)

	// The key must appear as the GBNF string literal "\"city\"" — NOT as the
	// bare identifier city (which would be an undefined rule reference).
	assert.Contains(t, grammar, `"\"city\""`, "property key must be a GBNF string literal")
	assert.NotContains(t, grammar, `"\"" city "\""`, "property key must not be a bare rule reference")
}

func TestBuildGrammar_SingleTool(t *testing.T) {
	tools := []Tool{
		{
			Type: "function",
			Function: ToolFunction{
				Name:        "get_weather",
				Description: "Get current weather for a city.",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"city": map[string]any{
							"type": "string",
						},
					},
					"required": []any{"city"},
				},
			},
		},
	}

	grammar, lazyTrigger, err := BuildGrammar(tools)
	require.NoError(t, err)

	// Grammar must contain a root rule.
	assert.Contains(t, grammar, "root ::=")
	// Grammar must contain the tool name literal.
	assert.Contains(t, grammar, `get_weather`)
	// Lazy trigger must be set.
	assert.Equal(t, `\{`, lazyTrigger)
}

func TestBuildGrammar_MultipleTools(t *testing.T) {
	tools := []Tool{
		{
			Type: "function",
			Function: ToolFunction{
				Name: "get_weather",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"city": map[string]any{"type": "string"},
					},
					"required": []any{"city"},
				},
			},
		},
		{
			Type: "function",
			Function: ToolFunction{
				Name: "send_email",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"to":      map[string]any{"type": "string"},
						"subject": map[string]any{"type": "string"},
						"body":    map[string]any{"type": "string"},
					},
					"required": []any{"to", "subject", "body"},
				},
			},
		},
	}

	grammar, lazyTrigger, err := BuildGrammar(tools)
	require.NoError(t, err)

	assert.Contains(t, grammar, `get_weather`)
	assert.Contains(t, grammar, `send_email`)
	assert.Equal(t, `\{`, lazyTrigger)

	// Both tool names should appear in the name rule.
	lines := strings.Split(grammar, "\n")
	var nameRuleLine string
	for _, l := range lines {
		if strings.HasPrefix(l, "tool-name ::=") {
			nameRuleLine = l
			break
		}
	}
	require.NotEmpty(t, nameRuleLine, "tool-name rule should exist")
	assert.Contains(t, nameRuleLine, `get_weather`)
	assert.Contains(t, nameRuleLine, `send_email`)
}

func TestBuildGrammar_EnumType(t *testing.T) {
	tools := []Tool{
		{
			Type: "function",
			Function: ToolFunction{
				Name: "set_mode",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"mode": map[string]any{
							"type": "string",
							"enum": []any{"fast", "slow", "medium"},
						},
					},
					"required": []any{"mode"},
				},
			},
		},
	}

	grammar, _, err := BuildGrammar(tools)
	require.NoError(t, err)
	assert.Contains(t, grammar, `fast`)
	assert.Contains(t, grammar, `slow`)
	assert.Contains(t, grammar, `medium`)
}

func TestBuildGrammar_ArrayType(t *testing.T) {
	tools := []Tool{
		{
			Type: "function",
			Function: ToolFunction{
				Name: "search",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"tags": map[string]any{
							"type": "array",
							"items": map[string]any{
								"type": "string",
							},
						},
					},
					"required": []any{"tags"},
				},
			},
		},
	}

	grammar, _, err := BuildGrammar(tools)
	require.NoError(t, err)
	assert.Contains(t, grammar, "root ::=")
	// Array rule should reference the item rule.
	assert.Contains(t, grammar, `"["`)
}

func TestBuildGrammar_FallbackOnUnknownType(t *testing.T) {
	tools := []Tool{
		{
			Type: "function",
			Function: ToolFunction{
				Name: "mystery",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"data": map[string]any{
							"type": "unknown_future_type",
						},
					},
					"required": []any{"data"},
				},
			},
		},
	}

	// Should not return an error — falls back to json-value.
	grammar, _, err := BuildGrammar(tools)
	require.NoError(t, err)
	assert.Contains(t, grammar, "json-value")
}

func TestBuildGrammar_EmptyParameters(t *testing.T) {
	tools := []Tool{
		{
			Type: "function",
			Function: ToolFunction{
				Name:       "ping",
				Parameters: map[string]any{},
			},
		},
	}

	grammar, _, err := BuildGrammar(tools)
	require.NoError(t, err)
	assert.Contains(t, grammar, "root ::=")
}

func TestBuildGrammar_ContainsCommonRules(t *testing.T) {
	tools := []Tool{
		{
			Type: "function",
			Function: ToolFunction{
				Name:       "noop",
				Parameters: map[string]any{"type": "object", "properties": map[string]any{}},
			},
		},
	}

	grammar, _, err := BuildGrammar(tools)
	require.NoError(t, err)

	for _, rule := range []string{"ws ::=", "string ::=", "number ::=", "boolean ::=", "json-value ::="} {
		assert.Contains(t, grammar, rule, "expected rule %q in grammar", rule)
	}
}

// TestBuildGrammar_OptionalFieldsWrapped verifies that optional (non-required) fields
// are wrapped with ( ... )? in the grammar so the model is not forced to emit them.
func TestBuildGrammar_OptionalFieldsWrapped(t *testing.T) {
	tools := []Tool{
		{
			Type: "function",
			Function: ToolFunction{
				Name: "create_event",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"title":       map[string]any{"type": "string"},
						"description": map[string]any{"type": "string"}, // optional
					},
					"required": []any{"title"},
				},
			},
		},
	}

	grammar, _, err := BuildGrammar(tools)
	require.NoError(t, err)

	// The optional "description" field must be wrapped with ()? in the grammar.
	assert.Contains(t, grammar, `)?`, "optional field should be wrapped with ()?")
	// Required "title" must still appear as a mandatory field.
	assert.Contains(t, grammar, `"\"title\""`, "required field must appear as literal")
}

// TestBuildGrammar_DeterministicOrdering verifies that calling BuildGrammar
// twice with the same input produces identical output (map iteration must be sorted).
func TestBuildGrammar_DeterministicOrdering(t *testing.T) {
	makeTools := func() []Tool {
		return []Tool{
			{
				Type: "function",
				Function: ToolFunction{
					Name: "search",
					Parameters: map[string]any{
						"type": "object",
						"properties": map[string]any{
							"query":    map[string]any{"type": "string"},
							"limit":    map[string]any{"type": "integer"},
							"offset":   map[string]any{"type": "integer"},
							"category": map[string]any{"type": "string"},
						},
						"required": []any{"query"},
					},
				},
			},
		}
	}

	grammar1, _, err1 := BuildGrammar(makeTools())
	require.NoError(t, err1)
	grammar2, _, err2 := BuildGrammar(makeTools())
	require.NoError(t, err2)

	assert.Equal(t, grammar1, grammar2, "grammar output must be deterministic")
}

// TestBuildGrammar_CollisionDetected verifies that two tool names producing the
// same sanitized rule name return an error.
func TestBuildGrammar_CollisionDetected(t *testing.T) {
	tools := []Tool{
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
				Name:       "get_weather", // sanitizes to same "get-weather"
				Parameters: map[string]any{"type": "object", "properties": map[string]any{}},
			},
		},
	}

	_, _, err := BuildGrammar(tools)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "collision")
}

// TestBuildGrammar_RequiredFieldsBeforeOptional verifies that required properties
// always appear before optional ones in the object rule.
func TestBuildGrammar_RequiredFieldsBeforeOptional(t *testing.T) {
	tools := []Tool{
		{
			Type: "function",
			Function: ToolFunction{
				Name: "example",
				Parameters: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"alpha": map[string]any{"type": "string"}, // optional
						"beta":  map[string]any{"type": "string"}, // required
						"gamma": map[string]any{"type": "string"}, // optional
						"delta": map[string]any{"type": "string"}, // required
					},
					"required": []any{"beta", "delta"},
				},
			},
		},
	}

	grammar, _, err := BuildGrammar(tools)
	require.NoError(t, err)

	// Find the args-example rule line.
	lines := strings.Split(grammar, "\n")
	var argsLine string
	for _, l := range lines {
		if strings.HasPrefix(l, "args-example ::=") {
			argsLine = l
			break
		}
	}
	require.NotEmpty(t, argsLine)

	// Required fields (beta, delta) must appear before optional fields (alpha, gamma).
	betaPos := strings.Index(argsLine, "beta")
	deltaPos := strings.Index(argsLine, "delta")
	alphaPos := strings.Index(argsLine, "alpha")
	gammaPos := strings.Index(argsLine, "gamma")

	assert.Greater(t, alphaPos, betaPos, "optional 'alpha' should appear after required 'beta'")
	assert.Greater(t, alphaPos, deltaPos, "optional 'alpha' should appear after required 'delta'")
	assert.Greater(t, gammaPos, betaPos, "optional 'gamma' should appear after required 'beta'")
	assert.Greater(t, gammaPos, deltaPos, "optional 'gamma' should appear after required 'delta'")
}
