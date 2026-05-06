package bot

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/cto-agent/cto-agent/internal/db"
)

// Agent is the interface the bot needs — avoids circular imports.
type Agent interface {
	Run(ctx context.Context)
	Pause(ctx context.Context)
	Resume(ctx context.Context)
	IngestDoc(ctx context.Context, filename, content string) (int, error)
	ScanRepo(ctx context.Context) (string, error)
}

// Bot wraps the Telegram bot API and routes messages to the agent.
type Bot struct {
	api      *tgbotapi.BotAPI
	db       *db.DB
	agent    Agent
	chatID   int64       // first chat that messages us becomes the founder's chat
	mu       sync.Mutex

	// ask() blocks until the founder replies — this channel delivers the reply.
	pending   chan string
	askActive bool
}

// New creates a new Bot (does not start polling yet).
func New(token string, database *db.DB) (*Bot, error) {
	api, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, fmt.Errorf("telegram init: %w", err)
	}
	api.Debug = false
	log.Printf("[bot] authorised as @%s", api.Self.UserName)

	return &Bot{
		api:     api,
		db:      database,
		pending: make(chan string, 1),
	}, nil
}

// SetAgent wires the agent after construction (breaks init cycle).
func (b *Bot) SetAgent(a Agent) {
	b.agent = a
}

// Send pushes a message to the founder (Markdown).
func (b *Bot) Send(text string) {
	b.mu.Lock()
	cid := b.chatID
	b.mu.Unlock()

	if cid == 0 {
		log.Printf("[bot] send skipped — no chat yet: %s", truncate(text, 80))
		return
	}

	msg := tgbotapi.NewMessage(cid, text)
	msg.ParseMode = tgbotapi.ModeMarkdown

	if _, err := b.api.Send(msg); err != nil {
		// Markdown parse failure? Try plain text.
		msg.ParseMode = ""
		_, _ = b.api.Send(msg)
	}
}

// Ask sends a question and blocks until the founder replies (or 10 min timeout).
func (b *Bot) Ask(question string) string {
	b.mu.Lock()
	b.askActive = true
	b.mu.Unlock()

	defer func() {
		b.mu.Lock()
		b.askActive = false
		b.mu.Unlock()
	}()

	b.Send(question)

	select {
	case reply := <-b.pending:
		return reply
	case <-time.After(10 * time.Minute):
		b.Send("⏰ No reply in 10 min — using best judgment and continuing.")
		return ""
	}
}

// Start begins the Telegram polling loop. Blocks until ctx is cancelled.
func (b *Bot) Start(ctx context.Context) {
	cfg := tgbotapi.NewUpdate(0)
	cfg.Timeout = 30

	updates := b.api.GetUpdatesChan(cfg)

	for {
		select {
		case <-ctx.Done():
			b.api.StopReceivingUpdates()
			return
		case update, ok := <-updates:
			if !ok {
				return
			}
			b.handleUpdate(ctx, update)
		}
	}
}

// ─── Update routing ────────────────────────────────────────────────────────

func (b *Bot) handleUpdate(ctx context.Context, update tgbotapi.Update) {
	if update.Message == nil {
		return
	}

	msg := update.Message

	// Register founder's chat on first contact
	b.mu.Lock()
	if b.chatID == 0 {
		b.chatID = msg.Chat.ID
		log.Printf("[bot] founder chat registered: %d", b.chatID)
	}
	active := b.askActive
	b.mu.Unlock()

	// If agent is waiting for a reply, forward directly
	if active && !isCommand(msg.Text) {
		select {
		case b.pending <- msg.Text:
		default:
		}
		return
	}

	// Document upload — ingest markdown files
	if msg.Document != nil {
		b.handleDocument(ctx, msg)
		return
	}

	// Commands
	switch {
	case msg.Text == "/start" || msg.Text == "/help":
		b.handleHelp()
	case msg.Text == "/pause":
		b.agent.Pause(ctx)
	case msg.Text == "/resume":
		b.agent.Resume(ctx)
	case msg.Text == "/status":
		b.handleStatus(ctx)
	case msg.Text == "/scan":
		b.handleScan(ctx)
	case msg.Text == "/usage":
		b.handleUsage(ctx)
	case strings.HasPrefix(msg.Text, "/replan"):
		b.Send("🔄 Replan triggered — resuming from replanning state...")
		go b.agent.Run(ctx)
	default:
		// Free-form message while not in ask() — offer help
		if msg.Text != "" {
			b.Send("I'm not waiting for input right now.\n\nAvailable commands:\n/status /pause /resume /scan /usage /replan")
		}
	}
}

