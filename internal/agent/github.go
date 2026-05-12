package agent

import (
	"bytes"
	"context"
	"encoding/base64"
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

// ghIssue is a subset of the GitHub Issue object.
type ghIssue struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	State  string `json:"state"`
	Body   string `json:"body"`
	URL    string `json:"html_url"`
}

// ghWorkflow is a subset of the GitHub Actions workflow.
type ghWorkflow struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	State string `json:"state"`
	Path  string `json:"path"`
}

// ghWorkflowListResult is the API response for listing workflows.
type ghWorkflowListResult struct {
	TotalCount int          `json:"total_count"`
	Workflows  []ghWorkflow `json:"workflows"`
}

// ghWorkflowRun is a subset of a workflow run.
type ghWorkflowRun struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	HTMLURL    string `json:"html_url"`
	Branch     string `json:"head_branch"`
}

// ghWorkflowRunsResult is the response for listing workflow runs.
type ghWorkflowRunsResult struct {
	TotalCount   int             `json:"total_count"`
	WorkflowRuns []ghWorkflowRun `json:"workflow_runs"`
}

// ghBranch is a subset of a GitHub branch.
type ghBranch struct {
	Name string `json:"name"`
}

// ghFileContent is the GitHub API response for get content.
type ghFileContent struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Content     string `json:"content"`
	Encoding    string `json:"encoding"`
	Size        int    `json:"size"`
	HTMLURL     string `json:"html_url"`
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

// ─── New GitHub tools (turn methods) ──────────────────────────────────────────

func (t *turn) ghToken() string {
	return t.a.db.GetConfig(context.Background(), "github_token")
}

func (t *turn) ghOwnerRepo() (string, string, error) {
	repoURL := t.a.db.GetConfig(context.Background(), "repo_url")
	if repoURL == "" {
		return "", "", fmt.Errorf("repo_url is not configured")
	}
	return parseOwnerRepo(repoURL)
}

// ghListIssues lists open/closed/all issues.
func (t *turn) ghListIssues(ctx context.Context, state string) string {
	token := t.ghToken()
	if token == "" {
		return "ERROR: github_token is not configured"
	}
	owner, repo, err := t.ghOwnerRepo()
	if err != nil {
		return "ERROR: " + err.Error()
	}
	if state == "" || (state != "open" && state != "closed" && state != "all") {
		state = "open"
	}

	data, status, err := githubReq(ctx, token, "GET",
		fmt.Sprintf("/repos/%s/%s/issues?state=%s&per_page=50", owner, repo, state), nil)
	if err != nil {
		return "ERROR: " + err.Error()
	}
	if status >= 300 {
		return fmt.Sprintf("ERROR: GitHub API returned %d: %s", status, truncate(string(data), 400))
	}
	var issues []ghIssue
	if err := json.Unmarshal(data, &issues); err != nil {
		return "ERROR: parse: " + err.Error()
	}
	if len(issues) == 0 {
		return "(no " + state + " issues)"
	}
	var b strings.Builder
	for _, iss := range issues {
		body := strings.SplitN(iss.Body, "\n", 2)[0]
		b.WriteString(fmt.Sprintf("#%d [%s] %s — %s\n", iss.Number, iss.State, iss.Title, truncate(body, 120)))
	}
	return b.String()
}

// ghCreateIssue creates a new issue.
func (t *turn) ghCreateIssue(ctx context.Context, title, body string) string {
	token := t.ghToken()
	if token == "" {
		return "ERROR: github_token is not configured"
	}
	owner, repo, err := t.ghOwnerRepo()
	if err != nil {
		return "ERROR: " + err.Error()
	}

	data, status, err := githubReq(ctx, token, "POST",
		fmt.Sprintf("/repos/%s/%s/issues", owner, repo),
		map[string]string{"title": title, "body": body})
	if err != nil {
		return "ERROR: " + err.Error()
	}
	if status >= 300 {
		return fmt.Sprintf("ERROR: GitHub API returned %d: %s", status, truncate(string(data), 400))
	}
	var iss ghIssue
	if err := json.Unmarshal(data, &iss); err != nil {
		return "ERROR: parse: " + err.Error()
	}
	return fmt.Sprintf("created issue #%d: %s — %s", iss.Number, iss.Title, iss.URL)
}

