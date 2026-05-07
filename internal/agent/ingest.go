package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cto-agent/cto-agent/internal/db"
	"github.com/cto-agent/cto-agent/internal/llm"
)

func IngestDoc(ctx context.Context, database *db.DB, pool *llm.Pool, filename, content string) (int, error) {
	return ingestDoc(ctx, database, pool, filename, content)
}

func ingestDoc(ctx context.Context, database *db.DB, pool *llm.Pool, filename, content string) (int, error) {
	docID, err := database.SaveDoc(ctx, filename, content)
	if err != nil {
		return 0, fmt.Errorf("save doc: %w", err)
	}

	chunks := chunkMarkdown(content)
	if len(chunks) == 0 {
		chunks = []string{content}
	}

	tagged := 0
	for _, chunk := range chunks {
		if strings.TrimSpace(chunk) == "" {
			continue
		}

		tags, relevance, err := tagChunk(ctx, pool, chunk)
		if err != nil {
			tags = []string{"other"}
			relevance = "untagged"
		}

		if err := database.SaveDocChunk(ctx, docID, chunk, tags, relevance); err != nil {
			return tagged, fmt.Errorf("save chunk: %w", err)
		}
		tagged++
	}

	return tagged, nil
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

func tagChunk(ctx context.Context, pool *llm.Pool, chunk string) (tags []string, relevance string, err error) {
	prompt := fmt.Sprintf(`You are tagging a documentation chunk for a software project.

Chunk:
---
%s
---

Respond ONLY with valid JSON (no markdown, no explanation):
{
  "tags": ["one or more of: feature, api, schema, rule, ui, data, config, auth, infra, other"],
  "relevance": "short label like: user auth, payment flow, data model, API endpoints, deployment rules"
}`, chunk)

	resp, err := pool.ChatJSON(ctx, []llm.Message{
		{Role: "user", Content: prompt},
	})
	if err != nil {
		return nil, "", err
	}

	if len(resp.Choices) == 0 {
		return nil, "", fmt.Errorf("empty response")
	}

	text := cleanJSON(resp.Choices[0].Message.Content)

	var result struct {
		Tags      []string `json:"tags"`
		Relevance string   `json:"relevance"`
	}
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		return []string{"other"}, "untagged", nil
	}

	if len(result.Tags) == 0 {
		result.Tags = []string{"other"}
	}
	if result.Relevance == "" {
		result.Relevance = "general"
	}

	return result.Tags, result.Relevance, nil
}

func BuildDocContext(ctx context.Context, database *db.DB, topic string, maxChunks int) (string, error) {
	tags := topicToTags(topic)

	chunks, err := database.SearchDocChunks(ctx, tags, maxChunks)
	if err != nil || len(chunks) == 0 {
		chunks, err = database.GetAllDocChunks(ctx)
		if err != nil {
			return "", err
		}
		if len(chunks) > maxChunks {
			chunks = chunks[:maxChunks]
		}
	}

	if len(chunks) == 0 {
		return "No documentation available.", nil
	}

	var sb strings.Builder
	sb.WriteString("=== RELEVANT DOCUMENTATION ===\n\n")
	for _, c := range chunks {
		sb.WriteString(fmt.Sprintf("[%s | tags: %s]\n", c.Relevance, strings.Join(c.Tags, ",")))
		sb.WriteString(c.ChunkText)
		sb.WriteString("\n\n---\n\n")
	}
	return sb.String(), nil
}

func topicToTags(topic string) []string {
	topic = strings.ToLower(topic)
	tagMap := map[string][]string{
		"auth":    {"auth", "rule", "api"},
		"api":     {"api", "feature"},
		"schema":  {"schema", "data"},
		"ui":      {"ui", "feature"},
		"config":  {"config", "infra"},
		"deploy":  {"infra", "config"},
		"test":    {"feature", "rule"},
		"model":   {"schema", "data"},
		"feature": {"feature"},
		"rule":    {"rule"},
	}

	for keyword, tags := range tagMap {
		if strings.Contains(topic, keyword) {
			return tags
		}
	}
	return []string{"feature", "rule", "api"}
}
