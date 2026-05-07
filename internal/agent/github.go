package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// pullRequest is a tiny subset of the GitHub PR object we surface to the LLM.
type pullRequest struct {
	Number  int    `json:"number"`
	HTMLURL string `json:"html_url"`
	State   string `json:"state"`
	Title   string `json:"title"`
}

// parseOwnerRepo extracts owner + repo from a github.com URL like
// https://github.com/owner/repo or https://github.com/owner/repo.git.
func parseOwnerRepo(url string) (string, string, error) {
	u := strings.TrimSuffix(strings.TrimSpace(url), ".git")
	for _, prefix := range []string{"https://github.com/", "http://github.com/", "git@github.com:"} {
		if strings.HasPrefix(u, prefix) {
			rest := strings.TrimPrefix(u, prefix)
			parts := strings.SplitN(rest, "/", 2)
			if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
				return "", "", fmt.Errorf("cannot parse owner/repo from %q", url)
			}
			return parts[0], strings.TrimSuffix(parts[1], "/"), nil
		}
	}
	return "", "", fmt.Errorf("not a recognised GitHub URL: %q", url)
}

func githubReq(ctx context.Context, token, method, path string, body interface{}) ([]byte, int, error) {
	var buf io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		buf = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, "https://api.github.com"+path, buf)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if buf != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	cl := &http.Client{Timeout: 30 * time.Second}
	resp, err := cl.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return data, resp.StatusCode, nil
}

func githubCreatePR(ctx context.Context, token, owner, repo, title, body, head, base string) (*pullRequest, error) {
	payload := map[string]interface{}{
		"title": title,
		"body":  body,
		"head":  head,
		"base":  base,
	}
	data, status, err := githubReq(ctx, token, "POST",
		fmt.Sprintf("/repos/%s/%s/pulls", owner, repo), payload)
	if err != nil {
		return nil, err
	}
	if status >= 300 {
		return nil, fmt.Errorf("github create_pr %d: %s", status, truncate(string(data), 400))
	}
	var pr pullRequest
	if err := json.Unmarshal(data, &pr); err != nil {
		return nil, err
	}
	return &pr, nil
}

func githubMergePR(ctx context.Context, token, owner, repo string, number int) (string, error) {
	data, status, err := githubReq(ctx, token, "PUT",
		fmt.Sprintf("/repos/%s/%s/pulls/%d/merge", owner, repo, number),
		map[string]interface{}{"merge_method": "squash"})
	if err != nil {
		return "", err
	}
	if status >= 300 {
		return "", fmt.Errorf("github merge_pr %d: %s", status, truncate(string(data), 400))
	}
	return string(data), nil
}
