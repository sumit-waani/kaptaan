package main

import (
        "context"
        "log"
        "os"
        "os/signal"
        "syscall"

        "github.com/joho/godotenv"

        "github.com/cto-agent/cto-agent/internal/agent"
        "github.com/cto-agent/cto-agent/internal/db"
        "github.com/cto-agent/cto-agent/internal/llm"
        "github.com/cto-agent/cto-agent/internal/tools"
        "github.com/cto-agent/cto-agent/internal/web"
)

func main() {
        // Load .env (ignore error if not present — rely on real env in prod)
        _ = godotenv.Load()

        ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
        defer stop()

        // ── Database ────────────────────────────────────────────────────────
        dsn := mustEnv("DATABASE_URL")
        database, err := db.New(ctx, dsn)
        if err != nil {
                log.Fatalf("db: %v", err)
        }
        defer database.Close()
        log.Println("✅ database connected")

        // ── Web server (start immediately so :5000 is reachable) ────────────
        webServer := web.New(database)

        // ── LLM keys check ──────────────────────────────────────────────────
        // Check before calling llm.New() — it panics when no keys are present.
        llmCfg := llm.Config{
                DeepSeekKey: os.Getenv("DEEPSEEK_API_KEY"),
                NIMKey1:     os.Getenv("NIM_API_KEY_1"),
                NIMKey2:     os.Getenv("NIM_API_KEY_2"),
        }
        if llmCfg.DeepSeekKey == "" && llmCfg.NIMKey1 == "" && llmCfg.NIMKey2 == "" {
                log.Println("⚠️  No LLM API keys found — running in UI-only mode. Set DEEPSEEK_API_KEY or NIM_API_KEY_* to enable the agent.")
                webServer.SetMOTD("⚠️ **Kaptaan needs an LLM API key to work.**\n\n" +
                        "Set at least one of the following environment variables and restart:\n" +
                        "- `DEEPSEEK_API_KEY`\n" +
                        "- `NIM_API_KEY_1`\n" +
                        "- `NIM_API_KEY_2`")
                go webServer.Start(ctx)
                <-ctx.Done()
                log.Println("👋 shutting down")
                return
        }

        // ── LLM pool ────────────────────────────────────────────────────────
        pool := llm.New(llmCfg, func(u llm.UsageRecord) {
                _ = database.RecordUsage(ctx, u.Provider, u.Model, u.PromptTokens, u.CompletionTokens)
        })
        log.Println("✅ LLM pool ready")

        // ── Tool executor ───────────────────────────────────────────────────
        executor := &tools.Executor{
                WorkspaceDir: workspaceDir(),
                GithubRepo:   os.Getenv("GITHUB_REPO"),
                GithubToken:  os.Getenv("GITHUB_TOKEN"),
        }

        // ── Wire agent ──────────────────────────────────────────────────────
        a := agent.New(
                database,
                pool,
                executor,
                webServer.Send,
                webServer.Ask,
                webServer.SendPRReview,
                webServer.SendBuilderStatus,
        )
        pool.OnAllDown = func() {
                webServer.Send("⚠️ All LLM providers are on cooldown. Pausing agent until a provider recovers.")
                a.Pause(context.Background())
        }
        webServer.SetAgent(a)

        // Stale pending asks from previous sessions — clear so they don't
        // resurrect as ghost prompts in the UI.
        if err := database.KVSet(ctx, "pending_ask", ""); err != nil {
                log.Printf("[main] warn: could not clear stale pending_ask: %v", err)
        }

        // ── Start ────────────────────────────────────────────────────────────
        log.Println("🚀 kaptaan starting on :5000 ...")

        go webServer.Start(ctx)
        go a.RunBuilderLoop(ctx)

        <-ctx.Done()
        log.Println("👋 shutting down gracefully")
}

// mustEnv returns the value of an env var or fatally exits.
func mustEnv(key string) string {
        v := os.Getenv(key)
        if v == "" {
                log.Fatalf("required env var %s is not set", key)
        }
        return v
}

// workspaceDir returns the local path where the repo will be cloned.
func workspaceDir() string {
        if d := os.Getenv("WORKSPACE_DIR"); d != "" {
                return d
        }
        return "/tmp/kaptaan-workspace"
}
