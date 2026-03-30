package api

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/google/go-github/v69/github"
)

// stubCollaboratorsService implements CollaboratorsService for testing.
type stubCollaboratorsService struct {
	users   []*github.User
	resp    *github.Response
	err     error
	callLog []string // tracks "owner/repo" per call
}

func (s *stubCollaboratorsService) ListCollaborators(_ context.Context, owner, repo string, _ *github.ListCollaboratorsOptions) ([]*github.User, *github.Response, error) {
	s.callLog = append(s.callLog, owner+"/"+repo)
	return s.users, s.resp, s.err
}

// stubCollaboratorStore implements CollaboratorStore for testing.
type stubCollaboratorStore struct {
	repos           []string
	reposErr        error
	replaced        map[string][]string
	replaceErr      error
}

func newStubCollabStore(repos []string) *stubCollaboratorStore {
	return &stubCollaboratorStore{repos: repos, replaced: make(map[string][]string)}
}

func (s *stubCollaboratorStore) ListDistinctRepos() ([]string, error) {
	return s.repos, s.reposErr
}

func (s *stubCollaboratorStore) ReplaceCollaborators(repo string, logins []string) error {
	s.replaced[repo] = logins
	return s.replaceErr
}

func strPtr(s string) *string { return &s }

func TestCollaboratorsFetcher_FetchesAndStores(t *testing.T) {
	svc := &stubCollaboratorsService{
		users: []*github.User{
			{Login: strPtr("alice")},
			{Login: strPtr("bob")},
		},
		resp: &github.Response{Response: &http.Response{StatusCode: 200}},
	}
	store := newStubCollabStore([]string{"owner/repo"})

	f := NewCollaboratorsFetcher(svc, store)
	if err := f.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	got, ok := store.replaced["owner/repo"]
	if !ok {
		t.Fatal("expected ReplaceCollaborators to be called for owner/repo")
	}
	if len(got) != 2 || got[0] != "alice" || got[1] != "bob" {
		t.Errorf("stored = %v, want [alice bob]", got)
	}
}

func TestCollaboratorsFetcher_MultipleRepos(t *testing.T) {
	svc := &stubCollaboratorsService{
		users: []*github.User{{Login: strPtr("alice")}},
		resp:  &github.Response{Response: &http.Response{StatusCode: 200}},
	}
	store := newStubCollabStore([]string{"owner/repo-a", "owner/repo-b"})

	f := NewCollaboratorsFetcher(svc, store)
	if err := f.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	if len(svc.callLog) != 2 {
		t.Errorf("expected 2 API calls, got %d", len(svc.callLog))
	}
	if _, ok := store.replaced["owner/repo-a"]; !ok {
		t.Error("expected store call for repo-a")
	}
	if _, ok := store.replaced["owner/repo-b"]; !ok {
		t.Error("expected store call for repo-b")
	}
}

func TestCollaboratorsFetcher_403_SkipsRepo(t *testing.T) {
	svc := &stubCollaboratorsService{
		err:  fmt.Errorf("forbidden"),
		resp: &github.Response{Response: &http.Response{StatusCode: 403}},
	}
	store := newStubCollabStore([]string{"owner/repo"})

	f := NewCollaboratorsFetcher(svc, store)
	if err := f.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch should not return error on 403: %v", err)
	}

	got, ok := store.replaced["owner/repo"]
	if !ok {
		t.Fatal("expected ReplaceCollaborators to be called (with empty list)")
	}
	if len(got) != 0 {
		t.Errorf("expected empty list for 403 repo, got %v", got)
	}
}

func TestCollaboratorsFetcher_APIError_SkipsRepo(t *testing.T) {
	svc := &stubCollaboratorsService{
		err:  fmt.Errorf("network error"),
		resp: &github.Response{Response: &http.Response{StatusCode: 500}},
	}
	store := newStubCollabStore([]string{"owner/repo"})

	f := NewCollaboratorsFetcher(svc, store)
	if err := f.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch should not return error on API failure: %v", err)
	}

	if _, ok := store.replaced["owner/repo"]; ok {
		t.Error("should not store when API returns error")
	}
}

