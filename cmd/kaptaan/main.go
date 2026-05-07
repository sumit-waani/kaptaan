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
        // Escape hatch: when LLM_DEEPSEEK_ONLY=1 we drop the (often slow / 429-prone)
        // free NIM tier entirely and route every call straight to paid DeepSeek.
        // Lets us isolate "is the agent stuck?" vs "is NIM stuck?" in one toggle.
        if os.Getenv("LLM_DEEPSEEK_ONLY") == "1" {
                llmCfg.NIMKey1 = ""
                llmCfg.NIMKey2 = ""
                log.Println("⚙️  LLM_DEEPSEEK_ONLY=1 — NIM disabled, routing only to DeepSeek paid")
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

        // ── Manager executor (no live workspace; merges PRs via GitHub REST) ─
        githubRepo := os.Getenv("GITHUB_REPO")
        githubToken := os.Getenv("GITHUB_TOKEN")
        managerExec := tools.NewNoopExecutor(githubRepo, githubToken)
        // Resolve repo+token at every Manager merge_pr — uses the active project's
        // values when set, else falls back to env (the static fields).
        managerExec.Resolver = func(ctx context.Context) (string, string, error) {
                proj, err := database.GetActiveProject(ctx)
                if err != nil || proj == nil {
                        return "", "", err
                }
                return proj.RepoURL, proj.GithubToken, nil
        }

        // ── Builder config (per-job E2B sandbox) ────────────────────────────
        builderCfg := agent.BuilderConfig{
                E2BAPIKey:   os.Getenv("E2B_API_KEY"),
                GithubRepo:  githubRepo,
                GithubToken: githubToken,
        }
        if builderCfg.E2BAPIKey == "" {
                log.Println("⚠️  E2B_API_KEY not set — Builder jobs will fail until it is configured.")
        }
        if githubToken == "" || githubRepo == "" {
                log.Println("⚠️  GITHUB_REPO/GITHUB_TOKEN not fully set — Builder will not be able to clone or open PRs.")
        }

        // ── Wire agent ──────────────────────────────────────────────────────
        a := agent.New(
                database,
                pool,
                managerExec,
                builderCfg,
                webServer.Send,
                webServer.Ask,
                webServer.SendPRReview,
                webServer.SendBuilderStatus,
                func() { webServer.BroadcastStatus(context.Background()) },
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
        go a.ResumePendingReviews(ctx)

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

