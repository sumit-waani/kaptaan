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
}
