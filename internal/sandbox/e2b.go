// Package sandbox is a minimal client for the E2B sandbox API. It speaks the
// REST control plane (https://api.e2b.dev) for create/kill and the in-sandbox
// envd HTTP API for shell + filesystem operations.
//
// Only what the builder needs is implemented: Create, Kill, Run (synchronous
// bash command), WriteFile, ReadFile.
package sandbox

import (
        "bytes"
        "context"
        "encoding/base64"
        "encoding/binary"
        "encoding/json"
        "fmt"
        "io"
        "mime/multipart"
        "net/http"
        "net/url"
        "strings"
        "time"
)

const (
        apiBase  = "https://api.e2b.dev"
        envdPort = 49983
)

// Sandbox is a handle to a live E2B sandbox.
type Sandbox struct {
        ID       string `json:"sandboxID"`
        ClientID string `json:"clientID"`
        Template string `json:"templateID"`

        apiKey string
}

func (s *Sandbox) host() string {
        return fmt.Sprintf("https://%d-%s-%s.e2b.app", envdPort, s.ID, s.ClientID)
}

// Create starts a new sandbox using `template` (default "base") and asks the
// platform to keep it alive for at most `timeoutSecs` seconds. The call blocks
// until envd is reachable so that subsequent Run/WriteFile calls succeed.
func Create(ctx context.Context, apiKey, template string, timeoutSecs int) (*Sandbox, error) {
        if apiKey == "" {
                return nil, fmt.Errorf("E2B_API_KEY is empty")
        }
        if template == "" {
                template = "base"
        }
        if timeoutSecs <= 0 {
                timeoutSecs = 600
        }
        body, _ := json.Marshal(map[string]any{
                "templateID": template,
                "timeout":    timeoutSecs,
                "lifecycle":  map[string]any{"onTimeout": "pause"},
        })
        req, _ := http.NewRequestWithContext(ctx, http.MethodPost, apiBase+"/sandboxes", bytes.NewReader(body))
        req.Header.Set("X-API-Key", apiKey)
        req.Header.Set("Content-Type", "application/json")

        cl := &http.Client{Timeout: 60 * time.Second}
        res, err := cl.Do(req)
        if err != nil {
                return nil, err
        }
        defer res.Body.Close()
        if res.StatusCode/100 != 2 {
                b, _ := io.ReadAll(res.Body)
                return nil, fmt.Errorf("create sandbox: %s: %s", res.Status, string(b))
        }
        sb := &Sandbox{apiKey: apiKey}
        if err := json.NewDecoder(res.Body).Decode(sb); err != nil {
                return nil, err
        }

        // Wait for envd to come up — usually < 2s.
        deadline := time.Now().Add(20 * time.Second)
        for time.Now().Before(deadline) {
                hctx, cancel := context.WithTimeout(ctx, 3*time.Second)
                hreq, _ := http.NewRequestWithContext(hctx, http.MethodGet, sb.host()+"/health", nil)
                r, herr := http.DefaultClient.Do(hreq)
                cancel()
                if herr == nil {
                        r.Body.Close()
                        if r.StatusCode == 204 || r.StatusCode == 200 {
                                return sb, nil
                        }
                }
                time.Sleep(500 * time.Millisecond)
        }
        // Return the sandbox anyway; first Run will surface the issue.
        return sb, nil
}

// Kill destroys the sandbox.
func (s *Sandbox) Kill(ctx context.Context) error {
        if s == nil || s.ID == "" {
                return nil
        }
        req, _ := http.NewRequestWithContext(ctx, http.MethodDelete, apiBase+"/sandboxes/"+s.ID, nil)
        req.Header.Set("X-API-Key", s.apiKey)
        cl := &http.Client{Timeout: 30 * time.Second}
        res, err := cl.Do(req)
        if err != nil {
                return err
        }
        res.Body.Close()
        return nil
}

// RunResult holds the outcome of one sandbox shell command.
type RunResult struct {
        Stdout   string
        Stderr   string
        ExitCode int
        Status   string
}

