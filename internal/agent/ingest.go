package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cto-agent/cto-agent/internal/db"
	"github.com/cto-agent/cto-agent/internal/llm"
)

// IngestDoc parses a markdown document, chunks it, tags each chunk via LLM,
// and stores everything in the DB. Returns chunk count.
func (a *Agent) IngestDoc(ctx context.Context, filename, content string) (int, error) {
	// 1. Save raw doc
	docID, err := a.db.SaveDoc(ctx, filename, content)
	if err != nil {
		return 0, fmt.Errorf("save doc: %w", err)
	}

	// 2. Chunk by headings
	chunks := chunkMarkdown(content)
	if len(chunks) == 0 {
		chunks = []string{content} // fallback: whole doc as one chunk
	}

	// 3. Tag each chunk via LLM
	tagged := 0
	for _, chunk := range chunks {
		if strings.TrimSpace(chunk) == "" {
			continue
		}

		tags, relevance, err := a.tagChunk(ctx, chunk)
		if err != nil {
			// Fallback tags — don't fail the whole ingest
			tags = []string{"other"}
			relevance = "untagged"
		}

		if err := a.db.SaveDocChunk(ctx, docID, chunk, tags, relevance); err != nil {
			return tagged, fmt.Errorf("save chunk: %w", err)
		}
		tagged++
	}

	return tagged, nil
}

// chunkMarkdown splits a markdown document at H2/H3 heading boundaries.
func chunkMarkdown(content string) []string {
	lines := strings.Split(content, "\n")
	var chunks []string
	var current strings.Builder

	for _, line := range lines {
		// Split at H2 or H3 headings
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

	// Last chunk
	if last := strings.TrimSpace(current.String()); last != "" {
		chunks = append(chunks, last)
	}

	// If chunks are too large, split further at 1500 char boundaries
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

// splitLarge splits a string into ~maxLen chunks at paragraph boundaries.
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

// tagChunk asks the LLM to assign tags and a short relevance label to a chunk.
func (a *Agent) tagChunk(ctx context.Context, chunk string) (tags []string, relevance string, err error) {
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

	resp, err := a.llm.ChatJSON(ctx, []llm.Message{
		{Role: "user", Content: prompt},
	})
	if err != nil {
		return nil, "", err
	}

	if len(resp.Choices) == 0 {
		return nil, "", fmt.Errorf("empty response")
	}

	text := resp.Choices[0].Message.Content
	// Strip markdown fences if present
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	text = strings.TrimSpace(text)

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

// BuildDocContext fetches relevant chunks for a given task topic.
func (a *Agent) BuildDocContext(ctx context.Context, topic string, maxChunks int) (string, error) {
	// Map topic keywords to tags
	tags := topicToTags(topic)

	chunks, err := a.db.SearchDocChunks(ctx, tags, maxChunks)
	if err != nil || len(chunks) == 0 {
		// Fallback: get any chunks
		chunks, err = a.db.GetAllDocChunks(ctx)
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

// DocCoverageScore returns 0-1 representing how well the docs cover key areas.
func (a *Agent) DocCoverageScore(ctx context.Context) (float64, error) {
	chunks, err := a.db.GetAllDocChunks(ctx)
	if err != nil {
		return 0, err
	}
	if len(chunks) == 0 {
		return 0, nil
	}

	requiredTags := []string{"feature", "api", "schema", "rule"}
	foundTags := map[string]bool{}

	for _, c := range chunks {
		for _, t := range c.Tags {
			foundTags[t] = true
		}
	}

	found := 0
	for _, t := range requiredTags {
		if foundTags[t] {
			found++
		}
	}

	// Also reward chunk volume (more docs = more confidence)
	volumeScore := float64(len(chunks)) / 20.0 // saturates at 20 chunks
	if volumeScore > 1 {
		volumeScore = 1
	}

	tagScore := float64(found) / float64(len(requiredTags))
	return (tagScore*0.7 + volumeScore*0.3), nil
}
