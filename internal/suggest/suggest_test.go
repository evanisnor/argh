package suggest

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// ── Fakes ─────────────────────────────────────────────────────────────────────

type fakeBlameProvider struct {
	logins []string
	err    error
}

func (f *fakeBlameProvider) ContributorsForPR(_ context.Context, _ string, _ int) ([]string, error) {
	return f.logins, f.err
}

type fakeRecentProvider struct {
	logins []string
	err    error
}

func (f *fakeRecentProvider) RecentReviewers(_ context.Context, _ string) ([]string, error) {
	return f.logins, f.err
}

type fakeCodeownersProvider struct {
	logins []string
	err    error
}

func (f *fakeCodeownersProvider) OwnersForPR(_ context.Context, _ string, _ int) ([]string, error) {
	return f.logins, f.err
}

type fakeFileLister struct {
	files []string
	err   error
}

func (f *fakeFileLister) ListChangedFiles(_ context.Context, _ string, _ int) ([]string, error) {
	return f.files, f.err
}

type fakeCommandRunner struct {
	// outputs maps "arg1 arg2 ..." (joined args after "git") to output bytes.
	outputs map[string][]byte
	errors  map[string]error
}

func newFakeCommandRunner() *fakeCommandRunner {
	return &fakeCommandRunner{
		outputs: make(map[string][]byte),
		errors:  make(map[string]error),
	}
}

func (f *fakeCommandRunner) Output(_ context.Context, _ string, args ...string) ([]byte, error) {
	key := strings.Join(args, " ")
	if err, ok := f.errors[key]; ok {
		return nil, err
	}
	if out, ok := f.outputs[key]; ok {
		return out, nil
	}
	return nil, nil
}

type fakeReviewerStore struct {
	logins []string
	err    error
}

func (f *fakeReviewerStore) ListReviewersByRepo(_ string) ([]string, error) {
	return f.logins, f.err
}

type fakeFileFetcher struct {
	content string
	err     error
}

func (f *fakeFileFetcher) FetchFileContent(_ context.Context, _, _ string) (string, error) {
	return f.content, f.err
}

// sliceEqual reports whether a and b contain the same elements in the same order.
func sliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ── Suggester.SuggestReviewers ────────────────────────────────────────────────

func TestSuggester_AllSignalsNil(t *testing.T) {
	s := &Suggester{}
	got, err := s.SuggestReviewers(context.Background(), "owner/repo", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("want empty list, got %v", got)
	}
}

