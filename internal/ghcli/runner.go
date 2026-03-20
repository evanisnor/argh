package ghcli

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
)

// CommandRunner executes gh CLI commands and returns their stdout.
type CommandRunner interface {
	Run(ctx context.Context, args []string) ([]byte, error)
}

// ExecRunner is the production CommandRunner that shells out to the gh binary.
type ExecRunner struct{}

// Run executes gh with the given arguments and returns combined stdout.
func (r *ExecRunner) Run(ctx context.Context, args []string) ([]byte, error) {
	slog.Debug("ghcli: running command", "args", args)
	cmd := exec.CommandContext(ctx, "gh", args...)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			slog.Error("ghcli: command failed", "args", args, "stderr", string(exitErr.Stderr))
			return nil, fmt.Errorf("gh %v failed: %s", args, string(exitErr.Stderr))
		}
		slog.Error("ghcli: command error", "args", args, "error", err)
		return nil, fmt.Errorf("gh %v: %w", args, err)
	}
	slog.Debug("ghcli: command completed", "args", args, "bytes", len(out))
	return out, nil
}

// BinaryLookup reports whether an executable is available in PATH.
type BinaryLookup func(name string) (string, error)

// CheckInstalled verifies that the gh CLI is installed and available in PATH.
func CheckInstalled(lookup BinaryLookup) error {
	_, err := lookup("gh")
	if err != nil {
		return fmt.Errorf("gh CLI not found in PATH: %w", err)
	}
	return nil
}
