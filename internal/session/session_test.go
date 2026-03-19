package session_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/evanisnor/argh/internal/session"
)

// ── GenerateID ────────────────────────────────────────────────────────────────

func TestGenerateID(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{0, "a"},
		{1, "b"},
		{25, "z"},
		{26, "aa"},
		{27, "ab"},
		{51, "az"},
		{52, "ba"},
		{701, "zz"},  // 26 + 26*26 - 1 = 701
		{702, "aaa"}, // 26 + 26*26 = 702
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := session.GenerateID(tt.n)
			if got != tt.want {
				t.Errorf("GenerateID(%d) = %q, want %q", tt.n, got, tt.want)
			}
		})
	}
}

// ── fakeStore ─────────────────────────────────────────────────────────────────

type fakeStore struct {
	data        map[string]string
	clearErr    error
	upsertErr   error
	getErr      error
	clearCalled bool
}

func newFakeStore() *fakeStore {
	return &fakeStore{data: make(map[string]string)}
}

func (f *fakeStore) ClearSessionIDs() error {
	f.clearCalled = true
	if f.clearErr != nil {
		return f.clearErr
	}
	f.data = make(map[string]string)
	return nil
}

func (f *fakeStore) UpsertSessionID(prURL string, sessionID string) error {
	if f.upsertErr != nil {
		return f.upsertErr
	}
	f.data[prURL] = sessionID
	return nil
}

func (f *fakeStore) GetSessionID(prURL string) (string, error) {
	if f.getErr != nil {
		return "", f.getErr
	}
	id, ok := f.data[prURL]
	if !ok {
		return "", errors.New("not found")
	}
	return id, nil
}

// ── Manager.Assign ────────────────────────────────────────────────────────────

func TestAssign_SinglePR(t *testing.T) {
	store := newFakeStore()
	mgr := session.New(store)

	if err := mgr.Assign([]string{"https://github.com/r/r/pull/1"}); err != nil {
		t.Fatalf("Assign() error: %v", err)
	}

	if !store.clearCalled {
		t.Error("expected ClearSessionIDs to be called")
	}

	got, err := mgr.GetID("https://github.com/r/r/pull/1")
	if err != nil {
		t.Fatalf("GetID() error: %v", err)
	}
	if got != "a" {
		t.Errorf("session ID = %q, want %q", got, "a")
	}
}

func TestAssign_26PRs(t *testing.T) {
	store := newFakeStore()
	mgr := session.New(store)

	urls := make([]string, 26)
	for i := range urls {
		urls[i] = fmt.Sprintf("https://github.com/r/r/pull/%d", i+1)
	}

	if err := mgr.Assign(urls); err != nil {
		t.Fatalf("Assign() error: %v", err)
	}

	got, err := mgr.GetID(urls[25])
	if err != nil {
		t.Fatalf("GetID() error: %v", err)
	}
	if got != "z" {
		t.Errorf("26th session ID = %q, want %q", got, "z")
	}
}

func TestAssign_27PRs(t *testing.T) {
	store := newFakeStore()
	mgr := session.New(store)

	urls := make([]string, 27)
	for i := range urls {
		urls[i] = "https://github.com/r/r/pull/" + fmt.Sprint(i+1)
	}

	if err := mgr.Assign(urls); err != nil {
		t.Fatalf("Assign() error: %v", err)
	}

	got, err := mgr.GetID(urls[26])
	if err != nil {
		t.Fatalf("GetID() error: %v", err)
	}
	if got != "aa" {
		t.Errorf("27th session ID = %q, want %q", got, "aa")
	}
}

func TestAssign_ClearsBeforeReassigning(t *testing.T) {
	store := newFakeStore()
	mgr := session.New(store)

	urls := []string{
		"https://github.com/r/r/pull/1",
		"https://github.com/r/r/pull/2",
	}

	if err := mgr.Assign(urls); err != nil {
		t.Fatalf("first Assign() error: %v", err)
	}

	// Second assign with reversed order — verify old IDs are gone and new ones set.
	if err := mgr.Assign([]string{urls[1], urls[0]}); err != nil {
		t.Fatalf("second Assign() error: %v", err)
	}

	got, err := mgr.GetID(urls[1])
	if err != nil {
		t.Fatalf("GetID() error: %v", err)
	}
	if got != "a" {
		t.Errorf("after reload, first URL session ID = %q, want %q", got, "a")
	}
}

func TestAssign_StableURLKey(t *testing.T) {
	store := newFakeStore()
	mgr := session.New(store)

	url := "https://github.com/r/r/pull/42"
	if err := mgr.Assign([]string{url}); err != nil {
		t.Fatalf("Assign() error: %v", err)
	}

	// Reload with same URL in same position — ID must be the same.
	if err := mgr.Assign([]string{url}); err != nil {
		t.Fatalf("second Assign() error: %v", err)
	}

	got, err := mgr.GetID(url)
	if err != nil {
		t.Fatalf("GetID() error: %v", err)
	}
	if got != "a" {
		t.Errorf("session ID after reload = %q, want %q", got, "a")
	}
}

func TestAssign_EmptyList(t *testing.T) {
	store := newFakeStore()
	mgr := session.New(store)

	if err := mgr.Assign([]string{}); err != nil {
		t.Fatalf("Assign([]) error: %v", err)
	}
	if !store.clearCalled {
		t.Error("expected ClearSessionIDs to be called even for empty list")
	}
}

func TestAssign_ClearError(t *testing.T) {
	store := newFakeStore()
	store.clearErr = errors.New("db failure")
	mgr := session.New(store)

	err := mgr.Assign([]string{"https://github.com/r/r/pull/1"})
	if err == nil {
		t.Fatal("expected error from Assign() when clear fails")
	}
}

func TestAssign_UpsertError(t *testing.T) {
	store := newFakeStore()
	store.upsertErr = errors.New("db failure")
	mgr := session.New(store)

	err := mgr.Assign([]string{"https://github.com/r/r/pull/1"})
	if err == nil {
		t.Fatal("expected error from Assign() when upsert fails")
	}
}

// ── Manager.GetID ─────────────────────────────────────────────────────────────

func TestGetID_Found(t *testing.T) {
	store := newFakeStore()
	mgr := session.New(store)

	url := "https://github.com/r/r/pull/7"
	if err := mgr.Assign([]string{url}); err != nil {
		t.Fatalf("Assign() error: %v", err)
	}

	got, err := mgr.GetID(url)
	if err != nil {
		t.Fatalf("GetID() error: %v", err)
	}
	if got != "a" {
		t.Errorf("GetID() = %q, want %q", got, "a")
	}
}

func TestGetID_NotFound(t *testing.T) {
	store := newFakeStore()
	mgr := session.New(store)

	_, err := mgr.GetID("https://github.com/r/r/pull/999")
	if err == nil {
		t.Fatal("expected error for unknown PR URL")
	}
}

func TestGetID_StoreError(t *testing.T) {
	store := newFakeStore()
	store.getErr = errors.New("db failure")
	mgr := session.New(store)

	_, err := mgr.GetID("https://github.com/r/r/pull/1")
	if err == nil {
		t.Fatal("expected error when store returns error")
	}
}