// Run executes `cmd` in bash inside the sandbox, blocking until exit or until
// `timeout` elapses. `cwd` and `env` are optional. stdout/stderr are returned
// as strings (decoded from envd's base64 stream).
func (s *Sandbox) Run(ctx context.Context, cmd, cwd string, env map[string]string, timeout time.Duration) (*RunResult, error) {
        if timeout <= 0 {
                timeout = 60 * time.Second
        }
        rctx, cancel := context.WithTimeout(ctx, timeout)
        defer cancel()

        proc := map[string]any{
                "cmd":  "/bin/bash",
                "args": []string{"-c", cmd},
        }
        if cwd != "" {
                proc["cwd"] = cwd
        }
        if len(env) > 0 {
                // envd's ProcessConfig.envs is a proto map<string,string>; in Connect-RPC
                // JSON that's a plain JSON object, not an array of {key,value} pairs.
                proc["envs"] = env
        }
        payload, _ := json.Marshal(map[string]any{"process": proc})
        framed := makeFrame(payload)

        req, _ := http.NewRequestWithContext(rctx, http.MethodPost,
                s.host()+"/process.Process/Start", bytes.NewReader(framed))
        req.Header.Set("Content-Type", "application/connect+json")
        req.Header.Set("Connect-Protocol-Version", "1")

        cl := &http.Client{Timeout: timeout + 30*time.Second}
        res, err := cl.Do(req)
        if err != nil {
                return nil, err
        }
        defer res.Body.Close()
        if res.StatusCode/100 != 2 {
                b, _ := io.ReadAll(res.Body)
                return nil, fmt.Errorf("envd Start: %s: %s", res.Status, string(b))
        }

        var stdout, stderr bytes.Buffer
        out := &RunResult{ExitCode: -1}

        for {
                var hdr [5]byte
                if _, rerr := io.ReadFull(res.Body, hdr[:]); rerr != nil {
                        if rerr == io.EOF || rerr == io.ErrUnexpectedEOF {
                                break
                        }
                        return nil, fmt.Errorf("read frame header: %w", rerr)
                }
                flags := hdr[0]
                n := binary.BigEndian.Uint32(hdr[1:5])
                buf := make([]byte, n)
                if _, rerr := io.ReadFull(res.Body, buf); rerr != nil {
                        return nil, fmt.Errorf("read frame body: %w", rerr)
                }
                if flags&0x02 != 0 {
                        // End-of-stream envelope; may carry an error.
                        var trailer struct {
                                Error *struct {
                                        Code    string `json:"code"`
                                        Message string `json:"message"`
                                } `json:"error"`
                        }
                        _ = json.Unmarshal(buf, &trailer)
                        if trailer.Error != nil {
                                return nil, fmt.Errorf("envd %s: %s", trailer.Error.Code, trailer.Error.Message)
                        }
                        break
                }
                var resp struct {
                        Event struct {
                                Data *struct {
                                        Stdout string `json:"stdout,omitempty"`
                                        Stderr string `json:"stderr,omitempty"`
                                } `json:"data,omitempty"`
                                End *struct {
                                        Exited   bool   `json:"exited"`
                                        ExitCode int    `json:"exitCode"`
                                        Status   string `json:"status"`
                                        Error    string `json:"error,omitempty"`
                                } `json:"end,omitempty"`
                        } `json:"event"`
                }
                if err := json.Unmarshal(buf, &resp); err != nil {
                        continue
                }
                if resp.Event.Data != nil {
                        if resp.Event.Data.Stdout != "" {
                                stdout.Write(b64dec(resp.Event.Data.Stdout))
                        }
                        if resp.Event.Data.Stderr != "" {
                                stderr.Write(b64dec(resp.Event.Data.Stderr))
                        }
                }
                if resp.Event.End != nil {
                        out.ExitCode = resp.Event.End.ExitCode
                        out.Status = resp.Event.End.Status
                        if out.Status == "" && resp.Event.End.Error != "" {
                                out.Status = resp.Event.End.Error
                        }
                }
        }
        out.Stdout = stdout.String()
        out.Stderr = stderr.String()
        return out, nil
}

