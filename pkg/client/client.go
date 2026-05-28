// Package client is a Go client for the Beeket HTTP API.
package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Client is a Beeket API client.
type Client struct {
	base       string
	httpClient *http.Client
}

// New creates a client pointing at the given server URL (e.g. "http://127.0.0.1:11435").
func New(baseURL string) *Client {
	return &Client{
		base:       strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{Timeout: 0}, // no timeout — streaming
	}
}

// ---- Model types (mirrors internal/api types) ----

type ModelDetails struct {
	Family            string `json:"family"`
	ParameterSize     string `json:"parameter_size"`
	QuantizationLevel string `json:"quantization_level"`
	ContextLength     int    `json:"context_length"`
	Format            string `json:"format"`
}

type ModelInfo struct {
	Name       string       `json:"name"`
	Model      string       `json:"model"`
	Size       int64        `json:"size"`
	Digest     string       `json:"digest"`
	ModifiedAt time.Time    `json:"modified_at"`
	Details    ModelDetails `json:"details"`
}

type ShowResponse struct {
	Name    string       `json:"name"`
	Details ModelDetails `json:"details"`
}

type PSModel struct {
	Name     string    `json:"name"`
	Size     int64     `json:"size"`
	LastUsed time.Time `json:"expires_at"`
}

// ---- API methods ----

// Pull downloads a model, calling progress for each status event.
func (c *Client) Pull(ctx context.Context, name string, progress func(status, digest string, total, completed int64)) error {
	body, _ := json.Marshal(map[string]any{"name": name, "stream": true})
	resp, err := c.post(ctx, "/api/pull", body)
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close() //nolint:errcheck // response body close error is not actionable
	}()

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		var ev struct {
			Status    string `json:"status"`
			Digest    string `json:"digest"`
			Total     int64  `json:"total"`
			Completed int64  `json:"completed"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			continue
		}
		if progress != nil {
			progress(ev.Status, ev.Digest, ev.Total, ev.Completed)
		}
	}
	return scanner.Err()
}

// List returns all installed models.
func (c *Client) List(ctx context.Context) ([]ModelInfo, error) {
	resp, err := c.get(ctx, "/api/tags")
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = resp.Body.Close() //nolint:errcheck
	}()
	var r struct {
		Models []ModelInfo `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, err
	}
	return r.Models, nil
}

// Show returns details for one model.
func (c *Client) Show(ctx context.Context, name string) (*ShowResponse, error) {
	body, _ := json.Marshal(map[string]string{"name": name})
	resp, err := c.post(ctx, "/api/show", body)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = resp.Body.Close() //nolint:errcheck
	}()
	var r ShowResponse
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, err
	}
	return &r, nil
}

// Delete removes a model.
func (c *Client) Delete(ctx context.Context, name string) error {
	body, _ := json.Marshal(map[string]string{"name": name})
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.base+"/api/delete", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("delete: HTTP %d", resp.StatusCode)
	}
	return nil
}

// Generate streams generated text pieces for a prompt.
func (c *Client) Generate(ctx context.Context, model, prompt string, out func(piece string)) error {
	body, _ := json.Marshal(map[string]any{"model": model, "prompt": prompt, "stream": true})
	resp, err := c.post(ctx, "/api/generate", body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		var ev struct {
			Response string `json:"response"`
			Done     bool   `json:"done"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			continue
		}
		if out != nil && ev.Response != "" {
			out(ev.Response)
		}
		if ev.Done {
			break
		}
	}
	return scanner.Err()
}

// GenerateSync runs a generation and returns the full response string.
func (c *Client) GenerateSync(ctx context.Context, model, prompt string) (string, error) {
	var sb strings.Builder
	if err := c.Generate(ctx, model, prompt, func(p string) { sb.WriteString(p) }); err != nil {
		return "", err
	}
	return sb.String(), nil
}

// PS returns currently loaded models.
func (c *Client) PS(ctx context.Context) ([]PSModel, error) {
	resp, err := c.get(ctx, "/api/ps")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var r struct {
		Models []PSModel `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, err
	}
	return r.Models, nil
}

// Version returns the server version string.
func (c *Client) Version(ctx context.Context) (string, error) {
	resp, err := c.get(ctx, "/api/version")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var r struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "", err
	}
	return r.Version, nil
}

// ---- HTTP helpers ----

func (c *Client) post(ctx context.Context, path string, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		resp.Body.Close()
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, path)
	}
	return resp, nil
}

func (c *Client) get(ctx context.Context, path string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		resp.Body.Close()
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, path)
	}
	return resp, nil
}