// handleDocument downloads and ingests a .md file sent by the founder.
func (b *Bot) handleDocument(ctx context.Context, msg *tgbotapi.Message) {
	doc := msg.Document
	if !strings.HasSuffix(strings.ToLower(doc.FileName), ".md") {
		b.Send("⚠️ Only `.md` (Markdown) files are accepted for doc ingest.")
		return
	}

	b.Send(fmt.Sprintf("📄 Received *%s* — ingesting...", doc.FileName))

	// Download file
	fileURL, err := b.api.GetFileDirectURL(doc.FileID)
	if err != nil {
		b.Send(fmt.Sprintf("❌ Could not get file URL: %v", err))
		return
	}

	content, err := downloadText(fileURL)
	if err != nil {
		b.Send(fmt.Sprintf("❌ Download failed: %v", err))
		return
	}

	chunks, err := b.agent.IngestDoc(ctx, doc.FileName, content)
	if err != nil {
		b.Send(fmt.Sprintf("❌ Ingest failed: %v", err))
		return
	}

	b.Send(fmt.Sprintf("✅ *%s* ingested — %d chunks tagged.\n\nSend more files or /status to check readiness.", doc.FileName, chunks))

	// Check if we should auto-advance out of ingesting state
	n, _ := b.db.CountDocChunks(ctx)
	if n >= 5 {
		b.Send("📋 Looks like you've uploaded enough docs. Use /status to check trust score, or I'll continue when ready.")
	}
}

// ─── Command handlers ──────────────────────────────────────────────────────

func (b *Bot) handleHelp() {
	b.Send(`👋 *Kaptaan — your autonomous CTO agent*

*Commands:*
/status — show agent state + trust score
/pause  — pause the agent after current task
/resume — resume the agent
/scan   — scan the GitHub repo
/usage  — LLM token usage summary
/replan — trigger a replan cycle

*To start:* upload your project docs as \`.md\` files.`)
}

func (b *Bot) handleStatus(ctx context.Context) {
	proj, err := b.db.GetProject(ctx)
	if err != nil {
		b.Send("No project found yet. Upload your docs to get started.")
		return
	}

	plan, _ := b.db.GetActivePlan(ctx)
	planInfo := "none"
	if plan != nil {
		tasks, _ := b.db.GetTasksByPlan(ctx, plan.ID)
		done := 0
		for _, t := range tasks {
			if t.ParentID == nil && t.Status == "done" {
				done++
			}
		}
		total := 0
		for _, t := range tasks {
			if t.ParentID == nil {
				total++
			}
		}
		planInfo = fmt.Sprintf("v%d — %d/%d tasks done", plan.Version, done, total)
	}

	b.Send(fmt.Sprintf(`📊 *Status*

Project: %s
State:   %s
Trust:   %.1f%%
Plan:    %s`,
		proj.Name, proj.Status, proj.TrustScore, planInfo))
}

func (b *Bot) handleScan(ctx context.Context) {
	b.Send("🔍 Scanning repo...")
	out, err := b.agent.ScanRepo(ctx)
	if err != nil {
		b.Send(fmt.Sprintf("❌ Scan failed: %v", err))
		return
	}
	b.Send(fmt.Sprintf("```\n%s\n```", truncate(out, 2000)))
}

func (b *Bot) handleUsage(ctx context.Context) {
	usage, err := b.db.GetUsageSummary(ctx)
	if err != nil || len(usage) == 0 {
		b.Send("No usage recorded yet.")
		return
	}

	var sb strings.Builder
	sb.WriteString("📈 *LLM Usage (all time)*\n\n")
	for _, u := range usage {
		sb.WriteString(fmt.Sprintf("`%s/%s`\n  prompt: %d | completion: %d | total: %d\n\n",
			u.Provider, u.Model, u.PromptTokens, u.CompletionTokens, u.TotalTokens))
	}

	today, _ := b.db.GetUsageToday(ctx)
	if len(today) > 0 {
		sb.WriteString("*Today:*\n")
		for _, u := range today {
			sb.WriteString(fmt.Sprintf("`%s` — %d tokens\n", u.Model, u.TotalTokens))
		}
	}

	b.Send(sb.String())
}

// ─── Helpers ───────────────────────────────────────────────────────────────

func isCommand(text string) bool {
	return strings.HasPrefix(text, "/")
}

func downloadText(url string) (string, error) {
	resp, err := http.Get(url) //nolint:gosec
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 5<<20)) // 5 MB cap
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
