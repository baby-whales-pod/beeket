// Package grammar converts JSON Schema objects to GBNF grammar strings
// suitable for use with the llama.cpp grammar sampler (SamplerInitGrammar).
//
// The converter supports a practical subset of JSON Schema Draft-07:
//   - Primitive types: string, number, integer, boolean, null
//   - object (with properties and required)
//   - array (with items)
//   - enum (string/number/boolean/null values)
//   - anyOf / oneOf
//   - Nested schemas
//
// The root rule is always named "root".
package grammar

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// JSONSchemaGrammar is the standard GBNF grammar for unconstrained JSON values.
// Use this when format is "json" (no schema).
const JSONSchemaGrammar = `root   ::= object | array | string | number | boolean | null
object ::= "{" ws (string ":" ws value ("," ws string ":" ws value)*)? "}"
array  ::= "[" ws (value ("," ws value)*)? "]"
value  ::= object | array | string | number | boolean | null
string ::= "\"" ([^"\\] | "\\" (["\\/bfnrt] | "u" [0-9a-fA-F] [0-9a-fA-F] [0-9a-fA-F] [0-9a-fA-F]))* "\""
number ::= ("-"? ([0-9] | [1-9] [0-9]*)) ("." [0-9]+)? (([eE] [-+]? [0-9]+))?
boolean ::= "true" | "false"
null ::= "null"
ws ::= ([ \t\n] ws)?
`

// FromJSONSchema converts a JSON Schema (as raw bytes) to a GBNF grammar string.
// Returns JSONSchemaGrammar if schema is empty.
func FromJSONSchema(schemaBytes []byte) (string, error) {
	if len(schemaBytes) == 0 {
		return JSONSchemaGrammar, nil
	}
	var schema map[string]any
	if err := json.Unmarshal(schemaBytes, &schema); err != nil {
		return "", fmt.Errorf("grammar: parse JSON schema: %w", err)
	}
	return FromMap(schema)
}

// FromMap converts a JSON Schema represented as a Go map to a GBNF grammar string.
func FromMap(schema map[string]any) (string, error) {
	c := &converter{
		rules:   make(map[string]string),
		counter: make(map[string]int),
	}
	rootRule, err := c.visit(schema, "root")
	if err != nil {
		return "", err
	}
	// If the root rule was inlined (not a named rule), wrap it.
	if rootRule != "root" {
		c.addRule("root", rootRule)
	}
	return c.build(), nil
}

// converter accumulates GBNF rules during schema traversal.
type converter struct {
	rules   map[string]string // ruleName → body
	order   []string          // insertion order for deterministic output
	counter map[string]int    // for unique name generation
}

// addRule records a rule. If the same name and body already exist it is reused.
// Returns the rule name.
func (c *converter) addRule(name, body string) string {
	if existing, ok := c.rules[name]; ok {
		if existing == body {
			return name
		}
		// Name collision with different body — generate a unique name.
		c.counter[name]++
		name = fmt.Sprintf("%s-%d", name, c.counter[name])
	}
	c.rules[name] = body
	c.order = append(c.order, name)
	return name
}

