package tools

import (
	"encoding/json"
	"fmt"
	"strings"
)

// RenderToolPreface builds a system-message preamble that lists the available
// tools in a compact, model-agnostic format. This is prepended to the system
// message before the chat template is applied, since yzma's ChatApplyTemplate
// does not expose llama.cpp's native tool-template rendering.
//
// The output instructs the model to respond with ONLY a JSON object when a
// tool is needed, and to respond normally otherwise.
func RenderToolPreface(tools []Tool) string {
	var b strings.Builder
	b.WriteString("You have access to the following tools. ")
	b.WriteString("When a tool is needed, respond ONLY with a single JSON object:\n")
	b.WriteString(`  {"name": "<tool_name>", "arguments": { ... }}` + "\n")
	b.WriteString("Otherwise respond normally.\n\n")
	b.WriteString("Tools:\n")
	for _, t := range tools {
		fn := t.Function
		b.WriteString(fmt.Sprintf("- name: %s\n", fn.Name))
		if fn.Description != "" {
			b.WriteString(fmt.Sprintf("  description: %s\n", fn.Description))
		}
		if fn.Parameters != nil {
			paramsJSON, err := json.Marshal(fn.Parameters)
			if err == nil {
				b.WriteString(fmt.Sprintf("  parameters: %s\n", string(paramsJSON)))
			}
		}
	}
	return b.String()
}

// RewriteToolMessages converts any messages with role "tool" into a
// user-role message so that yzma's ChatApplyTemplate (which only knows
// user/assistant/system roles) can render them correctly. The tool result
// content is preserved verbatim inside a structured wrapper.
//
// Messages with other roles are returned unchanged.
func RewriteToolMessages(messages []Message) []Message {
	out := make([]Message, 0, len(messages))
	for _, m := range messages {
		if m.Role != "tool" {
			out = append(out, m)
			continue
		}
		toolName := m.ToolName
		if toolName == "" {
			toolName = "unknown"
		}
		rewritten := Message{
			Role:    "user",
			Content: fmt.Sprintf("Tool result for %s: %s", toolName, m.Content),
		}
		out = append(out, rewritten)
	}
	return out
}

// Message is a local alias used by RewriteToolMessages. It mirrors
// api.Message to avoid a circular import between the tools and api packages.
// The caller (handlers.go) converts between the two types.
type Message struct {
	Role      string     `json:"role"`
	Content   string     `json:"content"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	ToolName  string     `json:"tool_name,omitempty"`
}
