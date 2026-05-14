// Package sandbox is a minimal client for the Daytona Cloud API.
// It speaks the REST control plane (https://app.daytona.io/api) for sandbox
// lifecycle and the Toolbox proxy URL (returned per-sandbox) for shell + filesystem ops.
//
// Toolbox URL pattern: {toolboxProxyUrl}/{sandboxID}/{operation}
// e.g. https://proxy.app.daytona.io/toolbox/{id}/process/execute
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

const apiBase = "https://app.daytona.io/api"

// Sandbox is a handle to a live Daytona sandbox.
type Sandbox struct {
	ID         string
	apiKey     string
	orgID      string
	toolboxURL string // {toolboxProxyUrl}/{sandboxID}
}

// sandboxInfo is the partial sandbox response from the Daytona API.
type sandboxInfo struct {
	ID              string `json:"id"`
	State           string `json:"state"`
	ToolboxProxyURL string `json:"toolboxProxyUrl"`
}

// RunResult holds the outcome of one shell command.
type RunResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Status   string
}

// doReq makes an authenticated request to the Daytona control-plane API.
func doReq(ctx context.Context, apiKey, orgID, method, path string, body interface{}) (*http.Response, error) {
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
	if orgID != "" {
		req.Header.Set("X-Daytona-Organization-ID", orgID)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	cl := &http.Client{Timeout: 60 * time.Second}
	return cl.Do(req)
}

// doToolboxReq makes an authenticated request to the sandbox toolbox proxy.
func doToolboxReq(ctx context.Context, apiKey, method, fullURL string, body interface{}) (*http.Response, error) {
	var buf io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		buf = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, fullURL, buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	cl := &http.Client{Timeout: 120 * time.Second}
	return cl.Do(req)
}

// buildToolboxURL constructs the toolbox base URL from the proxy URL and sandbox ID.
// Pattern: {toolboxProxyUrl}/{sandboxID}
func buildToolboxURL(proxyURL, sandboxID string) string {
	return strings.TrimRight(proxyURL, "/") + "/" + sandboxID
}

// getSandboxInfo fetches current sandbox info including toolboxProxyUrl.
func getSandboxInfo(ctx context.Context, apiKey, orgID, sandboxID string) (*sandboxInfo, error) {
	hctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	res, err := doReq(hctx, apiKey, orgID, http.MethodGet, "/sandbox/"+sandboxID, nil)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("get sandbox: HTTP %d: %s", res.StatusCode, string(b))
	}
	var info sandboxInfo
	if err := json.NewDecoder(res.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("decode sandbox info: %w", err)
	}
	return &info, nil
}

// ping fetches sandbox state and updates toolboxURL. Returns true if "started".
func (s *Sandbox) ping(ctx context.Context) bool {
	info, err := getSandboxInfo(ctx, s.apiKey, s.orgID, s.ID)
	if err != nil {
		return false
	}
	if info.ToolboxProxyURL != "" {
		s.toolboxURL = buildToolboxURL(info.ToolboxProxyURL, info.ID)
	}
	return info.State == "started"
}

// waitReady polls until the sandbox state is "started" or the timeout passes.
func (s *Sandbox) waitReady(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if s.ping(ctx) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return fmt.Errorf("timed out waiting for sandbox %s to be ready", s.ID)
}

// ── Lifecycle ────────────────────────────────────────────────────────────────

// Create starts a new Daytona sandbox and blocks until it is ready.
func Create(ctx context.Context, apiKey, orgID string) (*Sandbox, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("daytona_api_key is empty")
	}
	name := fmt.Sprintf("kaptaan-%d", time.Now().UnixMilli())
	res, err := doReq(ctx, apiKey, orgID, http.MethodPost, "/sandbox", map[string]interface{}{"name": name})
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		b, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("create sandbox: %s: %s", res.Status, string(b))
	}
	var info sandboxInfo
	if err := json.NewDecoder(res.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("decode create response: %w", err)
	}
	if info.ID == "" {
		return nil, fmt.Errorf("sandbox created but ID is empty")
	}
	sb := &Sandbox{ID: info.ID, apiKey: apiKey, orgID: orgID}
	if info.ToolboxProxyURL != "" {
		sb.toolboxURL = buildToolboxURL(info.ToolboxProxyURL, info.ID)
	}
	if err := sb.waitReady(ctx, 120*time.Second); err != nil {
		return nil, fmt.Errorf("sandbox not ready: %w", err)
	}
	return sb, nil
}

