// Package tools provides helpers for tool calling (function calling) support.
package tools

// ToolFunction describes the callable function inside a tool definition.
// This mirrors api.ToolFunction and is duplicated here to avoid circular imports.
type ToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters"`
}

// Tool is a function definition provided by the client.
type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

// ToolCall is the parsed result of a model-generated tool invocation.
type ToolCall struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}
