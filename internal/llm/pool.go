package llm

import (
        "bytes"
        "context"
        "encoding/json"
        "fmt"
        "io"
        "log"
        "net/http"
        "os"
        "strconv"
        "sync"
        "time"
)

const (
        deepseekURL = "https://api.deepseek.com/v1/chat/completions"
        // DeepSeek-V4-Pro is the only supported model. The legacy
        // "deepseek-chat" and "deepseek-reasoner" aliases are deprecated and
        // silently route to the cheaper V4-Flash tier, which we do NOT want.
        // Override only if you know you have access to a newer Pro variant.
        defaultDeepseekModel = "deepseek-v4-pro"
)

// deepseekModel is resolved at startup from $DEEPSEEK_MODEL or falls back
// to deepseek-v4-pro. Stored as a package-level var so the rest of the
// pool code (status, requests) sees a single source of truth.
var deepseekModel = func() string {
        if m := os.Getenv("DEEPSEEK_MODEL"); m != "" {
                return m
        }
        return defaultDeepseekModel
}()

// Retry policy: exponential backoff between attempts on transient failures.
// Total wall-clock budget if all retries trigger: 0 + 3 + 6 + 12 = 21s of sleep
// plus up to 4 × httpTimeout of work.
var retryDelays = []time.Duration{
        0,             // attempt 1: immediate
        3 * time.Second,
        6 * time.Second,
        12 * time.Second,
}

const httpTimeout = 90 * time.Second

// Pool is a thin wrapper over the DeepSeek chat completions API.
// Single provider, retries with exponential backoff, cooldown on hard rate limits.
type Pool struct {
        key       string
        http      *http.Client
        onUsage   func(UsageRecord)
        OnAllDown func() // called when we hit a long cooldown (rate limit / auth)

        mu       sync.Mutex
        cooldown time.Time // no calls allowed before this time
        failures int
}

// Config holds the API key. NIM is gone — DeepSeek is the only provider.
type Config struct {
        DeepSeekKey string
}

// New creates a Pool. Panics if no key is configured.
func New(cfg Config, onUsage func(UsageRecord)) *Pool {
        if cfg.DeepSeekKey == "" {
                panic("llm: DEEPSEEK_API_KEY is required")
        }
        if onUsage == nil {
                onUsage = func(UsageRecord) {}
        }
        return &Pool{
                key:     cfg.DeepSeekKey,
                http:    &http.Client{Timeout: httpTimeout},
                onUsage: onUsage,
        }
}

// Chat sends messages to DeepSeek with optional tool calls.
func (p *Pool) Chat(ctx context.Context, messages []Message, tools []Tool) (*Response, error) {
        return p.callWithRetry(ctx, messages, tools, false)
}

// ChatJSON sends messages requesting a JSON-object response.
func (p *Pool) ChatJSON(ctx context.Context, messages []Message) (*Response, error) {
        return p.callWithRetry(ctx, messages, nil, true)
}

// callWithRetry handles cooldown checks + exponential backoff over transient errors.
func (p *Pool) callWithRetry(ctx context.Context, messages []Message, tools []Tool, jsonMode bool) (*Response, error) {
        // Honor any active cooldown first (set by previous 429/auth/etc).
        if wait := p.cooldownRemaining(); wait > 0 {
                log.Printf("[llm] on cooldown, waiting %s", wait.Round(time.Second))
                if p.OnAllDown != nil {
                        p.OnAllDown()
                }
                select {
                case <-time.After(wait):
                case <-ctx.Done():
                        return nil, ctx.Err()
                }
        }

        var lastErr error
        for attempt, delay := range retryDelays {
                if delay > 0 {
                        log.Printf("[llm] retry attempt %d after %s …", attempt+1, delay)
                        select {
                        case <-time.After(delay):
                        case <-ctx.Done():
                                return nil, ctx.Err()
                        }
                }

                resp, headers, status, err := p.callOnce(ctx, messages, tools, jsonMode)
                if err == nil {
                        p.resetFailures()
                        p.onUsage(UsageRecord{
                                Provider:         "deepseek",
                                Model:            deepseekModel,
                                PromptTokens:     resp.Usage.PromptTokens,
                                CompletionTokens: resp.Usage.CompletionTokens,
                        })
                        return resp, nil
                }

                lastErr = err
                log.Printf("[llm] attempt %d/%d failed (http %d): %v", attempt+1, len(retryDelays), status, err)

                // Hard errors (rate-limit, auth, billing) — stop retrying, set cooldown,
                // surface error now. Classification is by HTTP status only, never by
                // error-string substring.
                if isHardStatus(status) {
                        p.applyHardCooldown(status, headers)
                        if p.OnAllDown != nil {
                                p.OnAllDown()
                        }
                        break
                }
                // Transient (timeout, 5xx, network, parse) → continue to next backoff slot.
        }

        return nil, fmt.Errorf("deepseek failed after %d attempts: %w", len(retryDelays), lastErr)
}

