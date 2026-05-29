package tools

import (
	"encoding/json"
	"strings"
)

// ParseToolCall scans s for the first balanced JSON object whose structure
// matches {"name": <string>, "arguments": <object>}. Returns the parsed
// ToolCall and true on success; returns nil, false if no valid tool call
// is found.
//
// The function skips leading prose, so models that emit a preamble before the
// JSON object are handled correctly.
func ParseToolCall(s string) (*ToolCall, bool) {
	// Find the first '{' and try to decode a balanced JSON object from that position.
	for i := 0; i < len(s); i++ {
		if s[i] != '{' {
			continue
		}
		end := findBalancedEnd(s, i)
		if end < 0 {
			continue
		}
		candidate := s[i : end+1]
		tc := tryDecodeToolCall(candidate)
		if tc != nil {
			return tc, true
		}
	}
	return nil, false
}

// tryDecodeToolCall attempts to decode a raw JSON string as a ToolCall.
// Returns nil if the structure does not match.
func tryDecodeToolCall(raw string) *ToolCall {
	var obj struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal([]byte(raw), &obj); err != nil {
		return nil
	}
	if obj.Name == "" {
		return nil
	}
	return &ToolCall{Name: obj.Name, Arguments: obj.Arguments}
}

// findBalancedEnd returns the index of the closing '}' that balances the '{'
// at position start in s. Returns -1 if no balanced close is found.
// It correctly handles nested objects, arrays, and JSON strings (including
// escaped characters).
func findBalancedEnd(s string, start int) int {
	depth := 0
	inString := false
	for i := start; i < len(s); i++ {
		c := s[i]
		if inString {
			if c == '\\' {
				i++ // skip escaped character
				continue
			}
			if c == '"' {
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{', '[':
			depth++
		case '}', ']':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// CleanOutput strips any tool-call JSON object that was emitted but should be
// hidden from the content field. Returns the prose portion only.
func CleanOutput(s string, tc *ToolCall) string {
	if tc == nil {
		return s
	}
	// Find and remove the first JSON object from the output.
	for i := 0; i < len(s); i++ {
		if s[i] != '{' {
			continue
		}
		end := findBalancedEnd(s, i)
		if end < 0 {
			continue
		}
		candidate := s[i : end+1]
		if tryDecodeToolCall(candidate) != nil {
			return strings.TrimSpace(s[:i] + s[end+1:])
		}
	}
	return s
}
