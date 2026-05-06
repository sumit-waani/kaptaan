package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/cto-agent/cto-agent/internal/llm"
)

// generateClarifyingQuestions asks the LLM what it still needs to know.
func (a *Agent) generateClarifyingQuestions(ctx context.Context) error {
	docCtx, _ := a.BuildDocContext(ctx, "feature api schema rule", 8)
	bd, _ := a.CalculateTrustScore(ctx)

	existing, _ := a.db.GetAllClarifications(ctx)
	var existingQs []string
	for _, c := range existing {
		existingQs = append(existingQs, c.Question)
	}

	prompt := fmt.Sprintf(`You are a senior CTO reviewing project docs before starting development.

%s

Current trust score: %.1f%%
Already asked questions:
%s

Based on the documentation above, identify the 1-3 most critical questions that, if answered, would significantly increase confidence to build this project end-to-end.

Only ask questions that:
- Are NOT already answered in the docs
- Are NOT already in the "already asked" list
- Would meaningfully clarify implementation decisions
- Cannot be reasonably inferred from the docs

If the docs are comprehensive enough and you have no blocking questions, return an empty list.

Respond ONLY with valid JSON:
{
  "questions": [
    "specific question 1",
    "specific question 2"
  ]
}`, docCtx, bd.Total, strings.Join(existingQs, "\n"))

	resp, err := a.llm.ChatJSON(ctx, []llm.Message{
		{Role: "user", Content: prompt},
	})
	if err != nil {
		return fmt.Errorf("generate questions: %w", err)
	}

	text := cleanJSON(resp.Choices[0].Message.Content)
	var result struct {
		Questions []string `json:"questions"`
	}
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		log.Printf("[planner] failed to parse questions: %v", err)
		return nil
	}

	if len(result.Questions) == 0 {
		log.Printf("[planner] no new questions — advancing trust check")
		return nil
	}

	for _, q := range result.Questions {
		if q == "" {
			continue
		}
		if _, err := a.db.CreateClarification(ctx, q); err != nil {
			return fmt.Errorf("save clarification: %w", err)
		}
	}

	return nil
}

// runPlanning generates and presents the full task plan to the founder.
func (a *Agent) runPlanning(ctx context.Context) error {
	a.send("📋 Generating implementation plan...")

	plan, err := a.generatePlan(ctx)
	if err != nil {
		return fmt.Errorf("generate plan: %w", err)
	}

	// Create plan in DB
	dbPlan, err := a.db.CreatePlan(ctx)
	if err != nil {
		return fmt.Errorf("create plan: %w", err)
	}

	// Present each top-level task one by one for approval
	for _, phase := range plan.Phases {
		for _, task := range phase.Tasks {
			approved, err := a.presentTaskForApproval(ctx, dbPlan.ID, phase.Number, task)
			if err != nil {
				return err
			}
			if !approved {
				a.send(fmt.Sprintf("⏭ Skipping: %s", task.Title))
			}
		}
	}

	a.send("✅ Plan approved! Starting implementation...")
	a.SetState(ctx, StateExecuting)
	return nil
}

// PlanPhase is one phase of the implementation plan.
type PlanPhase struct {
	Number int
	Title  string
	Tasks  []PlannedTask
}

// PlannedTask is a top-level task with subtasks.
type PlannedTask struct {
	Title       string
	Description string
	Subtasks    []string
}

// FullPlan is the complete generated plan.
type FullPlan struct {
	Phases []PlanPhase
}

