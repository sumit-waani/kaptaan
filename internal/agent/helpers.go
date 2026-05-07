package agent

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

func cleanJSON(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}

func parseToolArgs(raw string) map[string]string {
	out := map[string]string{}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return out
	}
	for k, v := range m {
		out[k] = fmt.Sprintf("%v", v)
	}
	return out
}

func slugify(s string) string {
	s = strings.ToLower(s)
	s = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			return r
		}
		return '-'
	}, s)
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	s = strings.Trim(s, "-")
	if len(s) > 40 {
		s = s[:40]
	}
	if s == "" {
		return "task"
	}
	return s
}

func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func extractCoverage(output string) string {
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "coverage:") {
			parts := strings.Fields(line)
			for i, p := range parts {
				if p == "coverage:" && i+1 < len(parts) {
					return parts[i+1]
				}
			}
		}
	}
	return "unknown"
}

func prNumberFromURL(prURL string) int {
	parts := strings.Split(strings.TrimSpace(prURL), "/")
	if len(parts) == 0 {
		return 0
	}
	n, _ := strconv.Atoi(parts[len(parts)-1])
	return n
}

func isYes(answer string) bool {
	a := strings.ToLower(strings.TrimSpace(answer))
	return a == "y" || a == "yes" || a == "approve" || a == "ok"
}
