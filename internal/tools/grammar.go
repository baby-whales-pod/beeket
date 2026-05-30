package tools

import (
	"fmt"
	"log/slog"
	"sort"
	"strings"
)

// BuildGrammar returns a GBNF grammar string that constrains generation to a
// tool-call JSON object of the form:
//
//	{"name": "<one_of_tool_names>", "arguments": { ... }}
//
// and a lazy-trigger pattern (always `\{`). The caller combines these
// with SamplerInitGrammarLazyPatterns so the model may emit normal prose OR
// begin a tool call by starting with "{".
//
// Supported JSON-schema types: object, string, integer, number, boolean, enum
// (string), array (of supported types), nested object. Unknown constructs fall
// back to the permissive json-value rule with a warning log.
//
// Returns an error if the tools slice is empty or if two tool names produce the
// same sanitized rule identifier (collision).
func BuildGrammar(tools []Tool) (grammar string, lazyTrigger string, err error) {
	if len(tools) == 0 {
		return "", "", fmt.Errorf("tools: BuildGrammar: empty tools slice")
	}

	// Collision check: two distinct tool names may map to the same sanitized
	// rule name (e.g. "get-weather" and "get_weather" both → "get-weather").
	seen := make(map[string]string, len(tools)) // safeRuleName → original name
	for _, t := range tools {
		safe := sanitizeRuleName(t.Function.Name)
		if orig, exists := seen[safe]; exists {
			return "", "", fmt.Errorf(
				"tools: rule name collision: %q and %q both sanitize to %q",
				orig, t.Function.Name, safe,
			)
		}
		seen[safe] = t.Function.Name
	}

	var b grammarBuilder
	b.build(tools)
	return b.String(), `\{`, nil
}

// grammarBuilder accumulates GBNF rules.
type grammarBuilder struct {
	rules   []string
	seenSet map[string]bool
}

func (b *grammarBuilder) addRule(name, body string) {
	if b.seenSet == nil {
		b.seenSet = make(map[string]bool)
	}
	if b.seenSet[name] {
		return
	}
	b.seenSet[name] = true
	b.rules = append(b.rules, fmt.Sprintf("%s ::= %s", name, body))
}

func (b *grammarBuilder) String() string {
	return strings.Join(b.rules, "\n")
}

func (b *grammarBuilder) build(tools []Tool) {
	// Common primitives.
	b.addRule("ws", `[ \t\n]*`)
	b.addRule("string", `"\"" ( [^"\\] | "\\" (["\\/bfnrt] | "u" [0-9a-fA-F] [0-9a-fA-F] [0-9a-fA-F] [0-9a-fA-F]) )* "\""`)
	b.addRule("integer", `"-"? ([0-9] | [1-9] [0-9]*)`)
	b.addRule("number", `"-"? ([0-9] | [1-9] [0-9]*) ("." [0-9]+)? ([eE] [-+]? [0-9]+)?`)
	b.addRule("boolean", `"true" | "false"`)
	b.addRule("null", `"null"`)
	b.addRule("json-value", `string | number | "true" | "false" | "null" | json-object | json-array`)
	b.addRule("json-object", `"{" ws ( string ws ":" ws json-value ws ("," ws string ws ":" ws json-value ws)* )? "}"`)
	b.addRule("json-array", `"[" ws ( json-value ws ("," ws json-value ws)* )? "]"`)

	// Per-tool argument schema rules.
	argRules := make([]string, 0, len(tools))
	nameAlts := make([]string, 0, len(tools))

	for _, t := range tools {
		fn := t.Function
		safeName := sanitizeRuleName(fn.Name)
		nameAlts = append(nameAlts, fmt.Sprintf(`"\"%s\""`, fn.Name))
		argsRule := fmt.Sprintf("args-%s", safeName)
		b.buildObjectRule(argsRule, fn.Parameters)
		argRules = append(argRules, argsRule)
	}

	// name rule: union of quoted tool names.
	b.addRule("tool-name", strings.Join(nameAlts, " | "))

	// args rule: union of per-tool arg schemas.
	// The JSON parser on our side validates name↔arguments pairing.
	b.addRule("tool-args", strings.Join(argRules, " | "))

	// Root rule.
	b.addRule("toolcall", `"{" ws "\"name\"" ws ":" ws tool-name ws "," ws "\"arguments\"" ws ":" ws tool-args ws "}"`)
	b.addRule("root", "toolcall")
}

