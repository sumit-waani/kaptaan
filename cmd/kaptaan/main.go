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
	"github.com/cto-agent/cto-agent/internal/web"
)

func main() {
	_ = godotenv.Load()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	database, err := db.New(ctx, "")
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer database.Close()
	log.Println("✅ database connected")

	server := web.New(database)

	dsKey := os.Getenv("DEEPSEEK_API_KEY")
	if dsKey == "" {
		log.Println("⚠️  DEEPSEEK_API_KEY not set — running in UI-only mode.")
		server.SetMOTD("⚠️ Set `DEEPSEEK_API_KEY` and restart so Kaptaan can think.")
		go server.Start(ctx)
		<-ctx.Done()
		return
	}

	pool := llm.New(llm.Config{DeepSeekKey: dsKey}, nil)
	log.Println("✅ LLM pool ready")

	a := agent.New(database, pool, os.Getenv("E2B_API_KEY"), agent.Hooks{
		Send:           server.SendToProject,
		Ask:            server.AskProject,
		NotifyState:    server.NotifyAgentState,
		Token:          server.SendToken,
		CancelStream:   server.CancelStream,
		FinalizeStream: server.FinalizeStream,
	})
	server.SetAgent(a)

	log.Println("🚀 kaptaan starting on :5000 ...")
	go server.Start(ctx)
	<-ctx.Done()
	log.Println("👋 shutting down")
}
