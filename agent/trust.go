package agent

import (
	"context"
	"fmt"
	"math"
	"strings"
)

// TrustBreakdown holds per-signal scores.
type TrustBreakdown struct {
	DocCoverage     float64 // 0-1
	Clarifications  float64 // 0-1
	RepoScan        float64 // 0-1
	ArchPatterns    float64 // 0-1
	AmbiguityInverse float64 // 0-1
	Total           float64 // 0-100
}

func (t TrustBreakdown) String() string {
	bar := func(v float64) string {
		filled := int(math.Round(v * 10))
		return strings.Repeat("█", filled) + strings.Repeat("░", 10-filled)
	}
	return fmt.Sprintf(
		`📊 Trust Score: %.1f%%

Doc Coverage     [%s] %.0f%%
Clarifications   [%s] %.0f%%
Repo Scan        [%s] %.0f%%
Architecture     [%s] %.0f%%
Low Ambiguity    [%s] %.0f%%`,
		t.Total,
		bar(t.DocCoverage), t.DocCoverage*100,
		bar(t.Clarifications), t.Clarifications*100,
		bar(t.RepoScan), t.RepoScan*100,
		bar(t.ArchPatterns), t.ArchPatterns*100,
		bar(t.AmbiguityInverse), t.AmbiguityInverse*100,
	)
}

// CalculateTrustScore computes and persists the current trust score.
func (a *Agent) CalculateTrustScore(ctx context.Context) (TrustBreakdown, error) {
	bd := TrustBreakdown{}

	// 1. Doc coverage (30%)
	docScore, err := a.DocCoverageScore(ctx)
	if err != nil {
		docScore = 0
	}
	bd.DocCoverage = docScore

	// 2. Clarifications answered (25%)
	total, answered, err := a.db.CountClarifications(ctx)
	if err == nil && total > 0 {
		bd.Clarifications = float64(answered) / float64(total)
	} else if total == 0 {
		// No clarifications needed yet — neutral
		bd.Clarifications = 0.5
	}

	// 3. Repo scan (15%)
	repoScore, err := a.repoScanScore(ctx)
	if err != nil {
		repoScore = 0
	}
	bd.RepoScan = repoScore

	// 4. Architecture pattern match (20%)
	archScore, err := a.archPatternScore(ctx)
	if err != nil {
		archScore = 0
	}
	bd.ArchPatterns = archScore

	// 5. Ambiguity inverse (10%)
	ambig := a.ambiguityScore(ctx)
	bd.AmbiguityInverse = ambig

	// Weighted total
	bd.Total = (bd.DocCoverage*0.30 +
		bd.Clarifications*0.25 +
		bd.RepoScan*0.15 +
		bd.ArchPatterns*0.20 +
		bd.AmbiguityInverse*0.10) * 100

	// Persist
	_ = a.db.UpdateProjectTrustScore(ctx, bd.Total)

	return bd, nil
}

// repoScanScore checks if repo has been cloned and scanned.
func (a *Agent) repoScanScore(ctx context.Context) (float64, error) {
	scanned := a.db.KVGetDefault(ctx, "repo_scanned", "0")
	if scanned != "1" {
		return 0, nil
	}

	fileCount := 0
	fmt.Sscanf(a.db.KVGetDefault(ctx, "repo_file_count", "0"), "%d", &fileCount)

	if fileCount == 0 {
		return 0.3, nil // cloned but empty
	}
	if fileCount < 5 {
		return 0.6, nil
	}
	return 1.0, nil
}

// archPatternScore checks for Go project structure patterns.
func (a *Agent) archPatternScore(ctx context.Context) (float64, error) {
	patterns := a.db.KVGetDefault(ctx, "arch_patterns", "")
	if patterns == "" {
		return 0, nil
	}

	found := strings.Split(patterns, ",")
	knownPatterns := []string{"go.mod", "main.go", "handler", "model", "migration", "test", "middleware"}
	matches := 0
	for _, f := range found {
		for _, kp := range knownPatterns {
			if strings.Contains(strings.ToLower(f), kp) {
				matches++
				break
			}
		}
	}

	score := float64(matches) / float64(len(knownPatterns))
	if score > 1 {
		score = 1
	}
	return score, nil
}

// ambiguityScore returns inverse of open ambiguity count.
func (a *Agent) ambiguityScore(ctx context.Context) float64 {
	total, answered, err := a.db.CountClarifications(ctx)
	if err != nil {
		return 0.5
	}
	open := total - answered
	if open == 0 {
		return 1.0
	}
	// Each open ambiguity reduces score
	score := 1.0 - float64(open)*0.2
	if score < 0 {
		score = 0
	}
	return score
}

// ScanRepo runs a file tree scan and updates arch_patterns in KV.
func (a *Agent) ScanRepo(ctx context.Context) (string, error) {
	result := a.exec.Shell(ctx,
		`find . -type f -name "*.go" | head -100 && echo "---" && cat go.mod 2>/dev/null || echo "no go.mod"`,
		30)

	output := result.Output

	// Extract patterns
	patterns := []string{}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line == "---" {
			continue
		}
		patterns = append(patterns, line)
	}

	_ = a.db.KVSet(ctx, "repo_scanned", "1")
	_ = a.db.KVSet(ctx, "repo_file_count", fmt.Sprintf("%d", len(patterns)))
	_ = a.db.KVSet(ctx, "arch_patterns", strings.Join(patterns, ","))

	return output, nil
}

// ReadyToBuild returns true if trust score >= 95.
func (a *Agent) ReadyToBuild(ctx context.Context) (bool, TrustBreakdown, error) {
	bd, err := a.CalculateTrustScore(ctx)
	if err != nil {
		return false, bd, err
	}
	return bd.Total >= 95.0, bd, nil
}