// Resume returns a ready *Sandbox for an existing sandbox ID.
// If the sandbox is already started, it returns immediately.
// If it is stopped/paused, it starts it and waits until ready.
func Resume(ctx context.Context, apiKey, orgID, id string) (*Sandbox, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("daytona_api_key is empty")
	}
	if id == "" {
		return nil, fmt.Errorf("sandbox id is empty")
	}
	sb := &Sandbox{ID: id, apiKey: apiKey, orgID: orgID}

	info, err := getSandboxInfo(ctx, apiKey, orgID, id)
	if err != nil {
		return nil, fmt.Errorf("get sandbox info: %w", err)
	}
	if info.ToolboxProxyURL != "" {
		sb.toolboxURL = buildToolboxURL(info.ToolboxProxyURL, id)
	}

	if info.State == "started" {
		return sb, nil
	}

	res, err := doReq(ctx, apiKey, orgID, http.MethodPost, "/sandbox/"+id+"/start", nil)
	if err != nil {
		return nil, fmt.Errorf("start sandbox: %w", err)
	}
	res.Body.Close()
	if res.StatusCode/100 != 2 {
		return nil, fmt.Errorf("start sandbox: HTTP %d", res.StatusCode)
	}

	if err := sb.waitReady(ctx, 120*time.Second); err != nil {
		return nil, fmt.Errorf("sandbox not ready after start: %w", err)
	}
	return sb, nil
}

// Stop pauses a running sandbox (non-blocking on Daytona's side).
func Stop(ctx context.Context, apiKey, orgID, id string) error {
	res, err := doReq(ctx, apiKey, orgID, http.MethodPost, "/sandbox/"+id+"/stop", nil)
	if err != nil {
		return err
	}
	res.Body.Close()
	if res.StatusCode/100 != 2 {
		return fmt.Errorf("stop sandbox: HTTP %d", res.StatusCode)
	}
	return nil
}

// DeleteSandbox permanently removes a sandbox from Daytona.
func DeleteSandbox(ctx context.Context, apiKey, orgID, id string) error {
	res, err := doReq(ctx, apiKey, orgID, http.MethodDelete, "/sandbox/"+id, nil)
	if err != nil {
		return err
	}
	res.Body.Close()
	if res.StatusCode/100 != 2 && res.StatusCode != http.StatusNotFound {
		return fmt.Errorf("delete sandbox: HTTP %d", res.StatusCode)
	}
	return nil
}

// State returns the current state of a sandbox (e.g. "started", "stopped").
func State(ctx context.Context, apiKey, orgID, id string) (string, error) {
	info, err := getSandboxInfo(ctx, apiKey, orgID, id)
	if err != nil {
		return "unknown", err
	}
	return info.State, nil
}

// ── Toolbox operations ───────────────────────────────────────────────────────

// executeRequest is the Daytona toolbox process/execute request body.
type executeRequest struct {
	Command string            `json:"command"`
	Cwd     string            `json:"cwd,omitempty"`
	Timeout int               `json:"timeout,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

// executeResponse is the Daytona toolbox process/execute response.
type executeResponse struct {
	Code   int    `json:"code"`
	Result string `json:"result"`
	Stderr string `json:"stderr"`
}

// Run executes a command synchronously in the sandbox via the Toolbox API.
func (s *Sandbox) Run(ctx context.Context, cmd, cwd string, env map[string]string, timeout time.Duration) (*RunResult, error) {
	if s.toolboxURL == "" {
		return nil, fmt.Errorf("toolbox URL not set — sandbox may not be fully ready")
	}
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

	endpoint := s.toolboxURL + "/process/execute"
	res, err := doToolboxReq(rctx, s.apiKey, http.MethodPost, endpoint, payload)
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

// WriteFile uploads content to an absolute path inside the sandbox.
func (s *Sandbox) WriteFile(ctx context.Context, path string, content []byte) error {
	if s.toolboxURL == "" {
		return fmt.Errorf("toolbox URL not set")
	}
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

	u := s.toolboxURL + "/files/upload?path=" + url.QueryEscape(path)
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

// ReadFile downloads the file at an absolute path from the sandbox.
func (s *Sandbox) ReadFile(ctx context.Context, path string) ([]byte, error) {
	if s.toolboxURL == "" {
		return nil, fmt.Errorf("toolbox URL not set")
	}
	u := s.toolboxURL + "/files/download?path=" + url.QueryEscape(path)
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

// ── Helpers ──────────────────────────────────────────────────────────────────

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
