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

// maxPermutationFields is the maximum number of all-required fields for which
// we generate full N! permutations. Beyond this we fall back to ordered output
// to avoid exponential grammar size.
const maxPermutationFields = 6

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
		rules: make(map[string]string),
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
	rules map[string]string // ruleName → body
	order []string          // insertion order for deterministic output
}

// addRule records a rule. If the same name and body already exist it is reused.
// Returns the (possibly renamed) rule name.
func (c *converter) addRule(name, body string) string {
	if existing, ok := c.rules[name]; ok {
		if existing == body {
			return name
		}
		// Name collision with different body — find an unused numbered variant.
		for i := 1; ; i++ {
			candidate := fmt.Sprintf("%s-%d", name, i)
			if existing2, ok2 := c.rules[candidate]; !ok2 {
				// Slot is free — use it.
				c.rules[candidate] = body
				c.order = append(c.order, candidate)
				return candidate
			} else if existing2 == body {
				// Same body already registered under this name — reuse.
				return candidate
			}
		}
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
		return "null", nil
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

	reqKeys := make([]string, 0, len(keys))
	optKeys := make([]string, 0, len(keys))
	for _, k := range keys {
		if requiredSet[k] {
			reqKeys = append(reqKeys, k)
		} else {
			optKeys = append(optKeys, k)
		}
	}

	// Fast path: all fields are required and there are few enough to enumerate
	// all N! orderings. This ensures the grammar accepts ANY field order that
	// the model might generate, preventing "empty grammar stack" crashes when
	// the model produces fields in a different order than the schema lists them.
	if len(optKeys) == 0 && len(reqKeys) >= 1 && len(reqKeys) <= maxPermutationFields {
		return c.visitObjectAllRequired(props, reqKeys, name)
	}

	// General path: mixed required + optional fields, or too many required
	// fields to enumerate permutations. Required fields are emitted in sorted
	// order; optional fields are wrapped in ( ... )?.
	return c.visitObjectMixed(props, reqKeys, optKeys, name)
}

// visitObjectAllRequired generates a grammar that accepts any field ordering
// for an object where every declared property is required.
//
// For N fields it emits N! alternative sequences as named per-field rules:
//
//	root-capital ::= "\"capital\"" ws ":" ws string
//	root-country ::= "\"country\"" ws ":" ws string
//	root ::= "{" ws ( root-capital "," ws root-country
//	                | root-country "," ws root-capital ) "}" ws
func (c *converter) visitObjectAllRequired(
	props map[string]any,
	keys []string, // sorted, all required
	name string,
) (string, error) {
	// Build a named per-field rule for each property so each appears once in
	// the grammar output regardless of how many permutations reference it.
	fieldRules := make(map[string]string, len(keys)) // key → rule name
	for _, k := range keys {
		propSchema, ok := props[k].(map[string]any)
		if !ok {
			propSchema = map[string]any{}
		}
		childName := sanitizeName(name + "-" + k)
		expr, err := c.visit(propSchema, childName)
		if err != nil {
			return "", err
		}
		if isComplex(expr) && !isPrimitive(expr) {
			expr = c.addRule(childName, expr)
		}
		// gbnfKey produces a GBNF string literal matching the JSON-quoted key.
		// json.Marshal handles keys with embedded special characters safely.
		// e.g. k="capital" → inner=`"capital"` → gbnfKey=`"\"capital\""` → GBNF matches `"capital"`
		inner, _ := json.Marshal(k)
		gbnfKey := `"` + strings.ReplaceAll(string(inner), `"`, `\"`) + `"`
		fieldBody := fmt.Sprintf(`%s ws ":" ws %s`, gbnfKey, expr)
		fieldRules[k] = c.addRule(childName, fieldBody)
	}

	// Generate all N! orderings.
	perms := fieldPermutations(keys)
	alts := make([]string, 0, len(perms))
	for _, perm := range perms {
		parts := make([]string, len(perm))
		for i, k := range perm {
			if i == 0 {
				parts[i] = fieldRules[k]
			} else {
				parts[i] = `"," ws ` + fieldRules[k]
			}
		}
		alts = append(alts, strings.Join(parts, " "))
	}

	// For a single field there are no alternatives — embed it directly.
	var body string
	if len(alts) == 1 {
		body = `"{" ws ` + alts[0] + ` "}" ws`
	} else {
		// Extract the permutation alternatives into a separate named rule.
		// llama.cpp's GBNF parser does not reliably handle inline ( A | B )
		// groups: the NFA epsilon-transitions from the group into the
		// continuation of the parent rule are not always generated, which
		// leaves valid characters (e.g. '"') unreachable after '{' and causes:
		//   std::runtime_error: Unexpected empty grammar stack
		// Naming the group as a separate rule forces a proper rule-reference
		// expansion path that llama.cpp handles correctly.
		fieldsRule := strings.Join(alts, " | ")
		fieldsName := c.addRule(name+"-fields", fieldsRule)
		body = `"{" ws ` + fieldsName + ` "}" ws`
	}

	ruleName := c.addRule(name, body)
	return ruleName, nil
}

// visitObjectMixed generates a grammar for objects with a mix of required and
// optional fields, or objects with more than maxPermutationFields required
// fields. Required fields are emitted in sorted order (no permutations).
// Optional fields are wrapped in ( ... )?.
func (c *converter) visitObjectMixed(
	props map[string]any,
	reqKeys, optKeys []string,
	name string,
) (string, error) {
	var parts []string

	for i, k := range reqKeys {
		propSchema, ok := props[k].(map[string]any)
		if !ok {
			propSchema = map[string]any{}
		}
		childName := sanitizeName(name + "-" + k)
		expr, err := c.visit(propSchema, childName)
		if err != nil {
			return "", err
		}
		if isComplex(expr) && !isPrimitive(expr) {
			expr = c.addRule(childName, expr)
		}
		inner, _ := json.Marshal(k)
		gbnfKey := `"` + strings.ReplaceAll(string(inner), `"`, `\"`) + `"`
		pair := fmt.Sprintf(`%s ":" ws %s`, gbnfKey, expr)
		if i == 0 {
			parts = append(parts, pair)
		} else {
			parts = append(parts, `"," ws `+pair)
		}
	}

	for i, k := range optKeys {
		propSchema, ok := props[k].(map[string]any)
		if !ok {
			propSchema = map[string]any{}
		}
		childName := sanitizeName(name + "-" + k)
		expr, err := c.visit(propSchema, childName)
		if err != nil {
			return "", err
		}
		if isComplex(expr) && !isPrimitive(expr) {
			expr = c.addRule(childName, expr)
		}
		inner, _ := json.Marshal(k)
		gbnfKey := `"` + strings.ReplaceAll(string(inner), `"`, `\"`) + `"`
		pair := fmt.Sprintf(`%s ":" ws %s`, gbnfKey, expr)

		needComma := len(reqKeys) > 0 || i > 0
		if needComma {
			parts = append(parts, `( "," ws `+pair+` )?`)
		} else {
			parts = append(parts, `( `+pair+` )?`)
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

// fieldPermutations returns all permutations of keys in lexicographic order
// of the permutations themselves (for deterministic grammar output).
func fieldPermutations(keys []string) [][]string {
	if len(keys) == 0 {
		return [][]string{{}}
	}
	if len(keys) == 1 {
		return [][]string{{keys[0]}}
	}
	result := make([][]string, 0)
	// Heap's algorithm for deterministic permutation generation.
	// We work on a copy to avoid mutating the caller's slice.
	work := make([]string, len(keys))
	copy(work, keys)
	var generate func(k int)
	generate = func(k int) {
		if k == 1 {
			perm := make([]string, len(work))
			copy(perm, work)
			result = append(result, perm)
			return
		}
		for i := 0; i < k; i++ {
			generate(k - 1)
			if k%2 == 0 {
				work[i], work[k-1] = work[k-1], work[i]
			} else {
				work[0], work[k-1] = work[k-1], work[0]
			}
		}
	}
	generate(len(work))
	return result
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
		alternatives = append(alternatives, `"`+strings.ReplaceAll(string(lit), `"`, `\"`)+`"`)
	}
	return strings.Join(alternatives, " | "), nil
}

// sharedPrimitives is appended to every generated grammar. It provides
// the base rules that generated rules reference.
// All inline repetition groups (`(...)* ` and `(...)?`) that llama.cpp's
// GBNF parser may not handle correctly are replaced with recursive named rules.
const sharedPrimitives = `boolean ::= "true" | "false"
integer ::= "-"? ([0-9] | [1-9] [0-9]*)
null    ::= "null"
number  ::= ("-"? ([0-9] | [1-9] [0-9]*)) ("." [0-9]+)? (([eE] [-+]? [0-9]+))?
string  ::= "\"" ([^"\\] | "\\" (["\\/bfnrt] | "u" [0-9a-fA-F] [0-9a-fA-F] [0-9a-fA-F] [0-9a-fA-F]))* "\""
value   ::= object-generic | array-generic | string | number | boolean | null
object-generic   ::= "{" ws object-pair-list "}" ws | "{" ws "}" ws
object-pair-list ::= string ":" ws value | string ":" ws value "," ws object-pair-list
array-generic    ::= "[" ws array-item-list "]" ws | "[" ws "]" ws
array-item-list  ::= value | value "," ws array-item-list
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
