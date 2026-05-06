package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
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
	name     string
	url      string
	model    string
	key      string
	mu       sync.Mutex
	cooldown time.Time
	failures int
}

func (p *provider) isAvailable() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return time.Now().After(p.cooldown)
}

func (p *provider) setCooldown(d time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cooldown = time.Now().Add(d)
	p.failures++
}

func (p *provider) resetFailures() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.failures = 0
}

// Pool manages multiple LLM providers with automatic failover.
type Pool struct {
	providers []*provider
	http      *http.Client
	onUsage   func(UsageRecord)
	OnAllDown func() // called once when every provider is on cooldown
}

// Config holds API keys for the pool.
type Config struct {
	DeepSeekKey string
	NIMKey1     string
	NIMKey2     string
}

// New creates a Pool from the given config.
func New(cfg Config, onUsage func(UsageRecord)) *Pool {
	var providers []*provider

	if cfg.DeepSeekKey != "" {
		providers = append(providers,
			&provider{name: "deepseek-pro", url: deepseekURL,
				model: "deepseek-v4-pro", key: cfg.DeepSeekKey},
			&provider{name: "deepseek-flash", url: deepseekURL,
				model: "deepseek-v4-flash", key: cfg.DeepSeekKey},
		)
	}
	if cfg.NIMKey1 != "" {
		providers = append(providers,
			&provider{name: "nim1-deepseek", url: nimURL,
				model: "deepseek-ai/deepseek-v4-pro", key: cfg.NIMKey1},
			&provider{name: "nim1-glm", url: nimURL,
				model: "z-ai/glm-5.1", key: cfg.NIMKey1},
		)
	}
	if cfg.NIMKey2 != "" {
		providers = append(providers,
			&provider{name: "nim2-deepseek", url: nimURL,
				model: "deepseek-ai/deepseek-v4-pro", key: cfg.NIMKey2},
			&provider{name: "nim2-glm", url: nimURL,
				model: "z-ai/glm-5.1", key: cfg.NIMKey2},
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
// It retries across providers on failure.
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
		resp, err := p.callProvider(ctx, pr, messages, tools, jsonMode)
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

		switch {
		case strings.Contains(errStr, "429") || strings.Contains(errStr, "rate"):
			pr.setCooldown(1 * time.Hour)
			log.Printf("[llm] %s rate limited → 1h cooldown", pr.name)
		case strings.Contains(errStr, "401") || strings.Contains(errStr, "403"):
			pr.setCooldown(24 * time.Hour)
			log.Printf("[llm] %s auth error → 24h cooldown", pr.name)
		case strings.Contains(errStr, "503") || strings.Contains(errStr, "502"):
			pr.setCooldown(5 * time.Minute)
			log.Printf("[llm] %s service unavailable → 5m cooldown", pr.name)
		case strings.Contains(errStr, "timeout") || strings.Contains(errStr, "deadline"):
			pr.setCooldown(2 * time.Minute)
			log.Printf("[llm] %s timeout → 2m cooldown", pr.name)
		default:
			pr.setCooldown(30 * time.Second)
		}
	}

	return nil, fmt.Errorf("all LLM providers failed: %w", lastErr)
}

func (p *Pool) callProvider(ctx context.Context, pr *provider, messages []Message, tools []Tool, jsonMode bool) (*Response, error) {
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
		return nil, fmt.Errorf("marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", pr.url, bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+pr.key)

	resp, err := p.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, truncate(string(data), 200))
	}

	var result Response
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	if result.Error != nil {
		return nil, fmt.Errorf("api error: %s", result.Error.Message)
	}
	if len(result.Choices) == 0 {
		return nil, fmt.Errorf("empty choices in response")
	}

	return &result, nil
}

// availableProviders returns providers not on cooldown.
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

		if now.After(cd) {
			sb.WriteString(fmt.Sprintf("  ✅ %s (%s)\n", pr.name, pr.model))
		} else {
			remaining := time.Until(cd).Round(time.Second)
			sb.WriteString(fmt.Sprintf("  ❌ %s (%s) — cooldown %s, failures: %d\n",
				pr.name, pr.model, remaining, failures))
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