func TestSuggester_SuggestReviewers(t *testing.T) {
	tests := []struct {
		name       string
		blame      BlameProvider
		recent     RecentReviewerProvider
		codeowners CodeownersProvider
		want       []string
	}{
		{
			name:  "only blame",
			blame: &fakeBlameProvider{logins: []string{"alice", "bob"}},
			want:  []string{"alice", "bob"},
		},
		{
			name:   "only recent",
			recent: &fakeRecentProvider{logins: []string{"carol"}},
			want:   []string{"carol"},
		},
		{
			name:       "only codeowners",
			codeowners: &fakeCodeownersProvider{logins: []string{"dave"}},
			want:       []string{"dave"},
		},
		{
			name:       "all three available, no duplicates",
			blame:      &fakeBlameProvider{logins: []string{"alice"}},
			recent:     &fakeRecentProvider{logins: []string{"bob", "carol"}},
			codeowners: &fakeCodeownersProvider{logins: []string{"dave"}},
			want:       []string{"alice", "bob", "carol", "dave"},
		},
		{
			name:       "deduplication: same login from multiple signals appears once",
			blame:      &fakeBlameProvider{logins: []string{"alice"}},
			recent:     &fakeRecentProvider{logins: []string{"alice", "bob"}},
			codeowners: &fakeCodeownersProvider{logins: []string{"alice", "bob", "carol"}},
			want:       []string{"alice", "bob", "carol"},
		},
		{
			name:       "blame nil, others work",
			recent:     &fakeRecentProvider{logins: []string{"carol"}},
			codeowners: &fakeCodeownersProvider{logins: []string{"dave"}},
			want:       []string{"carol", "dave"},
		},
		{
			name:       "blame errors are swallowed",
			blame:      &fakeBlameProvider{err: errors.New("git error")},
			recent:     &fakeRecentProvider{logins: []string{"carol"}},
			codeowners: nil,
			want:       []string{"carol"},
		},
		{
			name:       "recent errors are swallowed",
			blame:      &fakeBlameProvider{logins: []string{"alice"}},
			recent:     &fakeRecentProvider{err: errors.New("db error")},
			codeowners: &fakeCodeownersProvider{logins: []string{"dave"}},
			want:       []string{"alice", "dave"},
		},
		{
			name:       "codeowners errors are swallowed",
			blame:      &fakeBlameProvider{logins: []string{"alice"}},
			codeowners: &fakeCodeownersProvider{err: errors.New("api error")},
			want:       []string{"alice"},
		},
		{
			name:       "all three error — empty result",
			blame:      &fakeBlameProvider{err: errors.New("e")},
			recent:     &fakeRecentProvider{err: errors.New("e")},
			codeowners: &fakeCodeownersProvider{err: errors.New("e")},
			want:       nil,
		},
		{
			name:  "whitespace logins are stripped and empty strings skipped",
			blame: &fakeBlameProvider{logins: []string{"  alice  ", "  ", ""}},
			want:  []string{"alice"},
		},
		{
			name:       "priority order: blame first",
			blame:      &fakeBlameProvider{logins: []string{"first"}},
			recent:     &fakeRecentProvider{logins: []string{"second"}},
			codeowners: &fakeCodeownersProvider{logins: []string{"third"}},
			want:       []string{"first", "second", "third"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Suggester{
				Blame:      tt.blame,
				Recent:     tt.recent,
				Codeowners: tt.codeowners,
			}
			got, err := s.SuggestReviewers(context.Background(), "owner/repo", 42)
			if err != nil {
				t.Fatalf("SuggestReviewers() error = %v", err)
			}
			if !sliceEqual(got, tt.want) {
				t.Errorf("SuggestReviewers() = %v, want %v", got, tt.want)
			}
		})
	}
}

// ── GitBlameProvider ──────────────────────────────────────────────────────────

func TestGitBlameProvider_ContributorsForPR(t *testing.T) {
	runner := newFakeCommandRunner()
	// git log output for two files
	runner.outputs["-C /repo log --format=%an -- main.go"] = []byte("alice\nbob\nalice\n")
	runner.outputs["-C /repo log --format=%an -- util.go"] = []byte("carol\n")
	runner.errors["-C /repo log --format=%an -- missing.go"] = errors.New("not found")

	p := &GitBlameProvider{
		Files:   &fakeFileLister{files: []string{"main.go", "util.go", "missing.go"}},
		Runner:  runner,
		WorkDir: "/repo",
	}

	got, err := p.ContributorsForPR(context.Background(), "owner/repo", 1)
	if err != nil {
		t.Fatalf("ContributorsForPR() error = %v", err)
	}
	// alice has 2 commits, carol and bob have 1 each; sort: alice, bob, carol
	want := []string{"alice", "bob", "carol"}
	if !sliceEqual(got, want) {
		t.Errorf("ContributorsForPR() = %v, want %v", got, want)
	}
}