// callOnce fires one HTTP request to DeepSeek and returns the HTTP status code
// alongside the parsed response and headers. statusCode is 0 when the failure
// happened before we got an HTTP response (network error, timeout, marshal,
// JSON parse error). Callers MUST classify retry policy on statusCode, not on
// error-string substrings.
func (p *Pool) callOnce(ctx context.Context, messages []Message, tools []Tool, jsonMode bool) (*Response, http.Header, int, error) {
        body := map[string]interface{}{
                "model":       deepseekModel,
                "messages":    messages,
                "max_tokens":  8192,
                "temperature": 0.7,
        }
        if jsonMode {
                body["temperature"] = 0.3
                body["max_tokens"] = 4096
                body["response_format"] = map[string]string{"type": "json_object"}
        }
        if len(tools) > 0 {
                body["tools"] = tools
                body["tool_choice"] = "auto"
        }

        b, err := json.Marshal(body)
        if err != nil {
                return nil, nil, 0, fmt.Errorf("marshal: %w", err)
        }

        req, err := http.NewRequestWithContext(ctx, "POST", deepseekURL, bytes.NewReader(b))
        if err != nil {
                return nil, nil, 0, fmt.Errorf("build request: %w", err)
        }
        req.Header.Set("Content-Type", "application/json")
        req.Header.Set("Authorization", "Bearer "+p.key)

        resp, err := p.http.Do(req)
        if err != nil {
                return nil, nil, 0, fmt.Errorf("http: %w", err)
        }
        defer resp.Body.Close()

        data, err := io.ReadAll(resp.Body)
        if err != nil {
                return nil, resp.Header, resp.StatusCode, fmt.Errorf("read body: %w", err)
        }

        if resp.StatusCode >= 400 {
                return nil, resp.Header, resp.StatusCode, fmt.Errorf("status %d: %s", resp.StatusCode, truncate(string(data), 200))
        }

        var result Response
        if err := json.Unmarshal(data, &result); err != nil {
                return nil, resp.Header, resp.StatusCode, fmt.Errorf("parse response: %w", err)
        }
        if result.Error != nil {
                return nil, resp.Header, resp.StatusCode, fmt.Errorf("api error: %s", result.Error.Message)
        }
        if len(result.Choices) == 0 {
                return nil, resp.Header, resp.StatusCode, fmt.Errorf("empty choices in response")
        }
        return &result, resp.Header, resp.StatusCode, nil
}

// isHardStatus identifies HTTP status codes where in-call retries are pointless.
// These trigger a longer cooldown set on the pool. 402 = payment required (DeepSeek
// uses this for insufficient balance); 429 = rate limit; 401/403 = auth.
func isHardStatus(status int) bool {
        return status == 401 || status == 403 || status == 402 || status == 429
}

// applyHardCooldown sets the pool cooldown based on the HTTP status code.
// Uses Retry-After header when present (capped). Concurrent failures keep
// the LONGER cooldown — never downgrade an active long cooldown.
func (p *Pool) applyHardCooldown(status int, headers http.Header) {
        var cd time.Duration
        switch status {
        case 401, 403:
                cd = 24 * time.Hour
                log.Printf("[llm] auth error (http %d) → 24h cooldown (check DEEPSEEK_API_KEY)", status)
        case 402:
                cd = 1 * time.Hour
                log.Printf("[llm] payment required (http 402) → 1h cooldown (top up DeepSeek balance)")
        case 429:
                cd = retryAfterCooldown(headers, 1*time.Hour)
                log.Printf("[llm] rate limited (http 429) → cooldown %s", cd.Round(time.Second))
        default:
                cd = 30 * time.Second
        }

        until := time.Now().Add(cd)
        p.mu.Lock()
        if until.After(p.cooldown) {
                p.cooldown = until
        }
        p.failures++
        p.mu.Unlock()
}

func (p *Pool) cooldownRemaining() time.Duration {
        p.mu.Lock()
        defer p.mu.Unlock()
        return time.Until(p.cooldown)
}

func (p *Pool) resetFailures() {
        p.mu.Lock()
        p.failures = 0
        p.mu.Unlock()
}

// retryAfterCooldown reads the Retry-After header (seconds) and returns
// that duration capped at maxCap. Falls back to maxCap if header absent.
func retryAfterCooldown(headers http.Header, maxCap time.Duration) time.Duration {
        if headers == nil {
                return maxCap
        }
        val := headers.Get("Retry-After")
        if val == "" {
                return maxCap
        }
        secs, err := strconv.Atoi(val)
        if err != nil {
                return maxCap
        }
        d := time.Duration(secs) * time.Second
        if d > maxCap {
                return maxCap
        }
        if d < 1*time.Second {
                return 1 * time.Second
        }
        return d
}

// ActiveModel returns the model name currently in use.
func (p *Pool) ActiveModel() string { return deepseekModel }

// StatusReport returns a one-line human-readable status of the provider.
func (p *Pool) StatusReport() string {
        p.mu.Lock()
        cd := p.cooldown
        failures := p.failures
        p.mu.Unlock()

        now := time.Now()
        if now.After(cd) {
                return fmt.Sprintf("LLM Provider:\n  ✅ deepseek (%s) [paid]\n", deepseekModel)
        }
        remaining := time.Until(cd).Round(time.Second)
        return fmt.Sprintf("LLM Provider:\n  ❌ deepseek (%s) [paid] — cooldown %s, failures: %d\n",
                deepseekModel, remaining, failures)
}

func truncate(s string, n int) string {
        if len(s) <= n {
                return s
        }
        return s[:n] + "..."
}
