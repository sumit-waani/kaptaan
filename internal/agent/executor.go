package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/cto-agent/cto-agent/internal/db"
	"github.com/cto-agent/cto-agent/internal/llm"
	"github.com/cto-agent/cto-agent/internal/tools"
)

const maxBuildRetries = 3
const maxAgentIter = 40

// runExecuting picks the next approved task and runs it.
func (a *Agent) runExecuting(ctx context.Context) error {
	plan, err := a.db.GetActivePlan(ctx)
	if err != nil {
		a.send("⚠️ No active plan found. Triggering replan...")
		a.SetState(ctx, StateReplanning)
		return nil
	}

	task, err := a.db.GetNextPendingTask(ctx, plan.ID)
	if err != nil {
		// Plan exhausted
		a.send("🏁 All tasks in this plan are complete! Running replan scan...")
		_ = a.db.ExhaustPlan(ctx, plan.ID)
		a.SetState(ctx, StateReplanning)
		return nil
	}

	return a.executeTask(ctx, task)
}

// executeTask runs a single top-level task through its subtasks.
func (a *Agent) executeTask(ctx context.Context, task *db.Task) error {
	a.send(fmt.Sprintf("🔨 Starting: *%s* (Phase %d)", task.Title, task.Phase))
	_ = a.db.UpdateTaskStatus(ctx, task.ID, "in_progress")
	_ = a.db.LogEvent(ctx, task.ID, "task_start", task.Title)
	_ = a.db.KVSet(ctx, "current_task_id", fmt.Sprintf("%d", task.ID))

	// Make sure repo is cloned
	if err := a.ensureRepo(ctx, task.ID); err != nil {
		return err
	}

	// Create feature branch
	branch := fmt.Sprintf("feature/task-%d-%s", task.ID, slugify(task.Title))
	branchResult := a.exec.GithubOp(ctx, "create_branch", branch)
	if branchResult.IsErr {
		// Branch might exist — try checkout
		a.exec.GithubOp(ctx, "checkout_branch", branch)
	}
	_ = a.db.KVSet(ctx, "current_branch", branch)

	// Get subtasks
	subtasks, err := a.db.GetSubtasks(ctx, task.ID)
	if err != nil {
		return fmt.Errorf("get subtasks: %w", err)
	}

	// Run agentic loop for this task
	if err := a.runAgentLoop(ctx, task, subtasks, branch); err != nil {
		_ = a.db.UpdateTaskStatus(ctx, task.ID, "failed")
		_ = a.db.LogEvent(ctx, task.ID, "error", err.Error())
		a.send(fmt.Sprintf("❌ Task failed: %s\n\nError: %v", task.Title, err))
		return nil // Don't bubble up — continue to next task
	}

	return nil
}

// runAgentLoop is the core LLM + tool execution loop for a task.
func (a *Agent) runAgentLoop(ctx context.Context, task *db.Task, subtasks []db.Task, branch string) error {
	// Build rich context
	agentCtx, err := a.BuildContext(ctx, task.Title+" "+task.Description)
	if err != nil {
		agentCtx = ""
	}

	// Build subtask list for prompt
	var subtaskList strings.Builder
	for i, s := range subtasks {
		subtaskList.WriteString(fmt.Sprintf("%d. %s\n", i+1, s.Title))
	}

	taskPrompt := fmt.Sprintf(`TASK: %s

DESCRIPTION: %s

SUBTASKS TO COMPLETE:
%s

BRANCH: %s

INSTRUCTIONS:
1. Start with search_docs to load relevant documentation
2. Check repo status with github_op(status)
3. Implement each subtask in order
4. After every .go file: run go build ./...
5. After all code is written: run go test ./... -cover
6. Ensure test coverage ≥ 85%%
7. Commit and create a PR when everything passes
8. Mark task as done with update_task_status

CONTEXT:
%s`,
		task.Title, task.Description, subtaskList.String(), branch, agentCtx)

	messages := []llm.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: taskPrompt},
	}

	toolDefs := tools.Definitions()
	buildFailures := 0
	testPassed := false

	for i := 0; i < maxAgentIter; i++ {
		if a.IsPaused(ctx) {
			_ = a.db.KVSet(ctx, "checkpoint_iter", fmt.Sprintf("%d", i))
			return fmt.Errorf("paused by founder")
		}

		resp, err := a.llm.Chat(ctx, messages, toolDefs)
		if err != nil {
			return fmt.Errorf("llm: %w", err)
		}

		choice := resp.Choices[0]
		msg := choice.Message
		messages = append(messages, msg)

		// Send text updates to founder
		if msg.Content != "" {
			a.send(fmt.Sprintf("💬 %s", msg.Content))
			_ = a.db.LogEvent(ctx, task.ID, "llm_msg", truncateStr(msg.Content, 200))
		}

		// Done?
		if choice.FinishReason == "stop" || len(msg.ToolCalls) == 0 {
			break
		}

		// Execute each tool call
		for _, tc := range msg.ToolCalls {
			toolResult, handled := a.handleToolCall(ctx, task, tc, &buildFailures, &testPassed, branch)

			if !handled {
				// Should not happen
				toolResult = "unhandled tool: " + tc.Function.Name
			}

			messages = append(messages, llm.Message{
				Role:       "tool",
				ToolCallID: tc.ID,
				Content:    toolResult,
			})

			// Build failure: inject warning and check retry limit
			if tc.Function.Name == "write_file" && buildFailures >= maxBuildRetries {
				return fmt.Errorf("build failed %d times — stopping task", maxBuildRetries)
			}
		}
	}

	// Final check — if test never ran, run it now
	if !testPassed {
		a.send("🧪 Running final test check...")
		result := a.exec.Shell(ctx, "go test ./... -cover 2>&1", 120)
		_ = a.db.LogEvent(ctx, task.ID, "test_result", truncateStr(result.Output, 300))
		if result.IsErr {
			a.send(fmt.Sprintf("⚠️ Tests have issues:\n```\n%s\n```", truncateStr(result.Output, 800)))
		} else {
			testPassed = true
			a.send("✅ Tests pass!")
		}
	}

	_ = a.db.UpdateTaskStatus(ctx, task.ID, "done")
	_ = a.db.LogEvent(ctx, task.ID, "task_done", task.Title)
	a.send(fmt.Sprintf("✅ Task complete: *%s*", task.Title))

	return nil
}