func TestGitBlameProvider_FileListerError(t *testing.T) {
	p := &GitBlameProvider{
		Files:   &fakeFileLister{err: errors.New("api down")},
		Runner:  newFakeCommandRunner(),
		WorkDir: "/repo",
	}
	_, err := p.ContributorsForPR(context.Background(), "owner/repo", 1)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestGitBlameProvider_EmptyOutput(t *testing.T) {
	runner := newFakeCommandRunner()
	runner.outputs["-C /repo log --format=%an -- empty.go"] = []byte("\n\n")

	p := &GitBlameProvider{
		Files:   &fakeFileLister{files: []string{"empty.go"}},
		Runner:  runner,
		WorkDir: "/repo",
	}

	got, err := p.ContributorsForPR(context.Background(), "owner/repo", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("want empty result, got %v", got)
	}
}

func TestGitBlameProvider_NoFiles(t *testing.T) {
	p := &GitBlameProvider{
		Files:   &fakeFileLister{files: nil},
		Runner:  newFakeCommandRunner(),
		WorkDir: "/repo",
	}
	got, err := p.ContributorsForPR(context.Background(), "owner/repo", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("want empty result, got %v", got)
	}
}

// ── rankByCount ───────────────────────────────────────────────────────────────

func TestRankByCount(t *testing.T) {
	tests := []struct {
		name   string
		counts map[string]int
		want   []string
	}{
		{
			name:   "empty",
			counts: map[string]int{},
			want:   []string{},
		},
		{
			name:   "single",
			counts: map[string]int{"alice": 3},
			want:   []string{"alice"},
		},
		{
			name:   "sorted by count descending",
			counts: map[string]int{"alice": 1, "bob": 3, "carol": 2},
			want:   []string{"bob", "carol", "alice"},
		},
		{
			name:   "tie broken alphabetically",
			counts: map[string]int{"bob": 2, "alice": 2},
			want:   []string{"alice", "bob"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rankByCount(tt.counts)
			if !sliceEqual(got, tt.want) {
				t.Errorf("rankByCount() = %v, want %v", got, tt.want)
			}
		})
	}
}

// ── DBRecentReviewerProvider ──────────────────────────────────────────────────

func TestDBRecentReviewerProvider_RecentReviewers(t *testing.T) {
	p := &DBRecentReviewerProvider{
		Store: &fakeReviewerStore{logins: []string{"alice", "bob"}},
	}
	got, err := p.RecentReviewers(context.Background(), "owner/repo")
	if err != nil {
		t.Fatalf("RecentReviewers() error = %v", err)
	}
	want := []string{"alice", "bob"}
	if !sliceEqual(got, want) {
		t.Errorf("RecentReviewers() = %v, want %v", got, want)
	}
}

func TestDBRecentReviewerProvider_StoreError(t *testing.T) {
	p := &DBRecentReviewerProvider{
		Store: &fakeReviewerStore{err: errors.New("db error")},
	}
	_, err := p.RecentReviewers(context.Background(), "owner/repo")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ── GitHubCodeownersProvider ──────────────────────────────────────────────────

func TestGitHubCodeownersProvider_OwnersForPR(t *testing.T) {
	codeowners := `
# This is a comment
*.go @alice @bob
/cmd/ @carol
docs/ @dave
`
	p := &GitHubCodeownersProvider{
		Files:   &fakeFileLister{files: []string{"main.go", "cmd/main.go", "docs/README.md", "other.txt"}},
		Fetcher: &fakeFileFetcher{content: codeowners},
	}
	got, err := p.OwnersForPR(context.Background(), "owner/repo", 1)
	if err != nil {
		t.Fatalf("OwnersForPR() error = %v", err)
	}
	// main.go → alice, bob; cmd/main.go → carol (overrides alice,bob for cmd path);
	// docs/README.md → dave; other.txt → no match
	// alice, bob come first (from main.go), then carol (cmd/main.go), dave (docs/)
	want := []string{"alice", "bob", "carol", "dave"}
	if !sliceEqual(got, want) {
		t.Errorf("OwnersForPR() = %v, want %v", got, want)
	}
}

func TestGitHubCodeownersProvider_CodeownersNotFound(t *testing.T) {
	p := &GitHubCodeownersProvider{
		Files:   &fakeFileLister{files: []string{"main.go"}},
		Fetcher: &fakeFileFetcher{err: errors.New("404 not found")},
	}
	got, err := p.OwnersForPR(context.Background(), "owner/repo", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("want empty result when CODEOWNERS absent, got %v", got)
	}
}

func TestGitHubCodeownersProvider_FileListerError(t *testing.T) {
	p := &GitHubCodeownersProvider{
		Files:   &fakeFileLister{err: errors.New("api error")},
		Fetcher: &fakeFileFetcher{content: "*.go @alice"},
	}
	_, err := p.OwnersForPR(context.Background(), "owner/repo", 1)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestGitHubCodeownersProvider_NoMatchingRules(t *testing.T) {
	p := &GitHubCodeownersProvider{
		Files:   &fakeFileLister{files: []string{"README.md"}},
		Fetcher: &fakeFileFetcher{content: "*.go @alice"},
	}
	got, err := p.OwnersForPR(context.Background(), "owner/repo", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("want empty result when no rules match, got %v", got)
	}
}

// ── parseCodeowners ───────────────────────────────────────────────────────────

func TestParseCodeowners(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []codeownersRule
	}{
		{
			name:    "empty",
			content: "",
			want:    nil,
		},
		{
			name:    "comment only",
			content: "# just a comment",
			want:    nil,
		},
		{
			name:    "blank lines ignored",
			content: "\n\n*.go @alice\n\n",
			want:    []codeownersRule{{pattern: "*.go", owners: []string{"alice"}}},
		},
		{
			name:    "at-sign stripped",
			content: "*.go @alice @bob",
			want:    []codeownersRule{{pattern: "*.go", owners: []string{"alice", "bob"}}},
		},
		{
			name:    "team owner: org/team",
			content: "*.go @myorg/backend",
			want:    []codeownersRule{{pattern: "*.go", owners: []string{"myorg/backend"}}},
		},
		{
			name:    "pattern without owners ignored",
			content: "*.go",
			want:    nil,
		},
		{
			name:    "multiple rules",
			content: "*.go @alice\n/cmd/ @carol",
			want: []codeownersRule{
				{pattern: "*.go", owners: []string{"alice"}},
				{pattern: "/cmd/", owners: []string{"carol"}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseCodeowners(tt.content)
			if len(got) != len(tt.want) {
				t.Fatalf("parseCodeowners() = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i].pattern != tt.want[i].pattern {
					t.Errorf("rule[%d].pattern = %q, want %q", i, got[i].pattern, tt.want[i].pattern)
				}
				if !sliceEqual(got[i].owners, tt.want[i].owners) {
					t.Errorf("rule[%d].owners = %v, want %v", i, got[i].owners, tt.want[i].owners)
				}
			}
		})
	}
}

// ── matchOwners ───────────────────────────────────────────────────────────────

func TestMatchOwners_LastMatchWins(t *testing.T) {
	rules := []codeownersRule{
		{pattern: "*.go", owners: []string{"alice"}},
		{pattern: "cmd/*.go", owners: []string{"bob"}},
	}
	got := matchOwners(rules, "cmd/main.go")
	want := []string{"bob"}
	if !sliceEqual(got, want) {
		t.Errorf("matchOwners() = %v, want %v", got, want)
	}
}

func TestMatchOwners_NoMatch(t *testing.T) {
	rules := []codeownersRule{
		{pattern: "*.go", owners: []string{"alice"}},
	}
	got := matchOwners(rules, "README.md")
	if len(got) != 0 {
		t.Errorf("want no match, got %v", got)
	}
}

func TestMatchOwners_EmptyRules(t *testing.T) {
	got := matchOwners(nil, "main.go")
	if len(got) != 0 {
		t.Errorf("want no match, got %v", got)
	}
}

// ── globMatch ─────────────────────────────────────────────────────────────────

func TestGlobMatch(t *testing.T) {
	tests := []struct {
		pattern  string
		filePath string
		want     bool
	}{
		// No slash — matches any component
		{"*.go", "main.go", true},
		{"*.go", "pkg/util.go", true},
		{"*.go", "README.md", false},
		{"docs", "docs/README.md", true},
		// Rooted pattern with trailing slash (directory)
		{"/cmd/", "cmd/main.go", true},
		{"/cmd/", "other/main.go", false},
		// Pattern with slash, not rooted, glob match
		{"cmd/*.go", "cmd/main.go", true},
		{"cmd/*.go", "other/main.go", false},
		// No slash — component match via loop
		{"cmd", "cmd/main.go", true},
		// Rooted pattern without trailing slash — directory prefix match
		{"/cmd", "cmd/main.go", true},
		// Rooted pattern without trailing slash — no match at all
		{"/cmd", "other/main.go", false},
	}
	for _, tt := range tests {
		t.Run(tt.pattern+"~"+tt.filePath, func(t *testing.T) {
			got := globMatch(tt.pattern, tt.filePath)
			if got != tt.want {
				t.Errorf("globMatch(%q, %q) = %v, want %v", tt.pattern, tt.filePath, got, tt.want)
			}
		})
	}
}
