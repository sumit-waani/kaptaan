package agent

import "github.com/cto-agent/cto-agent/internal/llm"

// toolDefs returns the tool schema sent to DeepSeek every iteration.
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
	return map[string]interface{}{
		"type":       "object",
		"properties": props,
		"required":   required,
	}
}

func sprop(desc string) map[string]interface{} {
	return map[string]interface{}{"type": "string", "description": desc}
}
func iprop(desc string) map[string]interface{} {
	return map[string]interface{}{"type": "integer", "description": desc}
}

var allTools = []llm.Tool{
	mkTool("send", "Send a markdown message to the user (visible in the chat feed).",
		obj(map[string]interface{}{"text": sprop("markdown body")}, "text")),
	mkTool("ask", "Ask the user a question and block until they reply.",
		obj(map[string]interface{}{"question": sprop("the question to ask")}, "question")),

	mkTool("list_plans", "List plan files saved on disk for the active project (newest first).",
		obj(map[string]interface{}{})),
	mkTool("read_plan", "Read a plan file by basename (e.g. '1730000000-add-auth.plan.md').",
		obj(map[string]interface{}{"filename": sprop("plan basename")}, "filename")),
	mkTool("write_plan", "Write a NEW plan file. Required before any mutating tool call this turn.",
		obj(map[string]interface{}{
			"slug":    sprop("kebab-case slug, e.g. 'add-login'"),
			"content": sprop("markdown plan body"),
		}, "slug", "content")),
	mkTool("update_plan", "Overwrite an existing plan file (preserves filename/timestamp).",
		obj(map[string]interface{}{
			"filename": sprop("existing plan basename"),
			"content":  sprop("new full markdown body"),
		}, "filename", "content")),

	mkTool("list_memories", "List long-lived memories saved for the active project.",
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

	mkTool("list_repo", "List directory contents inside the sandbox workspace (default '.').",
		obj(map[string]interface{}{"path": sprop("relative or absolute path")})),
	mkTool("read_file", "Read a file from the sandbox workspace.",
		obj(map[string]interface{}{"path": sprop("relative or absolute path")}, "path")),
	mkTool("grep_repo", "Recursive grep inside the sandbox workspace.",
		obj(map[string]interface{}{
			"pattern": sprop("regex / literal"),
			"path":    sprop("optional starting path"),
		}, "pattern")),

	mkTool("write_file", "Write a file inside the sandbox workspace (mutating; plan required).",
		obj(map[string]interface{}{
			"path":    sprop("relative or absolute path"),
			"content": sprop("full file contents"),
		}, "path", "content")),
	mkTool("shell", "Run a bash command in the sandbox (mutating; plan required).",
		obj(map[string]interface{}{
			"cmd":          sprop("bash command"),
			"timeout_secs": iprop("seconds before SIGKILL (default 60)"),
		}, "cmd")),
	mkTool("git_commit", "Stage all changes and commit on the current (or specified) branch.",
		obj(map[string]interface{}{
			"message": sprop("commit message"),
			"branch":  sprop("optional branch to checkout -B before committing"),
		}, "message")),
	mkTool("open_pr", "Push the branch and open a pull request via the GitHub REST API.",
		obj(map[string]interface{}{
			"title":  sprop("PR title"),
			"body":   sprop("PR body markdown"),
			"branch": sprop("head branch (must already exist locally)"),
			"base":   sprop("base branch, default 'main'"),
		}, "title", "branch")),
	mkTool("merge_pr", "Merge a pull request by number.",
		obj(map[string]interface{}{"number": iprop("pull request number")}, "number")),
}
