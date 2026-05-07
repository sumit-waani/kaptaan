package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/cto-agent/cto-agent/internal/db"
)

// IngestDoc saves the markdown doc and splits it into chunks. We deliberately
// skip per-chunk LLM tagging — it was the source of long upload hangs (slow or
// rate-limited providers) and the planner already gets the full chunks back at
// retrieval time, which is enough context.
func IngestDoc(ctx context.Context, database *db.DB, filename, content string) (int, error) {
	return ingestDoc(ctx, database, filename, content)
}

func ingestDoc(ctx context.Context, database *db.DB, filename, content string) (int, error) {
	docID, err := database.SaveDoc(ctx, filename, content)
	if err != nil {
		return 0, fmt.Errorf("save doc: %w", err)
	}

	chunks := chunkMarkdown(content)
	if len(chunks) == 0 {
		chunks = []string{content}
	}

	saved := 0
	for _, chunk := range chunks {
		if strings.TrimSpace(chunk) == "" {
			continue
		}
		if err := database.SaveDocChunk(ctx, docID, chunk, []string{"doc"}, "doc"); err != nil {
			return saved, fmt.Errorf("save chunk: %w", err)
		}
		saved++
	}

	return saved, nil
}

func chunkMarkdown(content string) []string {
	lines := strings.Split(content, "\n")
	var chunks []string
	var current strings.Builder

	for _, line := range lines {
		if (strings.HasPrefix(line, "## ") || strings.HasPrefix(line, "### ")) && current.Len() > 0 {
			chunk := strings.TrimSpace(current.String())
			if chunk != "" {
				chunks = append(chunks, chunk)
			}
			current.Reset()
		}
		current.WriteString(line)
		current.WriteString("\n")
	}

	if last := strings.TrimSpace(current.String()); last != "" {
		chunks = append(chunks, last)
	}

	var result []string
	for _, c := range chunks {
		if len(c) <= 1500 {
			result = append(result, c)
		} else {
			result = append(result, splitLarge(c, 1500)...)
		}
	}

	return result
}

func splitLarge(s string, maxLen int) []string {
	paras := strings.Split(s, "\n\n")
	var chunks []string
	var cur strings.Builder

	for _, p := range paras {
		if cur.Len()+len(p) > maxLen && cur.Len() > 0 {
			chunks = append(chunks, strings.TrimSpace(cur.String()))
			cur.Reset()
		}
		cur.WriteString(p)
		cur.WriteString("\n\n")
	}
	if cur.Len() > 0 {
		chunks = append(chunks, strings.TrimSpace(cur.String()))
	}
	return chunks
}

// BuildDocContext returns relevant doc chunks formatted for prompt injection.
// We return the most-recent chunks (no tag filtering) — the planner gets the
// full corpus, capped at maxChunks.
func BuildDocContext(ctx context.Context, database *db.DB, _ string, maxChunks int) (string, error) {
	chunks, err := database.GetAllDocChunks(ctx)
	if err != nil {
		return "", err
	}
	if len(chunks) > maxChunks {
		chunks = chunks[:maxChunks]
	}
	if len(chunks) == 0 {
		return "(no documentation uploaded yet)", nil
	}

	var sb strings.Builder
	sb.WriteString("=== PROJECT DOCUMENTATION ===\n\n")
	for _, c := range chunks {
		sb.WriteString(c.ChunkText)
		sb.WriteString("\n\n---\n\n")
	}
	return sb.String(), nil
}