// WriteFile uploads `content` to absolute `path` inside the sandbox.
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
        u := s.host() + "/files?path=" + url.QueryEscape(path) + "&user=user"
        req, _ := http.NewRequestWithContext(ctx, http.MethodPost, u, &body)
        req.Header.Set("Content-Type", mw.FormDataContentType())
        cl := &http.Client{Timeout: 120 * time.Second}
        res, err := cl.Do(req)
        if err != nil {
                return err
        }
        defer res.Body.Close()
        if res.StatusCode/100 != 2 {
                b, _ := io.ReadAll(res.Body)
                return fmt.Errorf("write %s: %s: %s", path, res.Status, string(b))
        }
        return nil
}

// ReadFile downloads the bytes at absolute `path` inside the sandbox.
func (s *Sandbox) ReadFile(ctx context.Context, path string) ([]byte, error) {
        u := s.host() + "/files?path=" + url.QueryEscape(path) + "&user=user"
        req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
        cl := &http.Client{Timeout: 60 * time.Second}
        res, err := cl.Do(req)
        if err != nil {
                return nil, err
        }
        defer res.Body.Close()
        if res.StatusCode/100 != 2 {
                b, _ := io.ReadAll(res.Body)
                return nil, fmt.Errorf("read %s: %s: %s", path, res.Status, string(b))
        }
        return io.ReadAll(res.Body)
}

// Connect resumes a paused sandbox by ID and returns a usable handle.
// ref is "sandboxID:clientID" as stored by the agent in DB.
func Connect(ctx context.Context, apiKey, ref string) (*Sandbox, error) {
        if apiKey == "" {
                return nil, fmt.Errorf("E2B_API_KEY is empty")
        }
        parts := strings.SplitN(ref, ":", 2)
        sandboxID := parts[0]
        clientID := ""
        if len(parts) == 2 {
                clientID = parts[1]
        }

        req, _ := http.NewRequestWithContext(ctx, http.MethodPost,
                apiBase+"/sandboxes/"+sandboxID+"/resume", nil)
        req.Header.Set("X-API-Key", apiKey)
        cl := &http.Client{Timeout: 30 * time.Second}
        res, err := cl.Do(req)
        if err != nil {
                return nil, err
        }
        defer res.Body.Close()
        if res.StatusCode/100 != 2 {
                b, _ := io.ReadAll(res.Body)
                return nil, fmt.Errorf("resume sandbox: %s: %s", res.Status, string(b))
        }

        sb := &Sandbox{apiKey: apiKey, ID: sandboxID, ClientID: clientID}
        _ = json.NewDecoder(res.Body).Decode(sb)
        if sb.ID == "" {
                sb.ID = sandboxID
        }
        if sb.ClientID == "" {
                sb.ClientID = clientID
        }

        deadline := time.Now().Add(20 * time.Second)
        for time.Now().Before(deadline) {
                hctx, cancel := context.WithTimeout(ctx, 3*time.Second)
                hreq, _ := http.NewRequestWithContext(hctx, http.MethodGet, sb.host()+"/health", nil)
                r, herr := http.DefaultClient.Do(hreq)
                cancel()
                if herr == nil {
                        r.Body.Close()
                        if r.StatusCode == 204 || r.StatusCode == 200 {
                                return sb, nil
                        }
                }
                time.Sleep(500 * time.Millisecond)
        }
        return sb, nil
}

func makeFrame(payload []byte) []byte {
        buf := make([]byte, 5+len(payload))
        buf[0] = 0
        binary.BigEndian.PutUint32(buf[1:5], uint32(len(payload)))
        copy(buf[5:], payload)
        return buf
}

func b64dec(s string) []byte {
        out, err := base64.StdEncoding.DecodeString(s)
        if err != nil {
                return []byte(s)
        }
        return out
}
