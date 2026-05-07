package llm

import (
        "bytes"
        "context"
        "encoding/json"
        "fmt"
        "io"
        "log"
        "net/http"
        "strconv"
        "strings"
        "sync"
        "time"
)

const (
        deepseekURL = "https://api.deepseek.com/v1/chat/completions"
        nimURL      = "https://integrate.api.nvidia.com/v1/chat/completions"
)

// provider is a single LLM endpoint + key combo.
type provider struct {
        name   string
        url    string
        model  string
        key    string
        isPaid bool // true = DeepSeek (paid), false = NIM (free)

        mu       sync.Mutex
        cooldown time.Time
        failures int
}

func (p *provider) isAvailable() bool {
        p.mu.Lock()
        defer p.mu.Unlock()
        return time.Now().After(p.cooldown)
}

// setCooldown sets a cooldown duration from now.
func (p *provider) setCooldown(d time.Duration) {
        p.mu.Lock()
        defer p.mu.Unlock()
        p.cooldown = time.Now().Add(d)
        p.failures++
}

// setCooldownUntil sets cooldown to an absolute time.
func (p *provider) setCooldownUntil(t time.Time) {
        p.mu.Lock()
        defer p.mu.Unlock()
        p.cooldown = t
        p.failures++
}

func (p *provider) resetFailures() {
        p.mu.Lock()
        defer p.mu.Unlock()
        p.failures = 0
}

// Pool manages multiple LLM providers with automatic failover.
// Priority: NIM (free) → DeepSeek (paid, only when NIM daily quota exhausted).
type Pool struct {
        providers []*provider
        http      *http.Client
        onUsage   func(UsageRecord)
        OnAllDown func() // called when every provider is on cooldown
}

// Config holds API keys for the pool.
type Config struct {
        DeepSeekKey string
        NIMKey1     string
        NIMKey2     string
}

// New creates a Pool from the given config.
// Provider order: NIM1 → NIM2 → DeepSeek Pro → DeepSeek Flash.
// NIM is always tried first since it's free.
func New(cfg Config, onUsage func(UsageRecord)) *Pool {
        var providers []*provider

        // ── Free tier first ────────────────────────────────────────────────
        if cfg.NIMKey1 != "" {
                providers = append(providers,
                        &provider{name: "nim1-deepseek", url: nimURL,
                                model: "deepseek-ai/deepseek-r1", key: cfg.NIMKey1, isPaid: false},
                        &provider{name: "nim1-glm", url: nimURL,
                                model: "z-ai/glm-5.1", key: cfg.NIMKey1, isPaid: false},
                )
        }
        if cfg.NIMKey2 != "" {
                providers = append(providers,
                        &provider{name: "nim2-deepseek", url: nimURL,
                                model: "deepseek-ai/deepseek-r1", key: cfg.NIMKey2, isPaid: false},
                        &provider{name: "nim2-glm", url: nimURL,
                                model: "z-ai/glm-5.1", key: cfg.NIMKey2, isPaid: false},
                )
        }

        // ── Paid fallback last ─────────────────────────────────────────────
        if cfg.DeepSeekKey != "" {
                providers = append(providers,
                        &provider{name: "deepseek-pro", url: deepseekURL,
                                model: "deepseek-chat", key: cfg.DeepSeekKey, isPaid: true},
                        &provider{name: "deepseek-flash", url: deepseekURL,
                                model: "deepseek-chat", key: cfg.DeepSeekKey, isPaid: true},
                )
        }

        if len(providers) == 0 {
                panic("llm: no providers configured — set DEEPSEEK_API_KEY or NIM_API_KEY_*")
        }

        if onUsage == nil {
                onUsage = func(UsageRecord) {}
        }

        return &Pool{
                providers: providers,
                http:      &http.Client{Timeout: 120 * time.Second},
                onUsage:   onUsage,
        }
}

// Chat sends messages to the best available provider, with tool support.
func (p *Pool) Chat(ctx context.Context, messages []Message, tools []Tool) (*Response, error) {
        return p.call(ctx, messages, tools, false)
}

// ChatJSON sends messages requesting a JSON-object response.
func (p *Pool) ChatJSON(ctx context.Context, messages []Message) (*Response, error) {
        return p.call(ctx, messages, nil, true)
}

