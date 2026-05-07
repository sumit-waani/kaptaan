package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// PlansRoot is where every plan file lives. <root>/<project_id>/<unix>-<slug>.plan.md
const PlansRoot = "data/plans"

// PlanInfo summarises one plan file on disk.
type PlanInfo struct {
	Filename string `json:"filename"`
	Slug     string `json:"slug"`
	Created  string `json:"created"`
	Bytes    int64  `json:"bytes"`
}

func projectPlanDir(projectID int) string {
	return filepath.Join(PlansRoot, fmt.Sprintf("%d", projectID))
}

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	dashed := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dashed = false
		case r == '-' || r == '_':
			b.WriteRune(r)
			dashed = false
		default:
			if !dashed && b.Len() > 0 {
				b.WriteByte('-')
				dashed = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = "plan"
	}
	if len(out) > 60 {
		out = out[:60]
	}
	return out
}

// WritePlan creates a new plan file (always a fresh file, never overwritten).
// Returns the basename so the agent can refer back to it.
func WritePlan(projectID int, slug, content string) (string, error) {
	dir := projectPlanDir(projectID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := fmt.Sprintf("%d-%s.plan.md", time.Now().Unix(), slugify(slug))
	full := filepath.Join(dir, name)
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		return "", err
	}
	return name, nil
}

// UpdatePlan overwrites an existing plan file by basename. Refuses to escape
// the project's plan dir.
func UpdatePlan(projectID int, filename, content string) error {
	dir := projectPlanDir(projectID)
	clean := filepath.Base(filename)
	if clean != filename || !strings.HasSuffix(clean, ".plan.md") {
		return fmt.Errorf("invalid plan filename: %q", filename)
	}
	full := filepath.Join(dir, clean)
	if _, err := os.Stat(full); err != nil {
		return fmt.Errorf("plan not found: %s", clean)
	}
	return os.WriteFile(full, []byte(content), 0o644)
}

// ReadPlan loads a plan file by basename.
func ReadPlan(projectID int, filename string) (string, error) {
	dir := projectPlanDir(projectID)
	clean := filepath.Base(filename)
	if clean != filename || !strings.HasSuffix(clean, ".plan.md") {
		return "", fmt.Errorf("invalid plan filename: %q", filename)
	}
	b, err := os.ReadFile(filepath.Join(dir, clean))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// ListPlans returns every plan file for a project, newest first.
func ListPlans(projectID int) ([]PlanInfo, error) {
	dir := projectPlanDir(projectID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []PlanInfo
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".plan.md") {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		// filename: <unix>-<slug>.plan.md
		stem := strings.TrimSuffix(e.Name(), ".plan.md")
		parts := strings.SplitN(stem, "-", 2)
		slug := stem
		if len(parts) == 2 {
			slug = parts[1]
		}
		out = append(out, PlanInfo{
			Filename: e.Name(),
			Slug:     slug,
			Created:  fi.ModTime().Format(time.RFC3339),
			Bytes:    fi.Size(),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Filename > out[j].Filename })
	return out, nil
}