// handleToolCall executes one tool call and returns the result string.
func (a *Agent) handleToolCall(ctx context.Context, task *db.Task, tc llm.ToolCall,
	buildFailures *int, testPassed *bool, branch string) (string, bool) {

	log.Printf("[tool] %s: %s", tc.Function.Name, truncateStr(tc.Function.Arguments, 120))
	_ = a.db.LogEvent(ctx, task.ID, "tool_call",
		fmt.Sprintf("%s: %s", tc.Function.Name, truncateStr(tc.Function.Arguments, 100)))

	switch tc.Function.Name {

	case "ask_founder":
		args := parseToolArgs(tc.Function.Arguments)
		question := args["question"]
		answer := a.ask("❓ " + question)
		if answer == "" {
			answer = "No answer provided. Use best judgment."
		}
		return "Founder answered: " + answer, true

	case "search_docs":
		args := parseToolArgs(tc.Function.Arguments)
		tagsStr := args["tags"]
		limitStr := args["limit"]
		limit := 5
		fmt.Sscanf(limitStr, "%d", &limit)
		if limit <= 0 {
			limit = 5
		}
		tags := strings.Split(tagsStr, ",")
		chunks, err := a.db.SearchDocChunks(ctx, tags, limit)
		if err != nil || len(chunks) == 0 {
			return "No matching documentation found.", true
		}
		var sb strings.Builder
		for _, c := range chunks {
			sb.WriteString(fmt.Sprintf("[%s]\n%s\n\n", c.Relevance, c.ChunkText))
		}
		return sb.String(), true

	case "update_task_status":
		args := parseToolArgs(tc.Function.Arguments)
		var taskID int
		fmt.Sscanf(args["task_id"], "%d", &taskID)
		status := args["status"]
		if taskID > 0 && status != "" {
			_ = a.db.UpdateTaskStatus(ctx, taskID, status)
		}
		return fmt.Sprintf("Task %d status → %s", taskID, status), true

	case "log_event":
		args := parseToolArgs(tc.Function.Arguments)
		var taskID int
		fmt.Sscanf(args["task_id"], "%d", &taskID)
		_ = a.db.LogEvent(ctx, taskID, args["event"], args["payload"])
		return "logged", true

	case "write_file":
		result := a.exec.Run(ctx, tc.Function.Name, tc.Function.Arguments)
		output := result.String()

		// After writing a .go file — run build check
		args := parseToolArgs(tc.Function.Arguments)
		if strings.HasSuffix(strings.TrimSpace(args["path"]), ".go") {
			buildResult := a.exec.Shell(ctx, "go build ./... 2>&1", 60)
			_ = a.db.LogEvent(ctx, task.ID, "build_result", truncateStr(buildResult.Output, 200))

			if buildResult.IsErr || strings.Contains(buildResult.Output, "Error") {
				*buildFailures++
				a.send(fmt.Sprintf("🔨 Build check %d/%d failed", *buildFailures, maxBuildRetries))
				output += fmt.Sprintf("\n\nBUILD FAILED (%d/%d):\n%s\nFix this before continuing.",
					*buildFailures, maxBuildRetries, buildResult.Output)
			} else {
				*buildFailures = 0
				output += "\n\n✅ Build passes."
				a.send(fmt.Sprintf("🔨 Build ✅ — %s", args["path"]))
			}
		}
		return output, true

	case "shell":
		result := a.exec.Run(ctx, tc.Function.Name, tc.Function.Arguments)
		output := result.String()

		// Detect test run
		if strings.Contains(tc.Function.Arguments, "go test") {
			_ = a.db.LogEvent(ctx, task.ID, "test_result", truncateStr(output, 300))
			if !result.IsErr {
				*testPassed = true
				coverage := extractCoverage(output)
				a.send(fmt.Sprintf("🧪 Tests pass! Coverage: %s", coverage))
			} else {
				a.send(fmt.Sprintf("⚠️ Test issues:\n```\n%s\n```", truncateStr(output, 600)))
			}
		}
		return output, true

	default:
		result := a.exec.Run(ctx, tc.Function.Name, tc.Function.Arguments)
		return result.String(), true
	}
}

// ensureRepo clones or pulls the repo.
func (a *Agent) ensureRepo(ctx context.Context, taskID int) error {
	result := a.exec.GithubOp(ctx, "clone", "")
	if result.IsErr {
		_ = a.db.LogEvent(ctx, taskID, "error", "repo clone/pull: "+result.Output)
		return fmt.Errorf("repo setup: %s", result.Output)
	}
	_ = a.db.LogEvent(ctx, taskID, "note", "repo ready")
	return nil
}

// extractCoverage pulls coverage % from go test output.
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

// slugify makes a safe branch name segment from a title.
func slugify(s string) string {
	s = strings.ToLower(s)
	s = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			return r
		}
		return '-'
	}, s)
	// Collapse multiple dashes
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	s = strings.Trim(s, "-")
	if len(s) > 30 {
		s = s[:30]
	}
	return s
}

// parseToolArgs parses a JSON args string into a string map.
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
