package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"

	"github.com/cto-agent/cto-agent/internal/agent"
	"github.com/cto-agent/cto-agent/internal/bot"
	"github.com/cto-agent/cto-agent/internal/db"
	"github.com/cto-agent/cto-agent/internal/llm"
	"github.com/cto-agent/cto-agent/internal/tools"
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

	// ── LLM pool ────────────────────────────────────────────────────────
	pool := llm.New(llm.Config{
		DeepSeekKey: os.Getenv("DEEPSEEK_API_KEY"),
		NIMKey1:     os.Getenv("NIM_API_KEY_1"),
		NIMKey2:     os.Getenv("NIM_API_KEY_2"),
	}, func(u llm.UsageRecord) {
		// Persist token usage for /usage command
		_ = database.RecordUsage(ctx, u.Provider, u.Model, u.PromptTokens, u.CompletionTokens)
	})
	log.Println("✅ LLM pool ready")

	// ── Tool executor ───────────────────────────────────────────────────
	executor := &tools.Executor{
		WorkspaceDir: workspaceDir(),
		GithubRepo:   os.Getenv("GITHUB_REPO"),
		GithubToken:  os.Getenv("GITHUB_TOKEN"),
	}

	// ── Telegram bot ─────────────────────────────────────────────────────
	tgToken := mustEnv("TELEGRAM_BOT_TOKEN")
	tgBot, err := bot.New(tgToken, database)
	if err != nil {
		log.Fatalf("bot: %v", err)
	}
	log.Println("✅ telegram bot ready")

	// ── Wire agent ──────────────────────────────────────────────────────
	a := agent.New(
		database,
		pool,
		executor,
		tgBot.Send,
		tgBot.Ask,
	)
	tgBot.SetAgent(a)

	// ── Start ────────────────────────────────────────────────────────────
	log.Println("🚀 kaptaan starting...")

	go tgBot.Start(ctx)
	go a.Run(ctx)

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
