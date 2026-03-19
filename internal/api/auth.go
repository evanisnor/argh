package api

import (
	"context"
	"fmt"
	"strings"
)

// CommandExecutor runs a command and returns its stdout output.
// The command is specified as a slice where the first element is the program
// and subsequent elements are arguments.
type CommandExecutor interface {
	Output(ctx context.Context, name string, args ...string) ([]byte, error)
}

// Credentials holds the authenticated user's token and login.
type Credentials struct {
	Token string
	Login string
}

// Authenticate retrieves the GitHub token and login via the gh CLI.
// It returns an error if gh is not installed, not authenticated, or returns
// an empty token.
func Authenticate(ctx context.Context, exec CommandExecutor) (*Credentials, error) {
	tokenBytes, err := exec.Output(ctx, "gh", "auth", "token")
	if err != nil {
		return nil, fmt.Errorf("gh auth token: %w", err)
	}

	token := strings.TrimSpace(string(tokenBytes))
	if token == "" {
		return nil, fmt.Errorf("gh auth token returned empty token: ensure you are logged in with `gh auth login`")
	}

	loginBytes, err := exec.Output(ctx, "gh", "api", "user", "--jq", ".login")
	if err != nil {
		return nil, fmt.Errorf("gh api user: %w", err)
	}

	login := strings.TrimSpace(string(loginBytes))

	return &Credentials{
		Token: token,
		Login: login,
	}, nil
}
