package llm

import (
        "bufio"
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
        // DeepSeek-V4-Pro is the only supported model. The legacy
        // "deepseek-chat" and "deepseek-reasoner" aliases are deprecated and
        // silently route to the cheaper V4-Flash tier, which we do NOT want.
        // Override only if you know you have access to a newer Pro variant.
        defaultDeepseekModel = "deepseek-v4-pro"
)


// Retry policy: exponential backoff between attempts on transient failures.
// Total wall-clock budget if all retries trigger: 0 + 3 + 6 + 12 = 21s of sleep
// plus up to 4 × httpTimeout of work.
var retryDelays = []time.Duration{
        0,              // attempt 1: immediate
        3 * time.Second,
        6 * time.Second,
        12 * time.Second,
}

const httpTimeout = 90 * time.Second

// Pool is a thin wrapper over the DeepSeek chat completions API.
// Single provider, retries with exponential backoff, cooldown on hard rate limits.
type Pool struct {
        keyFn     func() string
        modelFn   func() string
        http      *http.Client
        onUsage   func(UsageRecord)
        OnAllDown func() // called when we hit a long cooldown (rate limit / auth)

        mu       sync.Mutex
        cooldown time.Time // no calls allowed before this time
        failures int
}

// Config holds functions to dynamically resolve the API key and model.
// Keys are read on every request so settings changes take effect immediately.
type Config struct {
        // KeyFn returns the current DeepSeek API key.
        KeyFn func() string
        // ModelFn returns the model name to use (optional; defaults to deepseek-v4-pro).
        ModelFn func() string
}

// New creates a Pool. An empty key does not panic; LLM calls will return an
// error at call time if the key is still empty, allowing the UI to be usable
// without credentials while settings are being configured.
func New(cfg Config, onUsage func(UsageRecord)) *Pool {
        keyFn := cfg.KeyFn
        if keyFn == nil {
                keyFn = func() string { return "" }
        }
        modelFn := cfg.ModelFn
        if modelFn == nil {
                modelFn = func() string { return defaultDeepseekModel }
        }
        if onUsage == nil {
                onUsage = func(UsageRecord) {}
        }
        return &Pool{
                keyFn:   keyFn,
                modelFn: modelFn,
                http:    &http.Client{Timeout: httpTimeout},
                onUsage: onUsage,
        }
}

// Chat sends messages to DeepSeek with optional tool calls (non-streaming).
func (p *Pool) Chat(ctx context.Context, messages []Message, tools []Tool) (*Response, error) {
        return p.callWithRetry(ctx, messages, tools, false)
}

// ChatStream sends messages to DeepSeek and streams content tokens to onToken
// as they arrive. It returns the fully assembled Response once complete.
// onToken is called only for content tokens (not tool-call argument fragments).
func (p *Pool) ChatStream(ctx context.Context, messages []Message, tools []Tool, onToken func(string)) (*Response, error) {
        return p.streamWithRetry(ctx, messages, tools, onToken)
}

// ── Non-streaming ─────────────────────────────────────────────────────────────

// callWithRetry handles cooldown checks + exponential backoff over transient errors.
func (p *Pool) callWithRetry(ctx context.Context, messages []Message, tools []Tool, jsonMode bool) (*Response, error) {
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
                                Model:            p.modelFn(),
                                PromptTokens:     resp.Usage.PromptTokens,
                                CompletionTokens: resp.Usage.CompletionTokens,
                        })
                        return resp, nil
                }

                lastErr = err
                log.Printf("[llm] attempt %d/%d failed (http %d): %v", attempt+1, len(retryDelays), status, err)

                if isHardStatus(status) {
                        p.applyHardCooldown(status, headers)
                        if p.OnAllDown != nil {
                                p.OnAllDown()
                        }
                        break
                }
        }

        return nil, fmt.Errorf("deepseek failed after %d attempts: %w", len(retryDelays), lastErr)
}

// callOnce fires one HTTP request to DeepSeek and returns the HTTP status code
// alongside the parsed response and headers.
func (p *Pool) callOnce(ctx context.Context, messages []Message, tools []Tool, jsonMode bool) (*Response, http.Header, int, error) {
        key := p.keyFn()
        if key == "" {
                return nil, nil, 0, fmt.Errorf("deepseek_api_key is not configured — set it in Settings → Configuration")
        }
        body := map[string]interface{}{
                "model":       p.modelFn(),
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
        req.Header.Set("Authorization", "Bearer "+key)

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

// ── Streaming ─────────────────────────────────────────────────────────────────

// streamWithRetry wraps streamOnce with the same cooldown + backoff logic.
func (p *Pool) streamWithRetry(ctx context.Context, messages []Message, tools []Tool, onToken func(string)) (*Response, error) {
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
                        log.Printf("[llm] stream retry attempt %d after %s …", attempt+1, delay)
                        select {
                        case <-time.After(delay):
                        case <-ctx.Done():
                                return nil, ctx.Err()
                        }
                }

                resp, headers, status, err := p.streamOnce(ctx, messages, tools, onToken)
                if err == nil {
                        p.resetFailures()
                        p.onUsage(UsageRecord{
                                Provider:         "deepseek",
                                Model:            p.modelFn(),
                                PromptTokens:     resp.Usage.PromptTokens,
                                CompletionTokens: resp.Usage.CompletionTokens,
                        })
                        return resp, nil
                }

                lastErr = err
                log.Printf("[llm] stream attempt %d/%d failed (http %d): %v", attempt+1, len(retryDelays), status, err)

                if isHardStatus(status) {
                        p.applyHardCooldown(status, headers)
                        if p.OnAllDown != nil {
                                p.OnAllDown()
                        }
                        break
                }
        }

        return nil, fmt.Errorf("deepseek stream failed after %d attempts: %w", len(retryDelays), lastErr)
}

