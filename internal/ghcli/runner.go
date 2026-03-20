package ghcli

import (
	"context"
	"fmt"
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
	cmd := exec.CommandContext(ctx, "gh", args...)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("gh %v failed: %s", args, string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("gh %v: %w", args, err)
	}
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
