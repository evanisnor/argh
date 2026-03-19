package api

import (
	"context"
	"os/exec"
)

// OSCommandExecutor is the real implementation of CommandExecutor that runs
// actual system commands.
type OSCommandExecutor struct{}

// Output runs the named command with the given arguments and returns stdout.
// Returns an error if the command is not found or exits with a non-zero status.
func (e *OSCommandExecutor) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}