// build serialises all collected rules into a GBNF string.
func (c *converter) build() string {
	var sb strings.Builder
	// root first, then sorted others
	if body, ok := c.rules["root"]; ok {
		sb.WriteString("root ::= ")
		sb.WriteString(body)
		sb.WriteString("\n")
	}
	names := make([]string, 0, len(c.rules))
	for n := range c.rules {
		if n != "root" {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	for _, n := range names {
		sb.WriteString(n)
		sb.WriteString(" ::= ")
		sb.WriteString(c.rules[n])
		sb.WriteString("\n")
	}
	// Always include the shared primitives helpers.
	sb.WriteString(sharedPrimitives)
	return sb.String()
}

// visit returns the GBNF expression for the given schema node.
// preferredName is a hint for the rule name (used when this schema becomes its own rule).
func (c *converter) visit(schema map[string]any, preferredName string) (string, error) {
	// anyOf / oneOf
	if anyOf, ok := schema["anyOf"]; ok {
		return c.visitCombination(anyOf, preferredName, "anyOf")
	}
	if oneOf, ok := schema["oneOf"]; ok {
		return c.visitCombination(oneOf, preferredName, "oneOf")
	}

	// enum
	if enumRaw, ok := schema["enum"]; ok {
		return c.visitEnum(enumRaw, preferredName)
	}

	// const
	if constRaw, ok := schema["const"]; ok {
		lit, err := json.Marshal(constRaw)
		if err != nil {
			return "", fmt.Errorf("grammar: marshal const: %w", err)
		}
		return fmt.Sprintf("%q", string(lit)), nil
	}

	// type field
	typeVal, _ := schema["type"].(string)
	switch typeVal {
	case "object":
		return c.visitObject(schema, preferredName)
	case "array":
		return c.visitArray(schema, preferredName)
	case "string":
		return "string", nil
	case "number":
		return "number", nil
	case "integer":
		return "integer", nil
	case "boolean":
		return "boolean", nil
	case "null":
		return `"null"`, nil
	default:
		// No type — treat as any JSON value
		return "value", nil
	}
}

func (c *converter) visitObject(schema map[string]any, name string) (string, error) {
	props, _ := schema["properties"].(map[string]any)
	required, _ := schema["required"].([]any)
	requiredSet := make(map[string]bool, len(required))
	for _, r := range required {
		if s, ok := r.(string); ok {
			requiredSet[s] = true
		}
	}

	// Sort properties for deterministic output.
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Separate required and optional properties.
	reqKeys := make([]string, 0)
	optKeys := make([]string, 0)
	for _, k := range keys {
		if requiredSet[k] {
			reqKeys = append(reqKeys, k)
		} else {
			optKeys = append(optKeys, k)
		}
	}

	var parts []string
	allKeys := append(reqKeys, optKeys...)

	for i, k := range allKeys {
		propSchema, ok := props[k].(map[string]any)
		if !ok {
			propSchema = map[string]any{}
		}
		childName := sanitizeName(name + "-" + k)
		expr, err := c.visit(propSchema, childName)
		if err != nil {
			return "", err
		}

		// If expr is complex (contains spaces) promote it to its own rule.
		if isComplex(expr) && !isPrimitive(expr) {
			ruleName := c.addRule(childName, expr)
			expr = ruleName
		}

		keyLit, _ := json.Marshal(k)
		pair := fmt.Sprintf("%s \":\" ws %s", string(keyLit), expr)

		if i == 0 {
			parts = append(parts, pair)
		} else if requiredSet[k] {
			parts = append(parts, `"," ws `+pair)
		} else {
			// Optional property: wrapped in ( "," ws pair )?
			parts = append(parts, `( "," ws `+pair+` )?`)
		}
	}

	body := `"{" ws`
	if len(parts) > 0 {
		body += " " + strings.Join(parts, " ")
	}
	body += ` "}" ws`

	ruleName := c.addRule(name, body)
	return ruleName, nil
}

func (c *converter) visitArray(schema map[string]any, name string) (string, error) {
	itemsSchema, hasItems := schema["items"].(map[string]any)
	var itemExpr string
	if hasItems {
		childName := sanitizeName(name + "-item")
		expr, err := c.visit(itemsSchema, childName)
		if err != nil {
			return "", err
		}
		if isComplex(expr) && !isPrimitive(expr) {
			expr = c.addRule(childName, expr)
		}
		itemExpr = expr
	} else {
		itemExpr = "value"
	}

	body := fmt.Sprintf(`"[" ws (%s ("," ws %s)*)? "]" ws`, itemExpr, itemExpr)
	ruleName := c.addRule(name, body)
	return ruleName, nil
}

func (c *converter) visitCombination(rawList any, name, kind string) (string, error) {
	list, ok := rawList.([]any)
	if !ok {
		return "value", nil
	}
	alternatives := make([]string, 0, len(list))
	for i, item := range list {
		sub, ok := item.(map[string]any)
		if !ok {
			continue
		}
		childName := fmt.Sprintf("%s-%s-%d", name, kind, i)
		expr, err := c.visit(sub, childName)
		if err != nil {
			return "", err
		}
		if isComplex(expr) && !isPrimitive(expr) {
			expr = c.addRule(childName, expr)
		}
		alternatives = append(alternatives, expr)
	}
	if len(alternatives) == 0 {
		return "value", nil
	}
	return strings.Join(alternatives, " | "), nil
}

func (c *converter) visitEnum(rawEnum any, name string) (string, error) {
	list, ok := rawEnum.([]any)
	if !ok {
		return "value", nil
	}
	alternatives := make([]string, 0, len(list))
	for _, v := range list {
		lit, err := json.Marshal(v)
		if err != nil {
			return "", fmt.Errorf("grammar: marshal enum value: %w", err)
		}
		// lit is already a valid JSON-encoded value (e.g. `"red"`, `42`, `true`, `null`).
		// In GBNF, string literals are written as double-quoted strings.
		alternatives = append(alternatives, `"`+strings.ReplaceAll(string(lit), `"`, `\"`)+`"`)
	}
	return strings.Join(alternatives, " | "), nil
}

// sharedPrimitives is appended to every generated grammar. It provides
// the base rules that generated rules reference.
const sharedPrimitives = `boolean ::= "true" | "false"
integer ::= "-"? ([0-9] | [1-9] [0-9]*)
null    ::= "null"
number  ::= ("-"? ([0-9] | [1-9] [0-9]*)) ("." [0-9]+)? (([eE] [-+]? [0-9]+))?
string  ::= "\"" ([^"\\] | "\\" (["\\/bfnrt] | "u" [0-9a-fA-F] [0-9a-fA-F] [0-9a-fA-F] [0-9a-fA-F]))* "\""
value   ::= object-generic | array-generic | string | number | boolean | null
object-generic ::= "{" ws (string ":" ws value ("," ws string ":" ws value)*)? "}"
array-generic  ::= "[" ws (value ("," ws value)*)? "]"
ws      ::= ([ \t\n] ws)?
`

// isComplex returns true if an expression contains whitespace (is more than a simple name).
func isComplex(expr string) bool {
	return strings.ContainsAny(expr, " \t\n")
}

// isPrimitive returns true if expr is one of the built-in primitive rule names.
func isPrimitive(expr string) bool {
	switch expr {
	case "string", "number", "integer", "boolean", "null", "value":
		return true
	}
	return false
}

// sanitizeName converts a schema path to a valid GBNF rule name.
func sanitizeName(s string) string {
	var sb strings.Builder
	for _, ch := range s {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '-' || ch == '_' {
			sb.WriteRune(ch)
		} else {
			sb.WriteRune('-')
		}
	}
	return strings.Trim(sb.String(), "-")
}
