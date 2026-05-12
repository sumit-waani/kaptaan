package agent

import "github.com/cto-agent/cto-agent/internal/llm"

func (t *turn) toolDefs() []llm.Tool {
        return allTools
}

func mkTool(name, desc string, params map[string]interface{}) llm.Tool {
        t := llm.Tool{Type: "function"}
        t.Function.Name = name
        t.Function.Description = desc
        t.Function.Parameters = params
        return t
}

func obj(props map[string]interface{}, required ...string) map[string]interface{} {
        req := make([]string, len(required))
        copy(req, required)
        return map[string]interface{}{
                "type":       "object",
                "properties": props,
                "required":   req,
        }
}

func sprop(desc string) map[string]interface{} {
        return map[string]interface{}{"type": "string", "description": desc}
}
func iprop(desc string) map[string]interface{} {
        return map[string]interface{}{"type": "integer", "description": desc}
}
func bprop(desc string) map[string]interface{} {
        return map[string]interface{}{"type": "boolean", "description": desc}
}

var allTools = []llm.Tool{
        // ── Chat ──
        mkTool("send", "Send a markdown message to the user (visible in the chat feed).",
                obj(map[string]interface{}{"text": sprop("markdown body")}, "text")),
        mkTool("ask", "Ask the user a question and block until they reply.",
                obj(map[string]interface{}{"question": sprop("the question to ask")}, "question")),

        // ── Scratchpad ──
        mkTool("write_scratchpad", "Overwrite scratchpad.md in the sandbox workspace. Use this to maintain your todo list.",
                obj(map[string]interface{}{"content": sprop("full markdown content to write")}, "content")),
        mkTool("read_scratchpad", "Read the current contents of scratchpad.md from the sandbox workspace.",
                obj(map[string]interface{}{})),

        // ── Memories ──
        mkTool("list_memories", "List long-lived memories saved for this project.",
                obj(map[string]interface{}{})),
        mkTool("read_memory", "Fetch one memory by key.",
                obj(map[string]interface{}{"key": sprop("memory key")}, "key")),
        mkTool("write_memory", "Upsert a long-lived memory by key (markdown / freeform).",
                obj(map[string]interface{}{
                        "key":     sprop("short identifier"),
                        "content": sprop("body to remember"),
                }, "key", "content")),
        mkTool("delete_memory", "Delete a memory by key.",
                obj(map[string]interface{}{"key": sprop("memory key")}, "key")),

        // ── Sandbox file ops ──
        mkTool("list_repo", "List directory contents inside the sandbox workspace (default '.').",
                obj(map[string]interface{}{"path": sprop("relative or absolute path")})),
        mkTool("read_file", "Read a file from the sandbox workspace.",
                obj(map[string]interface{}{"path": sprop("relative or absolute path")}, "path")),
        mkTool("grep_repo", "Recursive grep inside the sandbox workspace.",
                obj(map[string]interface{}{
                        "pattern": sprop("regex / literal"),
                        "path":    sprop("optional starting path"),
                }, "pattern")),

        mkTool("write_file", "Write a file inside the sandbox workspace.",
                obj(map[string]interface{}{
                        "path":    sprop("relative or absolute path"),
                        "content": sprop("full file contents"),
                }, "path", "content")),
        mkTool("shell", "Run a bash command in the sandbox.",
                obj(map[string]interface{}{
                        "cmd":          sprop("bash command"),
                        "timeout_secs": iprop("seconds before SIGKILL (default 60)"),
                }, "cmd")),
        mkTool("git_commit", "Stage all changes and commit on the current branch.",
                obj(map[string]interface{}{
                        "message": sprop("commit message"),
                }, "message")),
        mkTool("reset_sandbox", "Pause the sandbox. Call after completing a task. The sandbox state (repo, branch, files) is preserved for the next task.",
                obj(map[string]interface{}{})),

        // ── SSH tools ──
        mkTool("ssh_exec", "Execute a command on a remote host via SSH. Host keys/tokens are read from DB (never visible to LLM).",
                obj(map[string]interface{}{
                        "host":         sprop("logical host name exactly as configured in ssh_hosts (use list_memories or ask the user if unsure)"),
                        "cmd":          sprop("shell command to run"),
                        "timeout_secs": iprop("timeout in seconds (default 30)"),
                }, "host", "cmd")),
        mkTool("ssh_upload", "Upload/write content to a file on a remote host via SSH.",
                obj(map[string]interface{}{
                        "host":          sprop("logical host name exactly as configured in ssh_hosts (use list_memories or ask the user if unsure)"),
                        "local_content": sprop("content to write to the remote file"),
                        "remote_path":   sprop("absolute path on the remote host"),
                }, "host", "local_content", "remote_path")),
        mkTool("ssh_read", "Read a file from a remote host via SSH.",
                obj(map[string]interface{}{
                        "host":        sprop("logical host name exactly as configured in ssh_hosts (use list_memories or ask the user if unsure)"),
                        "remote_path": sprop("absolute path on the remote host"),
                }, "host", "remote_path")),

        // ── GitHub tools ──
        mkTool("gh_list_issues", "List issues on the configured GitHub repo.",
                obj(map[string]interface{}{
                        "state": sprop("issue state filter: open, closed, or all (default open)"),
                })),
        mkTool("gh_create_issue", "Create a new issue on the configured GitHub repo.",
                obj(map[string]interface{}{
                        "title": sprop("issue title"),
                        "body":  sprop("issue body (markdown)"),
                }, "title")),
        mkTool("gh_close_issue", "Close an issue on the configured GitHub repo by number.",
                obj(map[string]interface{}{
                        "number": iprop("issue number to close"),
                }, "number")),
        mkTool("gh_list_workflows", "List GitHub Actions workflows on the configured repo.",
                obj(map[string]interface{}{})),
        mkTool("gh_trigger_workflow", "Trigger a workflow_dispatch event for a GitHub Actions workflow.",
                obj(map[string]interface{}{
                        "workflow_id": sprop("workflow ID or filename"),
                        "ref":         sprop("git ref to run on (default main)"),
                }, "workflow_id", "ref")),
        mkTool("gh_get_workflow_run", "Get status and conclusion of a specific workflow run.",
                obj(map[string]interface{}{
                        "run_id": iprop("workflow run ID"),
                }, "run_id")),
        mkTool("gh_list_branches", "List branches on the configured GitHub repo.",
                obj(map[string]interface{}{})),
        mkTool("gh_delete_branch", "Delete a branch on the configured GitHub repo.",
                obj(map[string]interface{}{
                        "branch": sprop("branch name to delete"),
                }, "branch")),
        mkTool("gh_get_file", "Get file content directly from the GitHub repo (no sandbox needed).",
                obj(map[string]interface{}{
                        "path": sprop("path to the file in the repo"),
                        "ref":  sprop("git ref (branch, tag, SHA) — default main"),
                }, "path")),

        // ── Cloudflare tools ──
        mkTool("cf_list_dns_records", "List DNS records for the configured Cloudflare zone.",
                obj(map[string]interface{}{
                        "type": sprop("optional DNS record type filter (A, AAAA, CNAME, MX, TXT, etc.)"),
                })),
        mkTool("cf_create_dns", "Create a DNS record in the configured Cloudflare zone.",
                obj(map[string]interface{}{
                        "type":    sprop("record type: A, AAAA, CNAME, MX, TXT, etc."),
                        "name":    sprop("DNS name (e.g. 'www.example.com')"),
                        "content": sprop("record content (e.g. IP address or target)"),
                        "ttl":     iprop("TTL in seconds (use 1 for automatic)"),
                        "proxied": bprop("enable Cloudflare proxy (orange cloud) — default false"),
                }, "type", "name", "content")),
        mkTool("cf_update_dns", "Update an existing DNS record in the configured Cloudflare zone.",
                obj(map[string]interface{}{
                        "record_id": sprop("record ID to update (from cf_list_dns_records)"),
                        "type":      sprop("record type: A, AAAA, CNAME, MX, TXT, etc."),
                        "name":      sprop("DNS name (e.g. 'www.example.com')"),
                        "content":   sprop("record content (e.g. IP address or target)"),
                        "proxied":   bprop("enable Cloudflare proxy (orange cloud) — default false"),
                }, "record_id", "type", "name", "content")),
        mkTool("cf_delete_dns", "Delete a DNS record from the configured Cloudflare zone.",
                obj(map[string]interface{}{
                        "record_id": sprop("record ID to delete (from cf_list_dns_records)"),
                }, "record_id")),
        mkTool("cf_purge_cache", "Purge Cloudflare cache for specific URLs or everything.",
                obj(map[string]interface{}{
                        "files": sprop("comma-separated URLs to purge, or 'everything' / '*' to purge all"),
                }, "files")),
        mkTool("cf_get_analytics", "Get basic zone analytics from Cloudflare for a time window.",
                obj(map[string]interface{}{
                        "since_hours": iprop("lookback window in hours (default 24, max 72)"),
                })),
}
