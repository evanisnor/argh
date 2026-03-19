package watches

import (
	"fmt"
	"time"

	"github.com/evanisnor/argh/internal/persistence"
)

// WatchStore is the persistence interface used by Manager.
type WatchStore interface {
	InsertWatch(w persistence.Watch) error
	ListWatches() ([]persistence.Watch, error)
	UpdateWatchStatus(id string, status string, firedAt *time.Time) error
}

// Manager manages watch creation, listing, and cancellation.
type Manager struct {
	store WatchStore
	clock func() time.Time
	newID func() string
}

// NewManager creates a Manager backed by the given store.
// clock is called to timestamp new watches; newID generates unique watch IDs.
func NewManager(store WatchStore, clock func() time.Time, newID func() string) *Manager {
	return &Manager{store: store, clock: clock, newID: newID}
}

// AddWatch validates trigger and action expressions, then writes a new watch
// row to the DB with status "waiting". Returns an error (and does not write)
// if either expression is invalid.
func (m *Manager) AddWatch(repo string, number int, prURL string, triggerExpr, actionExpr string) error {
	if _, err := ParseTrigger(triggerExpr); err != nil {
		return fmt.Errorf("invalid trigger expression: %w", err)
	}
	if _, err := ParseActions(actionExpr); err != nil {
		return fmt.Errorf("invalid action expression: %w", err)
	}
	w := persistence.Watch{
		ID:          m.newID(),
		PRURL:       prURL,
		PRNumber:    number,
		Repo:        repo,
		TriggerExpr: triggerExpr,
		ActionExpr:  actionExpr,
		Status:      "waiting",
		CreatedAt:   m.clock(),
	}
	return m.store.InsertWatch(w)
}

// ListWatches returns all watches whose status is not "cancelled".
func (m *Manager) ListWatches() ([]persistence.Watch, error) {
	all, err := m.store.ListWatches()
	if err != nil {
		return nil, err
	}
	active := make([]persistence.Watch, 0, len(all))
	for _, w := range all {
		if w.Status != "cancelled" {
			active = append(active, w)
		}
	}
	return active, nil
}

// CancelWatch marks the watch with the given ID as "cancelled".
func (m *Manager) CancelWatch(id string) error {
	return m.store.UpdateWatchStatus(id, "cancelled", nil)
}