// ghCloseIssue closes an issue by number.
func (t *turn) ghCloseIssue(ctx context.Context, number int) string {
	token := t.ghToken()
	if token == "" {
		return "ERROR: github_token is not configured"
	}
	owner, repo, err := t.ghOwnerRepo()
	if err != nil {
		return "ERROR: " + err.Error()
	}

	data, status, err := githubReq(ctx, token, "PATCH",
		fmt.Sprintf("/repos/%s/%s/issues/%d", owner, repo, number),
		map[string]string{"state": "closed"})
	if err != nil {
		return "ERROR: " + err.Error()
	}
	if status >= 300 {
		return fmt.Sprintf("ERROR: GitHub API returned %d: %s", status, truncate(string(data), 400))
	}
	return fmt.Sprintf("closed issue #%d", number)
}

// ghListWorkflows lists GitHub Actions workflows.
func (t *turn) ghListWorkflows(ctx context.Context) string {
	token := t.ghToken()
	if token == "" {
		return "ERROR: github_token is not configured"
	}
	owner, repo, err := t.ghOwnerRepo()
	if err != nil {
		return "ERROR: " + err.Error()
	}

	data, status, err := githubReq(ctx, token, "GET",
		fmt.Sprintf("/repos/%s/%s/actions/workflows", owner, repo), nil)
	if err != nil {
		return "ERROR: " + err.Error()
	}
	if status >= 300 {
		return fmt.Sprintf("ERROR: GitHub API returned %d: %s", status, truncate(string(data), 400))
	}
	var res ghWorkflowListResult
	if err := json.Unmarshal(data, &res); err != nil {
		return "ERROR: parse: " + err.Error()
	}
	if len(res.Workflows) == 0 {
		return "(no workflows found)"
	}
	var b strings.Builder
	for _, wf := range res.Workflows {
		b.WriteString(fmt.Sprintf("id=%d  %s  [%s]  %s\n", wf.ID, wf.Name, wf.State, wf.Path))
	}
	b.WriteString(fmt.Sprintf("total: %d", res.TotalCount))
	return b.String()
}

// ghTriggerWorkflow triggers a workflow dispatch.
func (t *turn) ghTriggerWorkflow(ctx context.Context, workflowID, ref string) string {
	token := t.ghToken()
	if token == "" {
		return "ERROR: github_token is not configured"
	}
	owner, repo, err := t.ghOwnerRepo()
	if err != nil {
		return "ERROR: " + err.Error()
	}
	if ref == "" {
		ref = "main"
	}

	data, status, err := githubReq(ctx, token, "POST",
		fmt.Sprintf("/repos/%s/%s/actions/workflows/%s/dispatches", owner, repo, workflowID),
		map[string]string{"ref": ref})
	if err != nil {
		return "ERROR: " + err.Error()
	}
	if status >= 300 {
		return fmt.Sprintf("ERROR: GitHub API returned %d: %s", status, truncate(string(data), 400))
	}
	return fmt.Sprintf("triggered workflow %s on ref %s", workflowID, ref)
}

