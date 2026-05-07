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

        // ── LLM key check ───────────────────────────────────────────────────
        // DeepSeek is the only provider. llm.New() panics without a key, so
        // gate it here and run UI-only when missing.
        llmCfg := llm.Config{
                DeepSeekKey: os.Getenv("DEEPSEEK_API_KEY"),
        }
        if llmCfg.DeepSeekKey == "" {
                log.Println("⚠️  DEEPSEEK_API_KEY not set — running in UI-only mode.")
                webServer.SetMOTD("⚠️ **Kaptaan needs the DeepSeek API key to work.**\n\n" +
                        "Set `DEEPSEEK_API_KEY` and restart.")
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
        // GitHub repo + token live in the database now (per-project). The
        // executor resolves them on every merge_pr call from the active
        // project. No env-var fallback — fully UI-managed.
        managerExec := tools.NewNoopExecutor("", "")
        managerExec.Resolver = func(ctx context.Context) (string, string, error) {
                proj, err := database.GetActiveProject(ctx)
                if err != nil || proj == nil {
                        return "", "", err
                }
                return proj.RepoURL, proj.GithubToken, nil
        }

        // ── Builder config (per-job E2B sandbox) ────────────────────────────
        // GithubRepo/GithubToken intentionally left blank: the Builder reads
        // them from the active project record per job.
        builderCfg := agent.BuilderConfig{
                E2BAPIKey: os.Getenv("E2B_API_KEY"),
        }
        if builderCfg.E2BAPIKey == "" {
                log.Println("⚠️  E2B_API_KEY not set — Builder jobs will fail until it is configured.")
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