func (p *Pool) call(ctx context.Context, messages []Message, tools []Tool, jsonMode bool) (*Response, error) {
        available := p.availableProviders()
        if len(available) == 0 {
                if p.OnAllDown != nil {
                        p.OnAllDown()
                }
                soonest := p.soonestAvailable()
                wait := time.Until(soonest)
                if wait > 0 {
                        log.Printf("[llm] all providers on cooldown, waiting %s", wait.Round(time.Second))
                        select {
                        case <-time.After(wait):
                        case <-ctx.Done():
                                return nil, ctx.Err()
                        }
                }
                available = p.availableProviders()
        }

        var lastErr error
        for _, pr := range available {
                resp, headers, err := p.callProvider(ctx, pr, messages, tools, jsonMode)
                if err == nil {
                        pr.resetFailures()
                        p.onUsage(UsageRecord{
                                Provider:         pr.name,
                                Model:            pr.model,
                                PromptTokens:     resp.Usage.PromptTokens,
                                CompletionTokens: resp.Usage.CompletionTokens,
                        })
                        return resp, nil
                }

                lastErr = err
                errStr := strings.ToLower(err.Error())
                log.Printf("[llm] %s failed: %v", pr.name, err)

                p.applyCooldown(pr, errStr, headers)
        }

        return nil, fmt.Errorf("all LLM providers failed: %w", lastErr)
}

// applyCooldown sets the right cooldown based on provider type and error.
//
// NIM (free):
//   - 429 → use Retry-After header if present, else 30s max (just RPM reset)
//   - any other error → 24h (daily quota or hard error, skip for the day)
//
// DeepSeek (paid):
//   - 429 → use Retry-After header if present, else 1h
//   - 401/403 → 24h (bad key)
//   - 502/503 → 5m (service blip)
//   - timeout → 2m
//   - other → 30s
func (p *Pool) applyCooldown(pr *provider, errStr string, headers http.Header) {
        is429 := strings.Contains(errStr, "429") || strings.Contains(errStr, "rate")
        isAuth := strings.Contains(errStr, "401") || strings.Contains(errStr, "403")
        isDown := strings.Contains(errStr, "503") || strings.Contains(errStr, "502")
        isTimeout := strings.Contains(errStr, "timeout") || strings.Contains(errStr, "deadline")
        isQuota := strings.Contains(errStr, "quota") || strings.Contains(errStr, "insufficient") ||
                strings.Contains(errStr, "billing") || strings.Contains(errStr, "limit exceeded")

        if !pr.isPaid {
                // ── NIM (free) ─────────────────────────────────────────────────
                if is429 {
                        // RPM limit — recover in seconds, not hours
                        cd := retryAfterCooldown(headers, 30*time.Second)
                        pr.setCooldownUntil(time.Now().Add(cd))
                        log.Printf("[llm] %s RPM 429 → cooldown %s", pr.name, cd.Round(time.Second))
                } else {
                        // Any other error on NIM = daily quota hit or hard error
                        // Drop this provider for the rest of the day
                        until := tomorrowMidnight()
                        pr.setCooldownUntil(until)
                        log.Printf("[llm] %s hard error → dropped until %s (will try DeepSeek)",
                                pr.name, until.Format("15:04"))
                }
                return
        }

        // ── DeepSeek (paid) ────────────────────────────────────────────────
        switch {
        case is429 || isQuota:
                cd := retryAfterCooldown(headers, 1*time.Hour)
                pr.setCooldownUntil(time.Now().Add(cd))
                log.Printf("[llm] %s rate limited → cooldown %s", pr.name, cd.Round(time.Second))
        case isAuth:
                pr.setCooldown(24 * time.Hour)
                log.Printf("[llm] %s auth error → 24h cooldown", pr.name)
        case isDown:
                pr.setCooldown(5 * time.Minute)
                log.Printf("[llm] %s service down → 5m cooldown", pr.name)
        case isTimeout:
                pr.setCooldown(2 * time.Minute)
                log.Printf("[llm] %s timeout → 2m cooldown", pr.name)
        default:
                pr.setCooldown(30 * time.Second)
                log.Printf("[llm] %s unknown error → 30s cooldown", pr.name)
        }
}

