package watches

import (
	"errors"
	"testing"
	"time"

	"github.com/evanisnor/argh/internal/persistence"
)

// ── fakeWatchStore ────────────────────────────────────────────────────────────

type fakeWatchStore struct {
	watches         []persistence.Watch
	insertErr       error
	listErr         error
	updateStatusErr error

	insertCalled bool
	updateCalled bool
	lastInserted persistence.Watch
	lastUpdateID string
	lastStatus   string
}

func (f *fakeWatchStore) InsertWatch(w persistence.Watch) error {
	f.insertCalled = true
	f.lastInserted = w
	if f.insertErr != nil {
		return f.insertErr
	}
	f.watches = append(f.watches, w)
	return nil
}

func (f *fakeWatchStore) ListWatches() ([]persistence.Watch, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.watches, nil
}

func (f *fakeWatchStore) UpdateWatchStatus(id string, status string, firedAt *time.Time) error {
	f.updateCalled = true
	f.lastUpdateID = id
	f.lastStatus = status
	if f.updateStatusErr != nil {
		return f.updateStatusErr
	}
	for i, w := range f.watches {
		if w.ID == id {
			f.watches[i].Status = status
			if firedAt != nil {
				f.watches[i].FiredAt = firedAt
			}
			return nil
		}
	}
	return nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

var fixedTime = time.Date(2026, 3, 19, 12, 0, 0, 0, time.UTC)

func fixedClock() time.Time { return fixedTime }

var idCounter int

func newTestID() string {
	idCounter++
	return "watch-test-id"
}

func newManager(store *fakeWatchStore) *Manager {
	return NewManager(store, fixedClock, newTestID)
}

// ── AddWatch ──────────────────────────────────────────────────────────────────

func TestManager_AddWatch_Success(t *testing.T) {
	store := &fakeWatchStore{}
	m := newManager(store)

	err := m.AddWatch("owner/repo", 42, "https://github.com/owner/repo/pull/42", "on:ci-pass", "merge")
	if err != nil {
		t.Fatalf("AddWatch() unexpected error: %v", err)
	}
	if !store.insertCalled {
		t.Fatal("InsertWatch should have been called")
	}
	w := store.lastInserted
	if w.Repo != "owner/repo" {
		t.Errorf("Repo = %q, want %q", w.Repo, "owner/repo")
	}
	if w.PRNumber != 42 {
		t.Errorf("PRNumber = %d, want 42", w.PRNumber)
	}
	if w.PRURL != "https://github.com/owner/repo/pull/42" {
		t.Errorf("PRURL = %q, unexpected", w.PRURL)
	}
	if w.TriggerExpr != "on:ci-pass" {
		t.Errorf("TriggerExpr = %q, want %q", w.TriggerExpr, "on:ci-pass")
	}
	if w.ActionExpr != "merge" {
		t.Errorf("ActionExpr = %q, want %q", w.ActionExpr, "merge")
	}
	if w.Status != "waiting" {
		t.Errorf("Status = %q, want %q", w.Status, "waiting")
	}
	if w.CreatedAt != fixedTime {
		t.Errorf("CreatedAt = %v, want %v", w.CreatedAt, fixedTime)
	}
	if w.ID == "" {
		t.Error("ID must not be empty")
	}
}

func TestManager_AddWatch_InvalidTrigger(t *testing.T) {
	store := &fakeWatchStore{}
	m := newManager(store)

	err := m.AddWatch("owner/repo", 42, "https://github.com/owner/repo/pull/42", "not-a-trigger", "merge")
	if err == nil {
		t.Fatal("expected error for invalid trigger expression")
	}
	if store.insertCalled {
		t.Error("InsertWatch must not be called when trigger is invalid")
	}
}

func TestManager_AddWatch_InvalidAction(t *testing.T) {
	store := &fakeWatchStore{}
	m := newManager(store)

	err := m.AddWatch("owner/repo", 42, "https://github.com/owner/repo/pull/42", "on:ci-pass", "unknown-action")
	if err == nil {
		t.Fatal("expected error for invalid action expression")
	}
	if store.insertCalled {
		t.Error("InsertWatch must not be called when action is invalid")
	}
}

func TestManager_AddWatch_StoreError(t *testing.T) {
	store := &fakeWatchStore{insertErr: errors.New("db full")}
	m := newManager(store)

	err := m.AddWatch("owner/repo", 42, "https://github.com/owner/repo/pull/42", "on:ci-pass", "merge")
	if err == nil {
		t.Fatal("expected error from InsertWatch")
	}
}

func TestManager_AddWatch_ComplexExpressions(t *testing.T) {
	tests := []struct {
		name        string
		triggerExpr string
		actionExpr  string
		wantErr     bool
	}{
		{"AND trigger", "on:approved+ci-pass", "merge", false},
		{"OR trigger", "on:ci-pass,approved", "merge:squash", false},
		{"stale trigger", "on:24h-stale", "notify", false},
		{"combined actions", "on:ci-pass", "merge + notify", false},
		{"approved:N trigger", "on:approved:2", "merge", false},
		{"label trigger", "on:label-added:ready", "notify", false},
		{"empty trigger body", "on:", "", true},
		{"empty action", "on:ci-pass", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &fakeWatchStore{}
			m := newManager(store)
			err := m.AddWatch("owner/repo", 1, "https://github.com/owner/repo/pull/1", tt.triggerExpr, tt.actionExpr)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error for trigger=%q action=%q", tt.triggerExpr, tt.actionExpr)
				}
				if store.insertCalled {
					t.Error("InsertWatch must not be called on validation error")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

// ── ListWatches ───────────────────────────────────────────────────────────────

func TestManager_ListWatches_ExcludesCancelled(t *testing.T) {
	store := &fakeWatchStore{
		watches: []persistence.Watch{
			{ID: "w1", Status: "waiting"},
			{ID: "w2", Status: "cancelled"},
			{ID: "w3", Status: "fired"},
			{ID: "w4", Status: "failed"},
		},
	}
	m := newManager(store)

	watches, err := m.ListWatches()
	if err != nil {
		t.Fatalf("ListWatches() error: %v", err)
	}
	if len(watches) != 3 {
		t.Fatalf("expected 3 watches (non-cancelled), got %d", len(watches))
	}
	for _, w := range watches {
		if w.Status == "cancelled" {
			t.Errorf("cancelled watch %q should not appear in ListWatches", w.ID)
		}
	}
}

func TestManager_ListWatches_EmptyStore(t *testing.T) {
	store := &fakeWatchStore{}
	m := newManager(store)

	watches, err := m.ListWatches()
	if err != nil {
		t.Fatalf("ListWatches() error: %v", err)
	}
	if len(watches) != 0 {
		t.Errorf("expected empty list, got %d watches", len(watches))
	}
}

func TestManager_ListWatches_AllCancelled(t *testing.T) {
	store := &fakeWatchStore{
		watches: []persistence.Watch{
			{ID: "w1", Status: "cancelled"},
			{ID: "w2", Status: "cancelled"},
		},
	}
	m := newManager(store)

	watches, err := m.ListWatches()
	if err != nil {
		t.Fatalf("ListWatches() error: %v", err)
	}
	if len(watches) != 0 {
		t.Errorf("expected 0 watches when all cancelled, got %d", len(watches))
	}
}

func TestManager_ListWatches_StoreError(t *testing.T) {
	store := &fakeWatchStore{listErr: errors.New("db error")}
	m := newManager(store)

	_, err := m.ListWatches()
	if err == nil {
		t.Fatal("expected error from store.ListWatches")
	}
}

// ── CancelWatch ───────────────────────────────────────────────────────────────

func TestManager_CancelWatch_Success(t *testing.T) {
	store := &fakeWatchStore{
		watches: []persistence.Watch{
			{ID: "w1", Status: "waiting"},
		},
	}
	m := newManager(store)

	err := m.CancelWatch("w1")
	if err != nil {
		t.Fatalf("CancelWatch() error: %v", err)
	}
	if !store.updateCalled {
		t.Fatal("UpdateWatchStatus should have been called")
	}
	if store.lastUpdateID != "w1" {
		t.Errorf("UpdateWatchStatus called with ID %q, want %q", store.lastUpdateID, "w1")
	}
	if store.lastStatus != "cancelled" {
		t.Errorf("UpdateWatchStatus called with status %q, want %q", store.lastStatus, "cancelled")
	}
}

func TestManager_CancelWatch_StoreError(t *testing.T) {
	store := &fakeWatchStore{updateStatusErr: errors.New("db error")}
	m := newManager(store)

	err := m.CancelWatch("w1")
	if err == nil {
		t.Fatal("expected error from UpdateWatchStatus")
	}
}

// ── round-trip via DB (survives restart) ─────────────────────────────────────

func TestManager_WatchSurvivesRestart(t *testing.T) {
	// Simulate restart: insert via one Manager instance, list via a new one
	// backed by the same in-memory store.
	store := &fakeWatchStore{}
	m1 := newManager(store)

	err := m1.AddWatch("owner/repo", 7, "https://github.com/owner/repo/pull/7", "on:approved", "merge")
	if err != nil {
		t.Fatalf("AddWatch: %v", err)
	}

	// New Manager, same store (simulates restart reading from persistent DB).
	m2 := newManager(store)
	watches, err := m2.ListWatches()
	if err != nil {
		t.Fatalf("ListWatches after restart: %v", err)
	}
	if len(watches) != 1 {
		t.Fatalf("expected 1 watch after restart, got %d", len(watches))
	}
	if watches[0].TriggerExpr != "on:approved" {
		t.Errorf("TriggerExpr = %q, want %q", watches[0].TriggerExpr, "on:approved")
	}
}

// ── cancel then list ──────────────────────────────────────────────────────────

func TestManager_CancelThenList_ExcludesCancelled(t *testing.T) {
	store := &fakeWatchStore{}
	m := newManager(store)

	if err := m.AddWatch("owner/repo", 1, "https://github.com/owner/repo/pull/1", "on:ci-pass", "merge"); err != nil {
		t.Fatalf("AddWatch: %v", err)
	}
	id := store.lastInserted.ID

	watches, err := m.ListWatches()
	if err != nil {
		t.Fatalf("ListWatches: %v", err)
	}
	if len(watches) != 1 {
		t.Fatalf("expected 1 watch before cancel, got %d", len(watches))
	}

	if err := m.CancelWatch(id); err != nil {
		t.Fatalf("CancelWatch: %v", err)
	}

	watches, err = m.ListWatches()
	if err != nil {
		t.Fatalf("ListWatches after cancel: %v", err)
	}
	if len(watches) != 0 {
		t.Fatalf("expected 0 watches after cancel, got %d", len(watches))
	}
}
