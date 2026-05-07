// Package tools defines the small interfaces shared between the agent and
// the sandbox runtime. The agent never imports the sandbox directly; it goes
// through Runtime so unit tests can swap in a fake.
package tools

import (
	"context"
	"strings"
)

// Result is the outcome of one tool invocation.
type Result struct {
	Output string
	IsErr  bool
}

// Runtime is the tiny surface the agent needs from a workspace (E2B sandbox).
type Runtime interface {
	Shell(ctx context.Context, cmd string, timeoutSecs int) Result
	WriteFile(ctx context.Context, path string, data []byte) Result
	ReadFile(ctx context.Context, path string) Result
	Workdir() string
	Close(ctx context.Context) error
}

// capOutput truncates a string to maxBytes, appending a marker if cut.
func capOutput(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	cut := s[:maxBytes]
	if i := strings.LastIndexByte(cut, '\n'); i > maxBytes-200 {
		cut = cut[:i]
	}
	return cut + "\n…[output truncated]"
}