func (a *Agent) generatePlan(ctx context.Context) (*FullPlan, error) {
	docCtx, _ := a.BuildDocContext(ctx, "feature api schema rule ui data", 10)
	repoInfo, _ := a.ScanRepo(ctx)
	clarifs, _ := a.db.GetAllClarifications(ctx)

	var clarifications strings.Builder
	for _, c := range clarifs {
		if c.Answer != "" {
			clarifications.WriteString(fmt.Sprintf("Q: %s\nA: %s\n\n", c.Question, c.Answer))
		}
	}

	prompt := fmt.Sprintf(`You are a senior CTO creating an implementation plan for a Go backend project.

DOCUMENTATION:
%s

REPOSITORY CURRENT STATE:
%s

FOUNDER CLARIFICATIONS:
%s

Create a phased implementation plan. Each phase should be independently deployable.

Rules:
- Each task maps to one PR
- Include tests as explicit subtasks
- Stay strictly within what the documentation specifies
- Order by dependencies (infra → data → core → api → ui)
- 3-6 phases total, 1-3 tasks per phase, 2-5 subtasks per task

Respond ONLY with valid JSON:
{
  "phases": [
    {
      "number": 1,
      "title": "Phase title",
      "tasks": [
        {
          "title": "Task title",
          "description": "What this task accomplishes and why",
          "subtasks": [
            "Write X",
            "Write tests for X",
            "Wire X into Y"
          ]
        }
      ]
    }
  ]
}`,
		docCtx, repoInfo, clarifications.String())

	resp, err := a.llm.ChatJSON(ctx, []llm.Message{
		{Role: "user", Content: prompt},
	})
	if err != nil {
		return nil, err
	}

	text := cleanJSON(resp.Choices[0].Message.Content)

	var raw struct {
		Phases []struct {
			Number int    `json:"number"`
			Title  string `json:"title"`
			Tasks  []struct {
				Title       string   `json:"title"`
				Description string   `json:"description"`
				Subtasks    []string `json:"subtasks"`
			} `json:"tasks"`
		} `json:"phases"`
	}

	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		return nil, fmt.Errorf("parse plan: %w", err)
	}

	plan := &FullPlan{}
	for _, ph := range raw.Phases {
		phase := PlanPhase{Number: ph.Number, Title: ph.Title}
		for _, t := range ph.Tasks {
			task := PlannedTask{
				Title:       t.Title,
				Description: t.Description,
				Subtasks:    t.Subtasks,
			}
			phase.Tasks = append(phase.Tasks, task)
		}
		plan.Phases = append(plan.Phases, phase)
	}

	return plan, nil
}

// presentTaskForApproval shows a task plan to the founder and waits for yes/no.
func (a *Agent) presentTaskForApproval(ctx context.Context, planID, phase int, task PlannedTask) (bool, error) {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📋 *Phase %d — %s*\n\n", phase, task.Title))
	sb.WriteString(task.Description)
	sb.WriteString("\n\nSubtasks:\n")
	for i, s := range task.Subtasks {
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, s))
	}
	sb.WriteString("\nApprove? (yes / no / skip)")

	answer := a.ask(sb.String())
	answer = strings.ToLower(strings.TrimSpace(answer))

	approved := answer == "yes" || answer == "y" || answer == "approve" || answer == "ok"

	// Save task to DB regardless — status depends on approval
	status := "pending"
	if approved {
		status = "approved"
	} else {
		status = "skipped"
	}

	dbTask, err := a.db.CreateTask(ctx, planID, nil, phase, task.Title, task.Description, false)
	if err != nil {
		return false, err
	}
	_ = a.db.UpdateTaskStatus(ctx, dbTask.ID, status)

	// Save subtasks
	if approved {
		for _, s := range task.Subtasks {
			sub, err := a.db.CreateTask(ctx, planID, &dbTask.ID, phase, s, "", false)
			if err != nil {
				return false, err
			}
			_ = a.db.UpdateTaskStatus(ctx, sub.ID, "pending")
		}
	}

	return approved, nil
}

