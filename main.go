package main

import (
        "context"
        "log"
        "os"
        "os/signal"
        "syscall"

        "github.com/cto-agent/cto-agent/internal/agent"
        "github.com/cto-agent/cto-agent/internal/db"
        "github.com/cto-agent/cto-agent/internal/llm"
        "github.com/cto-agent/cto-agent/internal/web"
)

func main() {
        ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
        defer stop()

        database, err := db.New(ctx, "")
        if err != nil {
                log.Fatalf("db: %v", err)
        }
        defer database.Close()

        srv := web.New(database)

        pool := llm.New(llm.Config{
                KeyFn: func() string {
                        return database.GetConfig(context.Background(), 0, "deepseek_api_key")
                },
                ModelFn: func() string {
                        m := database.GetConfig(context.Background(), 0, "deepseek_model")
                        if m == "" {
                                return "deepseek-v4-pro"
                        }
                        return m
                },
        }, nil)

        pool.OnAllDown = func() {
                srv.SendToProject(1, "⚠️ LLM provider is down or rate-limited. Retrying when possible.")
        }

        ag := agent.New(database, pool, agent.Hooks{
                Send: srv.SendToProject,
                Ask:  srv.AskProject,
                NotifyState: func(projectID int) {
                        srv.NotifyAgentState(projectID)
                },
                Token:          srv.SendToken,
                CancelStream:   srv.CancelStream,
                FinalizeStream: srv.FinalizeStream,
        })

        srv.SetAgent(ag)

        srv.Start(ctx)
}