// ghGetWorkflowRun gets the status of a specific workflow run.
func (t *turn) ghGetWorkflowRun(ctx context.Context, runID int) string {
	token := t.ghToken()
	if token == "" {
		return "ERROR: github_token is not configured"
	}
	owner, repo, err := t.ghOwnerRepo()
	if err != nil {
		return "ERROR: " + err.Error()
	}

	data, status, err := githubReq(ctx, token, "GET",
		fmt.Sprintf("/repos/%s/%s/actions/runs/%d", owner, repo, runID), nil)
	if err != nil {
		return "ERROR: " + err.Error()
	}
	if status >= 300 {
		return fmt.Sprintf("ERROR: GitHub API returned %d: %s", status, truncate(string(data), 400))
	}
	var run ghWorkflowRun
	if err := json.Unmarshal(data, &run); err != nil {
		return "ERROR: parse: " + err.Error()
	}
	return fmt.Sprintf("run #%d  name=%s  status=%s  conclusion=%s  branch=%s\n%s",
		run.ID, run.Name, run.Status, run.Conclusion, run.Branch, run.HTMLURL)
}

// ghListBranches lists branches in the repo.
func (t *turn) ghListBranches(ctx context.Context) string {
	token := t.ghToken()
	if token == "" {
		return "ERROR: github_token is not configured"
	}
	owner, repo, err := t.ghOwnerRepo()
	if err != nil {
		return "ERROR: " + err.Error()
	}

	data, status, err := githubReq(ctx, token, "GET",
		fmt.Sprintf("/repos/%s/%s/branches?per_page=50", owner, repo), nil)
	if err != nil {
		return "ERROR: " + err.Error()
	}
	if status >= 300 {
		return fmt.Sprintf("ERROR: GitHub API returned %d: %s", status, truncate(string(data), 400))
	}
	var branches []ghBranch
	if err := json.Unmarshal(data, &branches); err != nil {
		return "ERROR: parse: " + err.Error()
	}
	if len(branches) == 0 {
		return "(no branches)"
	}
	var b strings.Builder
	for _, br := range branches {
		b.WriteString(br.Name + "\n")
	}
	return b.String()
}

// ghDeleteBranch deletes a branch from the repo.
func (t *turn) ghDeleteBranch(ctx context.Context, branch string) string {
	token := t.ghToken()
	if token == "" {
		return "ERROR: github_token is not configured"
	}
	owner, repo, err := t.ghOwnerRepo()
	if err != nil {
		return "ERROR: " + err.Error()
	}

	data, status, err := githubReq(ctx, token, "DELETE",
		fmt.Sprintf("/repos/%s/%s/git/refs/heads/%s", owner, repo, branch), nil)
	if err != nil {
		return "ERROR: " + err.Error()
	}
	if status >= 300 {
		return fmt.Sprintf("ERROR: GitHub API returned %d: %s", status, truncate(string(data), 400))
	}
	return fmt.Sprintf("deleted branch %s", branch)
}

// ghGetFile fetches file content directly from the GitHub repo API.
func (t *turn) ghGetFile(ctx context.Context, path, ref string) string {
	token := t.ghToken()
	if token == "" {
		return "ERROR: github_token is not configured"
	}
	owner, repo, err := t.ghOwnerRepo()
	if err != nil {
		return "ERROR: " + err.Error()
	}
	if ref == "" {
		ref = "main"
	}

	urlPath := fmt.Sprintf("/repos/%s/%s/contents/%s", owner, repo, path)
	urlPath += "?ref=" + ref

	data, status, err := githubReq(ctx, token, "GET", urlPath, nil)
	if err != nil {
		return "ERROR: " + err.Error()
	}
	if status >= 300 {
		return fmt.Sprintf("ERROR: GitHub API returned %d: %s", status, truncate(string(data), 400))
	}
	var file ghFileContent
	if err := json.Unmarshal(data, &file); err != nil {
		return "ERROR: parse: " + err.Error()
	}
	if file.Encoding != "base64" || file.Content == "" {
		return fmt.Sprintf("ERROR: unsupported encoding %q or empty file", file.Encoding)
	}
	clean := strings.Map(func(r rune) rune {
		if r == ' ' || r == '\n' || r == '\r' || r == '\t' {
			return -1
		}
		return r
	}, file.Content)
	decoded, err := base64.StdEncoding.DecodeString(clean)
	if err != nil {
		return "ERROR: base64 decode: " + err.Error()
	}
	return truncate(string(decoded), 8000)
}