// runReplanning scans repo, generates suggestions, presents them for approval.
func (a *Agent) runReplanning(ctx context.Context) error {
	a.send("🔍 Scanning codebase for gaps, bugs, and missing features...")

	scanOut, _ := a.ScanRepo(ctx)
	docCtx, _ := a.BuildDocContext(ctx, "feature rule api", 8)

	// Get completed tasks for context
	plan, _ := a.db.GetActivePlan(ctx)
	var doneWork strings.Builder
	if plan != nil {
		tasks, _ := a.db.GetTasksByPlan(ctx, plan.ID)
		for _, t := range tasks {
			if t.Status == "done" {
				doneWork.WriteString(fmt.Sprintf("- %s\n", t.Title))
			}
		}
	}

	prompt := fmt.Sprintf(`You are a senior CTO doing a code review after an implementation cycle.

PROJECT DOCS:
%s

REPO STATE:
%s

COMPLETED WORK:
%s

Analyze the codebase against the documentation. Identify:
1. Features in docs that are missing or incomplete
2. Known bugs or code quality issues
3. Performance or security improvements
4. Technical debt worth addressing

For each finding, propose a concrete implementation task.

Respond ONLY with valid JSON:
{
  "suggestions": [
    {
      "title": "Short title",
      "description": "What's wrong / missing and why it matters",
      "task_plan": "Step-by-step implementation plan"
    }
  ]
}`, docCtx, scanOut, doneWork.String())

	resp, err := a.llm.ChatJSON(ctx, []llm.Message{
		{Role: "user", Content: prompt},
	})
	if err != nil {
		return fmt.Errorf("replan llm: %w", err)
	}

	text := cleanJSON(resp.Choices[0].Message.Content)
	var raw struct {
		Suggestions []struct {
			Title       string `json:"title"`
			Description string `json:"description"`
			TaskPlan    string `json:"task_plan"`
		} `json:"suggestions"`
	}

	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		return fmt.Errorf("parse suggestions: %w", err)
	}

	if len(raw.Suggestions) == 0 {
		a.send("✅ Codebase looks good! No major gaps found.")
		a.SetState(ctx, StateDone)
		return nil
	}

	a.send(fmt.Sprintf("🔎 Found %d improvement suggestions. Reviewing one by one...", len(raw.Suggestions)))

	// Save all suggestions to DB
	for _, s := range raw.Suggestions {
		_, err := a.db.CreateSuggestion(ctx, s.Title, s.Description, s.TaskPlan)
		if err != nil {
			log.Printf("[planner] save suggestion: %v", err)
		}
	}

	// Present pending suggestions one by one
	for {
		sug, err := a.db.GetPendingSuggestion(ctx)
		if err != nil {
			break // no more pending
		}

		msg := fmt.Sprintf("💡 Suggestion:\n\n*%s*\n\n%s\n\nPlan:\n%s\n\nImplement? (yes / no)",
			sug.Title, sug.Description, sug.TaskPlan)

		answer := a.ask(msg)
		answer = strings.ToLower(strings.TrimSpace(answer))
		approved := answer == "yes" || answer == "y"

		status := "rejected"
		if approved {
			status = "approved"
		}
		_ = a.db.UpdateSuggestionStatus(ctx, sug.ID, status)

		if approved {
			// Create as new task in current/new plan
			activePlan, err := a.db.GetActivePlan(ctx)
			if err != nil {
				activePlan, _ = a.db.CreatePlan(ctx)
			}
			newTask, err := a.db.CreateTask(ctx, activePlan.ID, nil, 99, sug.Title, sug.Description, true)
			if err == nil {
				_ = a.db.UpdateTaskStatus(ctx, newTask.ID, "approved")
				// Parse subtasks from task_plan text (line by line)
				for _, line := range strings.Split(sug.TaskPlan, "\n") {
					line = strings.TrimSpace(line)
					line = strings.TrimLeft(line, "-•0123456789. ")
					if line != "" {
						sub, err := a.db.CreateTask(ctx, activePlan.ID, &newTask.ID, 99, line, "", true)
						if err == nil {
							_ = a.db.UpdateTaskStatus(ctx, sub.ID, "pending")
						}
					}
				}
			}
		}
	}

	a.SetState(ctx, StateExecuting)
	return nil
}

// cleanJSON strips markdown fences from LLM JSON responses.
func cleanJSON(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}
