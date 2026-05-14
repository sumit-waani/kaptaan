// Package sandbox is a minimal client for the Daytona Cloud API.
// It speaks the REST control plane (https://api.daytona.io) for workspace
// lifecycle and the in-workspace Toolbox HTTP API for shell + filesystem ops.
//
// Only what the agent needs is implemented: Create, Connect, NewHandle, Ping,
// Run (synchronous bash command), WriteFile, ReadFile.
package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const apiBase = "https://api.daytona.io"

// Sandbox is a handle to a live Daytona workspace.
type Sandbox struct {
	ID     string
	apiKey string
}

// workspaceState is the state sub-object returned by the Daytona API.
type workspaceState struct {
	Name string `json:"name"`
}

// workspaceInfo is a partial workspace response from Daytona.
type workspaceInfo struct {
	ID    string         `json:"id"`
	State workspaceState `json:"state"`
}

// RunResult holds the outcome of one shell command.
type RunResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Status   string
}

func doReq(ctx context.Context, apiKey, method, path string, body interface{}) (*http.Response, error) {
	var buf io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		buf = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, apiBase+path, buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	cl := &http.Client{Timeout: 60 * time.Second}
	return cl.Do(req)
}

// Create starts a new Daytona workspace and blocks until it is ready.
// The template and timeoutSecs parameters are accepted for API compatibility
// but are not used — Daytona manages its own image and lifecycle.
func Create(ctx context.Context, apiKey, _ string, _ int) (*Sandbox, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("daytona_api_key is empty")
	}
	name := fmt.Sprintf("kaptaan-%d", time.Now().UnixMilli())
	payload := map[string]interface{}{
		"name":  name,
		"image": "daytonaio/sandbox:10.0",
		"user":  "daytona",
	}
	res, err := doReq(ctx, apiKey, http.MethodPost, "/workspace", payload)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		b, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("create workspace: %s: %s", res.Status, string(b))
	}
	var ws workspaceInfo
	if err := json.NewDecoder(res.Body).Decode(&ws); err != nil {
		return nil, fmt.Errorf("decode workspace response: %w", err)
	}
	if ws.ID == "" {
		return nil, fmt.Errorf("workspace created but ID is empty")
	}
	sb := &Sandbox{ID: ws.ID, apiKey: apiKey}
	if err := sb.waitReady(ctx, 90*time.Second); err != nil {
		return nil, fmt.Errorf("workspace not ready: %w", err)
	}
	return sb, nil
}

// NewHandle returns a Sandbox handle from a stored workspace ID without any
// API calls. Useful for cheap Ping checks before calling Connect.
func NewHandle(apiKey, id string) *Sandbox {
	return &Sandbox{apiKey: apiKey, ID: id}
}

// Connect starts a stopped (auto-paused) Daytona workspace by ID and waits
// until the toolbox is reachable. id is the plain workspace ID as stored in DB.
func Connect(ctx context.Context, apiKey, id string) (*Sandbox, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("daytona_api_key is empty")
	}
	if id == "" {
		return nil, fmt.Errorf("workspace id is empty")
	}
	sb := &Sandbox{apiKey: apiKey, ID: id}

	// POST /workspace/{id}/start is idempotent — safe to call even if running.
	res, err := doReq(ctx, apiKey, http.MethodPost, "/workspace/"+id+"/start", nil)
	if err != nil {
		return nil, fmt.Errorf("start workspace: %w", err)
	}
	res.Body.Close()
	if res.StatusCode/100 != 2 {
		return nil, fmt.Errorf("start workspace: HTTP %d", res.StatusCode)
	}

	if err := sb.waitReady(ctx, 90*time.Second); err != nil {
		return nil, fmt.Errorf("workspace not ready after start: %w", err)
	}
	return sb, nil
}

// Ping returns true if the workspace is currently in "started" state.
func (s *Sandbox) Ping(ctx context.Context) bool {
	hctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	res, err := doReq(hctx, s.apiKey, http.MethodGet, "/workspace/"+s.ID, nil)
	if err != nil {
		return false
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return false
	}
	var ws workspaceInfo
	if err := json.NewDecoder(res.Body).Decode(&ws); err != nil {
		return false
	}
	return ws.State.Name == "started"
}

// waitReady polls until workspace state is "started" or the timeout passes.
func (s *Sandbox) waitReady(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if s.Ping(ctx) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return fmt.Errorf("timed out waiting for workspace %s to be ready", s.ID)
}

// executeRequest is the Daytona toolbox process execute request body.
type executeRequest struct {
	Command string            `json:"command"`
	Cwd     string            `json:"cwd,omitempty"`
	Timeout int               `json:"timeout,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

// executeResponse is the Daytona toolbox process execute response.
type executeResponse struct {
	Code   int    `json:"code"`
	Result string `json:"result"`
	Stderr string `json:"stderr"`
}

// Run executes cmd synchronously in the workspace via the Toolbox API.
// cwd and env are optional. stdout/stderr and exit code are returned.
func (s *Sandbox) Run(ctx context.Context, cmd, cwd string, env map[string]string, timeout time.Duration) (*RunResult, error) {
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	payload := executeRequest{
		Command: cmd,
		Cwd:     cwd,
		Timeout: int(timeout.Seconds()),
	}
	if len(env) > 0 {
		payload.Env = env
	}

	rctx, cancel := context.WithTimeout(ctx, timeout+30*time.Second)
	defer cancel()

	res, err := doReq(rctx, s.apiKey, http.MethodPost,
		"/workspace/"+s.ID+"/toolbox/process/execute", payload)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		b, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("toolbox execute: %s: %s", res.Status, string(b))
	}

	var out executeResponse
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode execute response: %w", err)
	}
	return &RunResult{
		Stdout:   out.Result,
		Stderr:   out.Stderr,
		ExitCode: out.Code,
	}, nil
}

// WriteFile uploads content to an absolute path inside the workspace.
func (s *Sandbox) WriteFile(ctx context.Context, path string, content []byte) error {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("file", "file")
	if err != nil {
		return err
	}
	if _, err := fw.Write(content); err != nil {
		return err
	}
	mw.Close()

	u := apiBase + "/workspace/" + s.ID + "/toolbox/files/upload?path=" + url.QueryEscape(path)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, &body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", mw.FormDataContentType())

	cl := &http.Client{Timeout: 120 * time.Second}
	res, err := cl.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		b, _ := io.ReadAll(res.Body)
		return fmt.Errorf("upload %s: %s: %s", path, res.Status, string(b))
	}
	return nil
}

// ReadFile downloads the file at the given absolute path from the workspace.
func (s *Sandbox) ReadFile(ctx context.Context, path string) ([]byte, error) {
	u := apiBase + "/workspace/" + s.ID + "/toolbox/files/download?path=" + url.QueryEscape(path)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+s.apiKey)

	cl := &http.Client{Timeout: 60 * time.Second}
	res, err := cl.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		b, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("download %s: %s: %s", path, res.Status, string(b))
	}
	return io.ReadAll(res.Body)
}

// truncate is a local helper used in error formatting.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// shellQuote wraps s in single quotes, escaping any existing single quotes.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