func TestCollaboratorsFetcher_ListReposError(t *testing.T) {
	svc := &stubCollaboratorsService{}
	store := newStubCollabStore(nil)
	store.reposErr = fmt.Errorf("db error")

	f := NewCollaboratorsFetcher(svc, store)
	err := f.Fetch(context.Background())
	if err == nil {
		t.Fatal("expected error when ListDistinctRepos fails")
	}
}

func TestCollaboratorsFetcher_EmptyRepos(t *testing.T) {
	svc := &stubCollaboratorsService{}
	store := newStubCollabStore(nil)

	f := NewCollaboratorsFetcher(svc, store)
	if err := f.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(svc.callLog) != 0 {
		t.Error("expected no API calls for empty repo list")
	}
}

func TestCollaboratorsFetcher_FiltersEmptyLogins(t *testing.T) {
	svc := &stubCollaboratorsService{
		users: []*github.User{
			{Login: strPtr("alice")},
			{Login: strPtr("")},
			{},
		},
		resp: &github.Response{Response: &http.Response{StatusCode: 200}},
	}
	store := newStubCollabStore([]string{"owner/repo"})

	f := NewCollaboratorsFetcher(svc, store)
	if err := f.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	got := store.replaced["owner/repo"]
	if len(got) != 1 || got[0] != "alice" {
		t.Errorf("stored = %v, want [alice]", got)
	}
}

func TestCollaboratorsFetcher_InvalidRepoFormat_SkipsRepo(t *testing.T) {
	svc := &stubCollaboratorsService{
		users: []*github.User{{Login: strPtr("alice")}},
		resp:  &github.Response{Response: &http.Response{StatusCode: 200}},
	}
	store := newStubCollabStore([]string{"noslash", "owner/valid"})

	f := NewCollaboratorsFetcher(svc, store)
	if err := f.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	// Only valid repo should be fetched.
	if len(svc.callLog) != 1 || svc.callLog[0] != "owner/valid" {
		t.Errorf("callLog = %v, want [owner/valid]", svc.callLog)
	}
	if _, ok := store.replaced["owner/valid"]; !ok {
		t.Error("expected store call for owner/valid")
	}
}

// paginatingCollaboratorsService returns different pages of results.
type paginatingCollaboratorsService struct {
	pages []struct {
		users    []*github.User
		nextPage int
	}
	callCount int
}

func (s *paginatingCollaboratorsService) ListCollaborators(_ context.Context, _, _ string, opts *github.ListCollaboratorsOptions) ([]*github.User, *github.Response, error) {
	idx := s.callCount
	if idx >= len(s.pages) {
		idx = len(s.pages) - 1
	}
	s.callCount++
	page := s.pages[idx]
	resp := &github.Response{
		Response: &http.Response{StatusCode: 200},
		NextPage: page.nextPage,
	}
	return page.users, resp, nil
}

func TestCollaboratorsFetcher_Pagination(t *testing.T) {
	svc := &paginatingCollaboratorsService{
		pages: []struct {
			users    []*github.User
			nextPage int
		}{
			{users: []*github.User{{Login: strPtr("alice")}}, nextPage: 2},
			{users: []*github.User{{Login: strPtr("bob")}}, nextPage: 0},
		},
	}
	store := newStubCollabStore([]string{"owner/repo"})

	f := NewCollaboratorsFetcher(svc, store)
	if err := f.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	got := store.replaced["owner/repo"]
	if len(got) != 2 || got[0] != "alice" || got[1] != "bob" {
		t.Errorf("stored = %v, want [alice bob]", got)
	}
	if svc.callCount != 2 {
		t.Errorf("expected 2 API calls for pagination, got %d", svc.callCount)
	}
}

func TestCollaboratorsFetcher_StoreError_Continues(t *testing.T) {
	svc := &stubCollaboratorsService{
		users: []*github.User{{Login: strPtr("alice")}},
		resp:  &github.Response{Response: &http.Response{StatusCode: 200}},
	}
	store := newStubCollabStore([]string{"owner/repo-a", "owner/repo-b"})
	store.replaceErr = fmt.Errorf("db error")

	f := NewCollaboratorsFetcher(svc, store)
	// Should not return error — continues to next repo.
	if err := f.Fetch(context.Background()); err != nil {
		t.Fatalf("Fetch should not error on store failure: %v", err)
	}
	// Both repos should have been attempted.
	if len(svc.callLog) != 2 {
		t.Errorf("expected 2 API calls, got %d", len(svc.callLog))
	}
}
