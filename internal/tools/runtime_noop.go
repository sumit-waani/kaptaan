package tools

import "context"

// NoopRuntime is used by the Manager — which plans, does not execute. Shell
// and file ops fail explicitly; the Executor still routes merge_pr through
// the GitHub REST API.
type NoopRuntime struct{}

func (NoopRuntime) Shell(_ context.Context, _ string, _ int) Result {
	return Result{Output: "shell is only available inside the Builder sandbox", IsErr: true}
}

func (NoopRuntime) WriteFile(_ context.Context, _ string, _ []byte) Result {
	return Result{Output: "write_file is only available inside the Builder sandbox", IsErr: true}
}

func (NoopRuntime) ReadFile(_ context.Context, _ string) Result {
	return Result{Output: "read_file is only available inside the Builder sandbox", IsErr: true}
}

func (NoopRuntime) Workdir() string { return "" }

func (NoopRuntime) Close(_ context.Context) error { return nil }