// streamOnce makes one streaming HTTP request, parses the SSE chunks, calls
// onToken for each content token, and returns the assembled Response.
func (p *Pool) streamOnce(ctx context.Context, messages []Message, tools []Tool, onToken func(string)) (*Response, http.Header, int, error) {
        key := p.keyFn()
        if key == "" {
                return nil, nil, 0, fmt.Errorf("deepseek_api_key is not configured — set it in Settings → Configuration")
        }
        body := map[string]interface{}{
                "model":       p.modelFn(),
                "messages":    messages,
                "max_tokens":  8192,
                "temperature": 0.7,
                "stream":      true,
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
        req.Header.Set("Authorization", "Bearer "+key)
        req.Header.Set("Accept", "text/event-stream")

        // Use a client without a hard read-deadline so the stream can flow freely.
        streamClient := &http.Client{}
        resp, err := streamClient.Do(req)
        if err != nil {
                return nil, nil, 0, fmt.Errorf("http: %w", err)
        }
        defer resp.Body.Close()

        if resp.StatusCode >= 400 {
                data, _ := io.ReadAll(resp.Body)
                return nil, resp.Header, resp.StatusCode, fmt.Errorf("status %d: %s", resp.StatusCode, truncate(string(data), 200))
        }

        // Accumulation state for assembling the full response from deltas.
        var (
                role             = "assistant"
                content          strings.Builder
                reasoningContent strings.Builder
                toolCallMap      = map[int]*ToolCall{}
                toolArgMap       = map[int]*strings.Builder{}
                finishReason     string
                usageTokens      StreamUsage
        )

        scanner := bufio.NewScanner(resp.Body)
        scanner.Buffer(make([]byte, 1024*64), 1024*64)

        for scanner.Scan() {
                line := scanner.Text()
                if !strings.HasPrefix(line, "data: ") {
                        continue
                }
                data := strings.TrimPrefix(line, "data: ")
                if data == "[DONE]" {
                        break
                }

                var chunk StreamChunk
                if err := json.Unmarshal([]byte(data), &chunk); err != nil {
                        continue
                }
                if chunk.Error != nil {
                        return nil, resp.Header, resp.StatusCode, fmt.Errorf("api error: %s", chunk.Error.Message)
                }
                if chunk.Usage != nil {
                        usageTokens = *chunk.Usage
                }
                if len(chunk.Choices) == 0 {
                        continue
                }

                delta := chunk.Choices[0].Delta
                if chunk.Choices[0].FinishReason != "" {
                        finishReason = chunk.Choices[0].FinishReason
                }
                if delta.Role != "" {
                        role = delta.Role
                }
                if delta.Content != "" {
                        content.WriteString(delta.Content)
                        if onToken != nil {
                                onToken(delta.Content)
                        }
                }
                if delta.ReasoningContent != "" {
                        reasoningContent.WriteString(delta.ReasoningContent)
                }
                for _, tc := range delta.ToolCalls {
                        if _, exists := toolCallMap[tc.Index]; !exists {
                                toolCallMap[tc.Index] = &ToolCall{
                                        ID:   tc.ID,
                                        Type: tc.Type,
                                }
                                toolCallMap[tc.Index].Function.Name = tc.Function.Name
                                toolArgMap[tc.Index] = &strings.Builder{}
                        }
                        if tc.ID != "" {
                                toolCallMap[tc.Index].ID = tc.ID
                        }
                        if tc.Type != "" {
                                toolCallMap[tc.Index].Type = tc.Type
                        }
                        if tc.Function.Name != "" {
                                toolCallMap[tc.Index].Function.Name = tc.Function.Name
                        }
                        toolArgMap[tc.Index].WriteString(tc.Function.Arguments)
                }
        }

        if err := scanner.Err(); err != nil {
                return nil, resp.Header, resp.StatusCode, fmt.Errorf("stream read: %w", err)
        }

        // Assemble tool calls in index order.
        toolCalls := make([]ToolCall, 0, len(toolCallMap))
        for i := 0; i < len(toolCallMap); i++ {
                tc := *toolCallMap[i]
                tc.Function.Arguments = toolArgMap[i].String()
                toolCalls = append(toolCalls, tc)
        }

        result := &Response{
                Choices: []Choice{{
                        Message: Message{
                                Role:             role,
                                Content:          content.String(),
                                ReasoningContent: reasoningContent.String(),
                                ToolCalls:        toolCalls,
                        },
                        FinishReason: finishReason,
                }},
        }
        result.Usage.PromptTokens = usageTokens.PromptTokens
        result.Usage.CompletionTokens = usageTokens.CompletionTokens

        return result, resp.Header, resp.StatusCode, nil
}

// ── Cooldown helpers ──────────────────────────────────────────────────────────

func isHardStatus(status int) bool {
        return status == 401 || status == 403 || status == 402 || status == 429
}

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
func (p *Pool) ActiveModel() string { return p.modelFn() }

func truncate(s string, n int) string {
        if len(s) <= n {
                return s
        }
        return s[:n] + "..."
}
