// Package session manages ephemeral per-session alphabetic IDs assigned to PRs.
//
// IDs are assigned in display order each time the PR list is loaded or reloaded.
// The sequence is: a, b, c, … z, aa, ab, … az, ba, … and so on.
// Mappings are stored in the session_ids SQLite table so the command bar can
// resolve "a" → PR URL. The table is cleared on every reload.
package session

import "fmt"

// Store persists session ID mappings between a PR URL and its session ID.
type Store interface {
	ClearSessionIDs() error
	UpsertSessionID(prURL string, sessionID string) error
	GetSessionID(prURL string) (string, error)
}

// GenerateID returns the alphabetic session ID for position n (0-indexed).
// n=0 → "a", n=25 → "z", n=26 → "aa", n=27 → "ab", n=51 → "az", n=52 → "ba".
func GenerateID(n int) string {
	id := ""
	n++ // shift to 1-indexed so the modular arithmetic works correctly
	for n > 0 {
		n--               // adjust so 'a' maps to remainder 0
		id = string(rune('a'+n%26)) + id
		n /= 26
	}
	return id
}

// Manager assigns and retrieves session IDs for PRs.
type Manager struct {
	store Store
}

// New creates a new Manager backed by the given Store.
func New(store Store) *Manager {
	return &Manager{store: store}
}

// Assign clears all existing session IDs then assigns fresh ones to the given
// PR URLs in the order they are provided. Index 0 receives "a", index 1 "b",
// and so on.
func (m *Manager) Assign(prURLs []string) error {
	if err := m.store.ClearSessionIDs(); err != nil {
		return fmt.Errorf("session: clear session IDs: %w", err)
	}
	for i, url := range prURLs {
		if err := m.store.UpsertSessionID(url, GenerateID(i)); err != nil {
			return fmt.Errorf("session: upsert session ID for %s: %w", url, err)
		}
	}
	return nil
}

// GetID returns the session ID for the given PR URL.
func (m *Manager) GetID(prURL string) (string, error) {
	return m.store.GetSessionID(prURL)
}
