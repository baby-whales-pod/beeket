// Package jsongrammar provides llama.cpp's canonical JSON grammar and
// Go-side JSON Schema validation for structured output.
package jsongrammar

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

// JSONGrammar is llama.cpp's canonical json.gbnf grammar.
// Source: https://github.com/ggml-org/llama.cpp/blob/master/grammars/json.gbnf
// Using this well-tested grammar avoids the NFA issues that arise from
// hand-crafted GBNF generators.
const JSONGrammar = `root   ::= object
value  ::= object | array | string | number | ("true" | "false" | "null") ws

object ::=
  "{" ws (
            string ":" ws value
    ("," ws string ":" ws value)*
  )? "}" ws

array  ::=
  "[" ws (
            value
    ("," ws value)*
  )? "]" ws

string ::=
  "\"" (
    [^"\\\x7F\x00-\x1F] |
    "\\" (["\\bfnrt] | "u" [0-9a-fA-F] [0-9a-fA-F] [0-9a-fA-F] [0-9a-fA-F])
  )* "\"" ws

number ::= ("-"? ([0-9] | [1-9] [0-9]*)) ("." [0-9]+)? ([eE] [-+]? [0-9]+)? ws

ws ::= | " " | "\n" [ \t][ \t][ \t][ \t][ \t][ \t][ \t][ \t][ \t][ \t][ \t][ \t][ \t][ \t][ \t][ \t][ \t][ \t][ \t][ \t]
`

// ValidateSchema checks that jsonStr satisfies the given JSON Schema.
// schema is the raw schema as a Go map (already decoded from the request's
// format field). Returns nil if validation passes.
func ValidateSchema(schema map[string]any, jsonStr string) error {
	// Encode schema map back to JSON so we can compile it.
	schemaBytes, err := json.Marshal(schema)
	if err != nil {
		return fmt.Errorf("jsongrammar: marshal schema: %w", err)
	}

	compiler := jsonschema.NewCompiler()
	compiler.Draft = jsonschema.Draft7

	schemaURL := "schema://inline"
	if err := compiler.AddResource(schemaURL, strings.NewReader(string(schemaBytes))); err != nil {
		return fmt.Errorf("jsongrammar: compile schema: %w", err)
	}

	sch, err := compiler.Compile(schemaURL)
	if err != nil {
		return fmt.Errorf("jsongrammar: compile schema: %w", err)
	}

	var v any
	if err := json.Unmarshal([]byte(jsonStr), &v); err != nil {
		return fmt.Errorf("jsongrammar: invalid JSON in response: %w", err)
	}

	if err := sch.Validate(v); err != nil {
		return fmt.Errorf("jsongrammar: schema validation failed: %w", err)
	}
	return nil
}
