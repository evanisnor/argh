package ghcli

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// GHCLIDiffFetcher implements diff.DiffFetcher using `gh pr diff`.
// The token parameter is ignored — gh manages its own credentials.
type GHCLIDiffFetcher struct {
	Runner CommandRunner
}

// Fetch retrieves the diff for a PR by parsing the URL to extract owner/repo and number,
// then running `gh pr diff`. The token parameter is ignored.
func (f *GHCLIDiffFetcher) Fetch(prURL, _ string) ([]byte, error) {
	repo, number, err := parsePRURL(prURL)
	if err != nil {
		return nil, err
	}

	out, err := f.Runner.Run(context.Background(), []string{
		"pr", "diff", strconv.Itoa(number), "-R", repo, "--color", "never",
	})
	if err != nil {
		return nil, fmt.Errorf("fetching diff for %s#%d: %w", repo, number, err)
	}

	return out, nil
}

// parsePRURL extracts "owner/repo" and the PR number from a GitHub PR URL.
// Expected format: https://github.com/owner/repo/pull/123
// or the API URL: https://api.github.com/repos/owner/repo/pulls/123
func parsePRURL(rawURL string) (repo string, number int, err error) {
	u, parseErr := url.Parse(rawURL)
	if parseErr != nil {
		return "", 0, fmt.Errorf("parsing PR URL %q: %w", rawURL, parseErr)
	}

	parts := strings.Split(strings.Trim(u.Path, "/"), "/")

	// github.com/owner/repo/pull/123
	if len(parts) >= 4 && (parts[2] == "pull" || parts[2] == "pulls") {
		n, err := strconv.Atoi(parts[3])
		if err != nil {
			return "", 0, fmt.Errorf("invalid PR number in URL %q: %w", rawURL, err)
		}
		return parts[0] + "/" + parts[1], n, nil
	}

	// api.github.com/repos/owner/repo/pulls/123
	if len(parts) >= 5 && parts[0] == "repos" && (parts[3] == "pulls" || parts[3] == "pull") {
		n, err := strconv.Atoi(parts[4])
		if err != nil {
			return "", 0, fmt.Errorf("invalid PR number in URL %q: %w", rawURL, err)
		}
		return parts[1] + "/" + parts[2], n, nil
	}

	return "", 0, fmt.Errorf("cannot extract repo and PR number from URL %q", rawURL)
}
