package ghcli

import (
	"context"
	"fmt"
	"testing"
)

// stubCollabStore implements api.CollaboratorStore for testing.
type stubCollabStore struct {
	repos      []string
	reposErr   error
	replaced   map[string][]string
	replaceErr error
}

func newStubCollabStore(repos []string) *stubCollabStore {
	return &stubCollabStore{repos: repos, replaced: make(map[string][]string)}
}

func (s *stubCollabStore) ListDistinctRepos() ([]string, error) {
	return s.repos, s.reposErr
}

func (s *stubCollabStore) ReplaceCollaborators(repo string, logins []string) error {
	s.replaced[repo] = logins
	return s.replaceErr
}

func TestGHCLICollaboratorsFetcher_HappyPath(t *testing.T) {
	runner := NewStubCommandRunner()
	runner.RunFunc = func(_ context.Context, args []string) ([]byte, error) {
		return []byte(`[{"login":"alice"},{"login":"bob"}]`), nil
	}
	store := newStubCollabStore([]string{"owner/repo"})
	f := NewGHCLICollaboratorsFetcher(runner, store)

	if err := f.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	got := store.replaced["owner/repo"]
	if len(got) != 2 || got[0] != "alice" || got[1] != "bob" {
		t.Errorf("stored = %v, want [alice bob]", got)
	}
	call := runner.FindCall("api", "repos/owner/repo/collaborators")
	if call == nil {
		t.Error("expected gh api call for repos/owner/repo/collaborators")
	}
}

func TestGHCLICollaboratorsFetcher_MultipleRepos(t *testing.T) {
	runner := NewStubCommandRunner()
	runner.RunFunc = func(_ context.Context, args []string) ([]byte, error) {
		for _, a := range args {
			if a == "repos/org/alpha/collaborators" {
				return []byte(`[{"login":"alice"}]`), nil
			}
			if a == "repos/org/beta/collaborators" {
				return []byte(`[{"login":"bob"}]`), nil
			}
		}
		return []byte(`[]`), nil
	}
	store := newStubCollabStore([]string{"org/alpha", "org/beta"})
	f := NewGHCLICollaboratorsFetcher(runner, store)

	if err := f.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got := store.replaced["org/alpha"]; len(got) != 1 || got[0] != "alice" {
		t.Errorf("org/alpha = %v, want [alice]", got)
	}
	if got := store.replaced["org/beta"]; len(got) != 1 || got[0] != "bob" {
		t.Errorf("org/beta = %v, want [bob]", got)
	}
}

func TestGHCLICollaboratorsFetcher_InvalidRepoFormat(t *testing.T) {
	runner := NewStubCommandRunner()
	runner.RunFunc = func(_ context.Context, args []string) ([]byte, error) {
		return []byte(`[{"login":"alice"}]`), nil
	}
	store := newStubCollabStore([]string{"noslash", "owner/valid"})
	f := NewGHCLICollaboratorsFetcher(runner, store)

	if err := f.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if runner.CallCount() != 1 {
		t.Errorf("expected 1 API call (skipping invalid repo), got %d", runner.CallCount())
	}
	if _, ok := store.replaced["owner/valid"]; !ok {
		t.Error("expected store call for owner/valid")
	}
}

func TestGHCLICollaboratorsFetcher_APIError(t *testing.T) {
	runner := NewStubCommandRunner()
	runner.RunFunc = func(_ context.Context, args []string) ([]byte, error) {
		return nil, fmt.Errorf("network error")
	}
	store := newStubCollabStore([]string{"owner/repo"})
	f := NewGHCLICollaboratorsFetcher(runner, store)

	if err := f.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch should not return error for individual repo failure: %v", err)
	}
	if len(store.replaced) != 0 {
		t.Errorf("expected no store calls on API error, got %v", store.replaced)
	}
}

func TestGHCLICollaboratorsFetcher_403Forbidden(t *testing.T) {
	runner := NewStubCommandRunner()
	runner.RunFunc = func(_ context.Context, args []string) ([]byte, error) {
		return nil, fmt.Errorf("gh api failed: HTTP 403")
	}
	store := newStubCollabStore([]string{"owner/private"})
	f := NewGHCLICollaboratorsFetcher(runner, store)

	if err := f.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	// 403 is treated as "no data" — store is called with nil/empty logins
	// because fetchCollaborators returns nil, nil for 403.
	// The store should NOT be called since fetchCollaborators returns nil, nil
	// and nil is passed to ReplaceCollaborators.
	if got, ok := store.replaced["owner/private"]; ok && len(got) > 0 {
		t.Errorf("expected empty or no store call for 403 repo, got %v", got)
	}
}

func TestGHCLICollaboratorsFetcher_EmptyResponse(t *testing.T) {
	runner := NewStubCommandRunner()
	runner.RunFunc = func(_ context.Context, args []string) ([]byte, error) {
		return []byte(`[]`), nil
	}
	store := newStubCollabStore([]string{"owner/repo"})
	f := NewGHCLICollaboratorsFetcher(runner, store)

	if err := f.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	got := store.replaced["owner/repo"]
	if len(got) != 0 {
		t.Errorf("expected empty logins, got %v", got)
	}
}

func TestGHCLICollaboratorsFetcher_ListReposError(t *testing.T) {
	runner := NewStubCommandRunner()
	store := newStubCollabStore(nil)
	store.reposErr = fmt.Errorf("db error")
	f := NewGHCLICollaboratorsFetcher(runner, store)

	if err := f.Fetch(context.Background()); err == nil {
		t.Fatal("expected error when ListDistinctRepos fails")
	}
}

func TestGHCLICollaboratorsFetcher_StoreError(t *testing.T) {
	runner := NewStubCommandRunner()
	runner.RunFunc = func(_ context.Context, args []string) ([]byte, error) {
		return []byte(`[{"login":"alice"}]`), nil
	}
	store := newStubCollabStore([]string{"owner/repo"})
	store.replaceErr = fmt.Errorf("store error")
	f := NewGHCLICollaboratorsFetcher(runner, store)

	if err := f.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch should not return error on individual store failure: %v", err)
	}
}

func TestGHCLICollaboratorsFetcher_EmptyLoginSkipped(t *testing.T) {
	runner := NewStubCommandRunner()
	runner.RunFunc = func(_ context.Context, args []string) ([]byte, error) {
		return []byte(`[{"login":"alice"},{"login":""},{"login":"bob"}]`), nil
	}
	store := newStubCollabStore([]string{"owner/repo"})
	f := NewGHCLICollaboratorsFetcher(runner, store)

	if err := f.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	got := store.replaced["owner/repo"]
	if len(got) != 2 || got[0] != "alice" || got[1] != "bob" {
		t.Errorf("stored = %v, want [alice bob]", got)
	}
}

func TestGHCLICollaboratorsFetcher_InvalidJSON(t *testing.T) {
	runner := NewStubCommandRunner()
	runner.RunFunc = func(_ context.Context, args []string) ([]byte, error) {
		return []byte(`not json`), nil
	}
	store := newStubCollabStore([]string{"owner/repo"})
	f := NewGHCLICollaboratorsFetcher(runner, store)

	if err := f.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch should not return error for individual parse failure: %v", err)
	}
	if len(store.replaced) != 0 {
		t.Errorf("expected no store calls on parse error, got %v", store.replaced)
	}
}
