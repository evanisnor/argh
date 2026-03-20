package ghcli

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

// GHCLIAuthVerifier implements api.TokenVerifier by delegating to `gh auth status`.
// The token parameter is ignored — gh manages its own credential store.
type GHCLIAuthVerifier struct {
	Runner CommandRunner
}

// Verify checks that gh is authenticated and extracts the logged-in username.
// The token parameter is ignored.
func (v *GHCLIAuthVerifier) Verify(ctx context.Context, _ string) (string, error) {
	out, err := v.Runner.Run(ctx, []string{"auth", "status"})
	if err != nil {
		return "", fmt.Errorf("gh auth status failed: %w", err)
	}

	login := parseGHAuthLogin(string(out))
	if login == "" {
		return "", fmt.Errorf("could not parse username from gh auth status output")
	}

	slog.Debug("ghcli: authenticated", "login", login)
	return login, nil
}

// parseGHAuthLogin extracts the username from `gh auth status` output.
// It looks for "Logged in to github.com account <login>" or
// "account <login>" pattern in the output.
func parseGHAuthLogin(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if idx := strings.Index(line, "account "); idx >= 0 {
			rest := line[idx+len("account "):]
			// The username ends at the next space or parenthesis.
			end := strings.IndexAny(rest, " (")
			if end > 0 {
				return rest[:end]
			}
			if rest != "" {
				return rest
			}
		}
	}
	return ""
}