// retryAfterCooldown reads the Retry-After header (seconds) and returns
// that duration capped at maxCap. Falls back to maxCap if header absent.
func retryAfterCooldown(headers http.Header, maxCap time.Duration) time.Duration {
        if headers == nil {
                return maxCap
        }
        val := headers.Get("Retry-After")
        if val == "" {
                val = headers.Get("X-RateLimit-Reset-After") // NIM sometimes uses this
        }
        if val != "" {
                if secs, err := strconv.Atoi(val); err == nil {
                        d := time.Duration(secs) * time.Second
                        if d > maxCap {
                                return maxCap
                        }
                        if d < 1*time.Second {
                                return 1 * time.Second
                        }
                        return d
                }
        }
        return maxCap
}

// tomorrowMidnight returns midnight of the next day in local time.
func tomorrowMidnight() time.Time {
        now := time.Now()
        tomorrow := now.AddDate(0, 0, 1)
        return time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 0, 0, 0, 0, now.Location())
}

// callProvider makes one HTTP call and returns response + headers on error.
func (p *Pool) callProvider(ctx context.Context, pr *provider, messages []Message, tools []Tool, jsonMode bool) (*Response, http.Header, error) {
        body := map[string]interface{}{
                "model":       pr.model,
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
                return nil, nil, fmt.Errorf("marshal: %w", err)
        }

        req, err := http.NewRequestWithContext(ctx, "POST", pr.url, bytes.NewReader(b))
        if err != nil {
                return nil, nil, fmt.Errorf("build request: %w", err)
        }
        req.Header.Set("Content-Type", "application/json")
        req.Header.Set("Authorization", "Bearer "+pr.key)

        resp, err := p.http.Do(req)
        if err != nil {
                return nil, nil, fmt.Errorf("http: %w", err)
        }
        defer resp.Body.Close()

        data, err := io.ReadAll(resp.Body)
        if err != nil {
                return nil, resp.Header, fmt.Errorf("read body: %w", err)
        }

        if resp.StatusCode >= 400 {
                return nil, resp.Header, fmt.Errorf("status %d: %s", resp.StatusCode, truncate(string(data), 200))
        }

        var result Response
        if err := json.Unmarshal(data, &result); err != nil {
                return nil, resp.Header, fmt.Errorf("parse response: %w", err)
        }
        if result.Error != nil {
                return nil, resp.Header, fmt.Errorf("api error: %s", result.Error.Message)
        }
        if len(result.Choices) == 0 {
                return nil, resp.Header, fmt.Errorf("empty choices in response")
        }

        return &result, resp.Header, nil
}

// availableProviders returns providers not on cooldown, in priority order.
func (p *Pool) availableProviders() []*provider {
        var out []*provider
        for _, pr := range p.providers {
                if pr.isAvailable() {
                        out = append(out, pr)
                }
        }
        return out
}

// soonestAvailable returns the earliest cooldown expiry across all providers.
func (p *Pool) soonestAvailable() time.Time {
        soonest := time.Now().Add(24 * time.Hour)
        for _, pr := range p.providers {
                pr.mu.Lock()
                cd := pr.cooldown
                pr.mu.Unlock()
                if cd.Before(soonest) {
                        soonest = cd
                }
        }
        return soonest
}

// StatusReport returns a human-readable status of all providers.
func (p *Pool) StatusReport() string {
        var sb strings.Builder
        sb.WriteString("LLM Providers:\n")
        now := time.Now()
        for _, pr := range p.providers {
                pr.mu.Lock()
                cd := pr.cooldown
                failures := pr.failures
                pr.mu.Unlock()

                tier := "free"
                if pr.isPaid {
                        tier = "paid"
                }

                if now.After(cd) {
                        sb.WriteString(fmt.Sprintf("  ✅ %s (%s) [%s]\n", pr.name, pr.model, tier))
                } else {
                        remaining := time.Until(cd).Round(time.Second)
                        sb.WriteString(fmt.Sprintf("  ❌ %s (%s) [%s] — cooldown %s, failures: %d\n",
                                pr.name, pr.model, tier, remaining, failures))
                }
        }
        return sb.String()
}

func truncate(s string, n int) string {
        if len(s) <= n {
                return s
        }
        return s[:n] + "..."
}
