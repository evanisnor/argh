// Package suggest provides ranked reviewer suggestion logic combining up to
// three optional signals: git blame history, recent PR reviewers, and
// CODEOWNERS rules.
package suggest

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// ── Aggregator interfaces ─────────────────────────────────────────────────────

// BlameProvider suggests reviewers based on git blame history for the files
// changed in a pull request.
type BlameProvider interface {
	ContributorsForPR(ctx context.Context, repo string, number int) ([]string, error)
}

// RecentReviewerProvider suggests reviewers from recent PR review history in
// the same repository.
type RecentReviewerProvider interface {
	RecentReviewers(ctx context.Context, repo string) ([]string, error)
}

// CodeownersProvider suggests reviewers based on CODEOWNERS rules matching
// the files changed in a pull request.
type CodeownersProvider interface {
	OwnersForPR(ctx context.Context, repo string, number int) ([]string, error)
}

// ── Suggester ─────────────────────────────────────────────────────────────────

// Suggester combines up to three optional signals to produce a ranked,
// de-duplicated list of reviewer suggestions for a pull request.
// Any nil signal is silently skipped (graceful degradation).
type Suggester struct {
	Blame      BlameProvider
	Recent     RecentReviewerProvider
	Codeowners CodeownersProvider
}

// SuggestReviewers returns a ranked, de-duplicated list of reviewer logins.
// Signals are queried in priority order: Blame > Recent > Codeowners.
// Errors from individual signals are silently ignored so unavailable signals
// degrade gracefully. An empty list is returned when no signals produce results.
func (s *Suggester) SuggestReviewers(ctx context.Context, repo string, number int) ([]string, error) {
	seen := make(map[string]bool)
	var result []string

	add := func(logins []string) {
		for _, l := range logins {
			l = strings.TrimSpace(l)
			if l != "" && !seen[l] {
				result = append(result, l)
				seen[l] = true
			}
		}
	}

	if s.Blame != nil {
		logins, _ := s.Blame.ContributorsForPR(ctx, repo, number)
		add(logins)
	}
	if s.Recent != nil {
		logins, _ := s.Recent.RecentReviewers(ctx, repo)
		add(logins)
	}
	if s.Codeowners != nil {
		logins, _ := s.Codeowners.OwnersForPR(ctx, repo, number)
		add(logins)
	}

	return result, nil
}

// ── Git blame signal ──────────────────────────────────────────────────────────

// PRFileLister lists the file paths changed in a pull request.
type PRFileLister interface {
	ListChangedFiles(ctx context.Context, repo string, number int) ([]string, error)
}

// CommandRunner runs an external command and returns its combined stdout.
type CommandRunner interface {
	Output(ctx context.Context, name string, args ...string) ([]byte, error)
}

// GitBlameProvider implements BlameProvider using local git log history.
// WorkDir should be the path to the local git repository.
// If git fails for a file (e.g. file not checked out locally), that file is
// silently skipped.
type GitBlameProvider struct {
	Files   PRFileLister
	Runner  CommandRunner
	WorkDir string
}

// ContributorsForPR returns git committer names for the changed files, ranked
// by commit frequency. These are best-effort suggestions: they may not match
// GitHub logins exactly but serve as useful hints.
func (g *GitBlameProvider) ContributorsForPR(ctx context.Context, repo string, number int) ([]string, error) {
	files, err := g.Files.ListChangedFiles(ctx, repo, number)
	if err != nil {
		return nil, fmt.Errorf("listing changed files: %w", err)
	}

	counts := make(map[string]int)
	for _, f := range files {
		out, err := g.Runner.Output(ctx, "git", "-C", g.WorkDir, "log", "--format=%an", "--", f)
		if err != nil {
			continue
		}
		for _, name := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			name = strings.TrimSpace(name)
			if name != "" {
				counts[name]++
			}
		}
	}

	return rankByCount(counts), nil
}

// rankByCount returns the keys of counts sorted by descending value, with
// ties broken alphabetically.
func rankByCount(counts map[string]int) []string {
	type kv struct {
		key   string
		count int
	}
	pairs := make([]kv, 0, len(counts))
	for k, v := range counts {
		pairs = append(pairs, kv{k, v})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].count != pairs[j].count {
			return pairs[i].count > pairs[j].count
		}
		return pairs[i].key < pairs[j].key
	})
	result := make([]string, len(pairs))
	for i, p := range pairs {
		result[i] = p.key
	}
	return result
}

// ── Recent reviewer signal ────────────────────────────────────────────────────

