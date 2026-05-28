// ndjson.go — newline-delimited JSON writer for streaming responses.
package api

import (
	"encoding/json"
	"net/http"
)

// NDJSONWriter streams JSON objects as NDJSON (one per line).
type NDJSONWriter struct {
	w   http.ResponseWriter
	enc *json.Encoder
}

// NewNDJSONWriter returns an NDJSONWriter that sets streaming headers.
func NewNDJSONWriter(w http.ResponseWriter) *NDJSONWriter {
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Transfer-Encoding", "chunked")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	return &NDJSONWriter{w: w, enc: json.NewEncoder(w)}
}

// Write encodes v as JSON followed by a newline and flushes.
func (n *NDJSONWriter) Write(v any) error {
	if err := n.enc.Encode(v); err != nil {
		return err
	}
	if f, ok := n.w.(http.Flusher); ok {
		f.Flush()
	}
	return nil
}

// writeJSON writes a single JSON response (non-streaming).
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, ErrorResponse{Error: msg})
}