// buildObjectRule emits GBNF rules for an object schema.
// schema is a map[string]any representing a JSON schema object.
//
// Required properties are emitted as a fixed sequence. Optional properties are
// each wrapped in ( ... )? so the model may omit them.
func (b *grammarBuilder) buildObjectRule(ruleName string, schema map[string]any) {
	// Extract properties and required list from schema.
	properties, _ := schema["properties"].(map[string]any)
	requiredRaw, _ := schema["required"].([]any)

	requiredSet := make(map[string]bool, len(requiredRaw))
	for _, r := range requiredRaw {
		if s, ok := r.(string); ok {
			requiredSet[s] = true
		}
	}

	if len(properties) == 0 {
		// Empty or unknown schema → permissive object.
		b.addRule(ruleName, "json-object")
		return
	}

	// Separate and sort keys for deterministic output.
	var reqKeys, optKeys []string
	for k := range properties {
		if requiredSet[k] {
			reqKeys = append(reqKeys, k)
		} else {
			optKeys = append(optKeys, k)
		}
	}
	sort.Strings(reqKeys)
	sort.Strings(optKeys)

	// Emit per-property value rules.
	emitField := func(key string) string {
		propSchema, _ := properties[key].(map[string]any)
		valueRule := fmt.Sprintf("%s--%s", ruleName, sanitizeRuleName(key))
		b.buildValueRule(valueRule, propSchema)
		// Use the key directly as a GBNF string literal: "\"key\"".
		return fmt.Sprintf(`"\"%s\"" ws ":" ws %s`, key, valueRule)
	}

	// Build the object body.
	// Required fields form a mandatory sequence (comma-separated).
	// Optional fields follow, each as  ( ws "," ws <field> )?
	var body strings.Builder
	body.WriteString(`"{" ws`)

	for i, k := range reqKeys {
		if i == 0 {
			body.WriteString(` `)
		} else {
			body.WriteString(` ws "," ws `)
		}
		body.WriteString(emitField(k))
	}

	for i, k := range optKeys {
		field := emitField(k)
		if len(reqKeys) == 0 && i == 0 {
			// First field overall (no required fields) — no leading comma.
			body.WriteString(fmt.Sprintf(` ( %s )?`, field))
		} else {
			// Subsequent optional fields after required or previous optional.
			body.WriteString(fmt.Sprintf(` ( ws "," ws %s )?`, field))
		}
	}

	body.WriteString(` ws "}"`)
	b.addRule(ruleName, body.String())
}

// buildValueRule emits a rule for a JSON-schema value.
func (b *grammarBuilder) buildValueRule(ruleName string, schema map[string]any) {
	if schema == nil {
		b.addRule(ruleName, "json-value")
		return
	}

	// Handle enum first (takes priority over type).
	if enumVals, ok := schema["enum"].([]any); ok && len(enumVals) > 0 {
		alts := make([]string, 0, len(enumVals))
		for _, v := range enumVals {
			if s, ok := v.(string); ok {
				alts = append(alts, fmt.Sprintf(`"\"%s\""`, s))
			}
		}
		if len(alts) > 0 {
			b.addRule(ruleName, strings.Join(alts, " | "))
			return
		}
	}

	typStr, _ := schema["type"].(string)
	switch typStr {
	case "string":
		b.addRule(ruleName, "string")
	case "integer":
		b.addRule(ruleName, "integer")
	case "number":
		b.addRule(ruleName, "number")
	case "boolean":
		b.addRule(ruleName, "boolean")
	case "null":
		b.addRule(ruleName, "null")
	case "array":
		itemSchema, _ := schema["items"].(map[string]any)
		itemRule := ruleName + "-item"
		b.buildValueRule(itemRule, itemSchema)
		b.addRule(ruleName, fmt.Sprintf(`"[" ws ( %s ws ("," ws %s ws)* )? "]"`, itemRule, itemRule))
	case "object":
		b.buildObjectRule(ruleName, schema)
	default:
		// Unknown type — fall back to permissive json-value.
		slog.Warn("tools: grammar: unsupported schema type, using json-value fallback",
			"rule", ruleName, "type", typStr)
		b.addRule(ruleName, "json-value")
	}
}

// sanitizeRuleName converts a tool/property name to a safe GBNF rule identifier.
// Only letters, digits, and hyphens are allowed; everything else becomes '-'.
func sanitizeRuleName(name string) string {
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	return b.String()
}