// ReviewersByRepoStore retrieves distinct reviewer logins from past pull
// requests in a given repository.
type ReviewersByRepoStore interface {
	ListReviewersByRepo(repo string) ([]string, error)
}

// DBRecentReviewerProvider implements RecentReviewerProvider by querying the
// local SQLite cache of reviewer history.
type DBRecentReviewerProvider struct {
	Store ReviewersByRepoStore
}

// RecentReviewers returns reviewer logins from past PRs in the same repo.
func (d *DBRecentReviewerProvider) RecentReviewers(_ context.Context, repo string) ([]string, error) {
	return d.Store.ListReviewersByRepo(repo)
}

// ── CODEOWNERS signal ─────────────────────────────────────────────────────────

// CodeownersFileFetcher fetches raw file content from a repository by path.
type CodeownersFileFetcher interface {
	FetchFileContent(ctx context.Context, repo, path string) (string, error)
}

// GitHubCodeownersProvider implements CodeownersProvider by parsing the
// repository's .github/CODEOWNERS file via the GitHub API.
type GitHubCodeownersProvider struct {
	Files   PRFileLister
	Fetcher CodeownersFileFetcher
}

// OwnersForPR returns reviewer logins derived from CODEOWNERS rules that match
// files changed in the pull request. If the CODEOWNERS file does not exist,
// an empty list is returned gracefully.
func (c *GitHubCodeownersProvider) OwnersForPR(ctx context.Context, repo string, number int) ([]string, error) {
	content, err := c.Fetcher.FetchFileContent(ctx, repo, ".github/CODEOWNERS")
	if err != nil {
		// CODEOWNERS file may not exist — degrade gracefully.
		return nil, nil
	}

	files, err := c.Files.ListChangedFiles(ctx, repo, number)
	if err != nil {
		return nil, fmt.Errorf("listing changed files: %w", err)
	}

	rules := parseCodeowners(content)
	seen := make(map[string]bool)
	var owners []string
	for _, f := range files {
		for _, o := range matchOwners(rules, f) {
			if !seen[o] {
				owners = append(owners, o)
				seen[o] = true
			}
		}
	}
	return owners, nil
}

// ── CODEOWNERS parser ─────────────────────────────────────────────────────────

// codeownersRule maps a glob pattern to a list of owner logins.
type codeownersRule struct {
	pattern string
	owners  []string
}

// parseCodeowners parses CODEOWNERS content and returns its rules in order.
// Lines starting with '#' or empty lines are ignored. Owner tokens are
// stripped of their leading '@'.
func parseCodeowners(content string) []codeownersRule {
	var rules []codeownersRule
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		pattern := fields[0]
		var owners []string
		for _, o := range fields[1:] {
			login := strings.TrimPrefix(o, "@")
			if login != "" {
				owners = append(owners, login)
			}
		}
		if len(owners) > 0 {
			rules = append(rules, codeownersRule{pattern: pattern, owners: owners})
		}
	}
	return rules
}

// matchOwners returns the owners from the last matching rule for filePath.
// Later rules in CODEOWNERS take precedence (gitignore-style last-match wins).
func matchOwners(rules []codeownersRule, filePath string) []string {
	var matched []string
	for _, rule := range rules {
		if globMatch(rule.pattern, filePath) {
			matched = rule.owners
		}
	}
	return matched
}

// globMatch reports whether the CODEOWNERS pattern matches filePath.
// Handles the most common CODEOWNERS patterns:
//   - No '/' in pattern: matches any path component (basename match)
//   - Leading '/': anchors to repository root
//   - Trailing '/': matches directory and all contents
//   - '*' and '?' glob wildcards via filepath.Match
func globMatch(pattern, filePath string) bool {
	if !strings.Contains(pattern, "/") {
		// No slash — match against any component of the path.
		base := filepath.Base(filePath)
		if ok, _ := filepath.Match(pattern, base); ok {
			return true
		}
		for _, part := range strings.Split(filePath, "/") {
			if ok, _ := filepath.Match(pattern, part); ok {
				return true
			}
		}
		return false
	}

	// Strip leading slash for rooted patterns.
	p := strings.TrimPrefix(pattern, "/")

	// Directory pattern (trailing slash) — match prefix.
	if strings.HasSuffix(p, "/") {
		return strings.HasPrefix(filePath, p)
	}

	// Standard glob match against full path.
	if ok, _ := filepath.Match(p, filePath); ok {
		return true
	}
	// Also try matching as a directory prefix (pattern is a directory).
	if strings.HasPrefix(filePath, p+"/") {
		return true
	}
	return false
}
