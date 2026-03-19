package watches

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/evanisnor/argh/internal/eventbus"
	"github.com/evanisnor/argh/internal/persistence"
)

// PRReader reads PR state from the persistence layer.
type PRReader interface {
	ListReviewers(prID string) ([]persistence.Reviewer, error)
	ListReviewThreads(prID string) ([]persistence.ReviewThread, error)
}

// ActionExecutor executes watch actions against the GitHub API.
type ActionExecutor interface {
	MergePR(ctx context.Context, repo string, number int, method string) error
	MarkReadyForReview(ctx context.Context, repo string, number int, globalID string) error
	RequestReview(ctx context.Context, repo string, number int, users []string) error
	PostComment(ctx context.Context, repo string, number int, body string) error
	AddLabel(ctx context.Context, repo string, number int, label string) error
}

// Notifier sends a desktop notification.
type Notifier interface {
	Notify(title, body string) error
}

// EngineEventBus is the event bus subset used by the Engine.
type EngineEventBus interface {
	Subscribe(handler func(eventbus.Event)) func()
	Publish(e eventbus.Event)
}

// EngineAuditLogger records watch execution in the audit log.
type EngineAuditLogger interface {
	Log(ctx context.Context, action, owner, repo string, number int, details string) error
}

// EngineWatchStore is the persistence interface used by the Engine.
type EngineWatchStore interface {
	ListWatches() ([]persistence.Watch, error)
	UpdateWatchStatus(id string, status string, firedAt *time.Time) error
}

// Engine subscribes to the DB event bus and evaluates watch triggers,
// executing the corresponding actions when conditions are met.
type Engine struct {
	store    EngineWatchStore
	prReader PRReader
	executor ActionExecutor
	notifier Notifier
	bus      EngineEventBus
	audit    EngineAuditLogger
	clock    func() time.Time
}

// NewEngine creates a new Engine with the given dependencies.
func NewEngine(
	store EngineWatchStore,
	prReader PRReader,
	executor ActionExecutor,
	notifier Notifier,
	bus EngineEventBus,
	audit EngineAuditLogger,
	clock func() time.Time,
) *Engine {
	return &Engine{
		store:    store,
		prReader: prReader,
		executor: executor,
		notifier: notifier,
		bus:      bus,
		audit:    audit,
		clock:    clock,
	}
}

// Run subscribes to the event bus and evaluates watches on PR state change events.
// It blocks until ctx is cancelled, then returns cleanly.
func (e *Engine) Run(ctx context.Context) {
	unsub := e.bus.Subscribe(func(ev eventbus.Event) {
		switch ev.Type {
		case eventbus.PRUpdated, eventbus.CIChanged, eventbus.ReviewChanged:
			pr, ok := ev.After.(persistence.PullRequest)
			if !ok {
				return
			}
			e.evaluateWatchesForPR(ctx, pr)
		}
	})
	defer unsub()
	<-ctx.Done()
}

// evaluateWatchesForPR loads all waiting watches for the given PR URL and
// evaluates each trigger against the current PR snapshot.
func (e *Engine) evaluateWatchesForPR(ctx context.Context, pr persistence.PullRequest) {
	watches, err := e.store.ListWatches()
	if err != nil {
		return
	}

	snapshot, err := e.buildSnapshot(pr)
	if err != nil {
		return
	}

	for _, w := range watches {
		if w.Status != "waiting" {
			continue
		}
		if w.PRURL != pr.URL {
			continue
		}
		e.evaluateWatch(ctx, w, snapshot, pr)
	}
}

// buildSnapshot constructs a PRSnapshot from the current PR state and related rows.
func (e *Engine) buildSnapshot(pr persistence.PullRequest) (PRSnapshot, error) {
	reviewers, err := e.prReader.ListReviewers(pr.ID)
	if err != nil {
		return PRSnapshot{}, fmt.Errorf("listing reviewers: %w", err)
	}
	approvals := 0
	for _, r := range reviewers {
		if r.State == "APPROVED" {
			approvals++
		}
	}

	threads, err := e.prReader.ListReviewThreads(pr.ID)
	if err != nil {
		return PRSnapshot{}, fmt.Errorf("listing review threads: %w", err)
	}
	allResolved := true
	for _, t := range threads {
		if !t.Resolved {
			allResolved = false
			break
		}
	}

	return PRSnapshot{
		Status:             pr.Status,
		CIState:            pr.CIState,
		ApprovalCount:      approvals,
		AllThreadsResolved: allResolved,
		Labels:             []string{},
		LastActivityAt:     pr.LastActivityAt,
		Now:                e.clock(),
	}, nil
}

// evaluateWatch evaluates a single watch trigger and executes actions if it fires.
func (e *Engine) evaluateWatch(ctx context.Context, w persistence.Watch, snapshot PRSnapshot, pr persistence.PullRequest) {
	node, err := ParseTrigger(w.TriggerExpr)
	if err != nil {
		now := e.clock()
		_ = e.store.UpdateWatchStatus(w.ID, "failed", &now)
		owner, repo := splitRepo(w.Repo)
		_ = e.audit.Log(ctx, "watch-failed", owner, repo, w.PRNumber, fmt.Sprintf("invalid trigger: %v", err))
		e.bus.Publish(eventbus.Event{Type: eventbus.WatchFired, After: w})
		return
	}

	if !node.Evaluate(snapshot) {
		return
	}

	actions, err := ParseActions(w.ActionExpr)
	if err != nil {
		now := e.clock()
		_ = e.store.UpdateWatchStatus(w.ID, "failed", &now)
		owner, repo := splitRepo(w.Repo)
		_ = e.audit.Log(ctx, "watch-failed", owner, repo, w.PRNumber, fmt.Sprintf("invalid action: %v", err))
		e.bus.Publish(eventbus.Event{Type: eventbus.WatchFired, After: w})
		return
	}

	for _, action := range actions {
		if err := e.executeAction(ctx, action, pr); err != nil {
			now := e.clock()
			_ = e.store.UpdateWatchStatus(w.ID, "failed", &now)
			owner, repo := splitRepo(w.Repo)
			_ = e.audit.Log(ctx, "watch-failed", owner, repo, w.PRNumber,
				fmt.Sprintf("action %s failed: %v", action.Type, err))
			e.bus.Publish(eventbus.Event{Type: eventbus.WatchFired, After: w})
			return
		}
	}

	now := e.clock()
	_ = e.store.UpdateWatchStatus(w.ID, "fired", &now)
	owner, repo := splitRepo(w.Repo)
	_ = e.audit.Log(ctx, "watch-fired", owner, repo, w.PRNumber,
		fmt.Sprintf("trigger=%s action=%s", w.TriggerExpr, w.ActionExpr))
	e.bus.Publish(eventbus.Event{Type: eventbus.WatchFired, After: w})
}

// executeAction executes a single watch action against the given PR.
func (e *Engine) executeAction(ctx context.Context, action Action, pr persistence.PullRequest) error {
	switch action.Type {
	case ActionMerge:
		return e.executor.MergePR(ctx, pr.Repo, pr.Number, action.Method)
	case ActionReady:
		return e.executor.MarkReadyForReview(ctx, pr.Repo, pr.Number, pr.GlobalID)
	case ActionRequest:
		user := strings.TrimPrefix(action.User, "@")
		return e.executor.RequestReview(ctx, pr.Repo, pr.Number, []string{user})
	case ActionComment:
		return e.executor.PostComment(ctx, pr.Repo, pr.Number, action.Text)
	case ActionLabel:
		return e.executor.AddLabel(ctx, pr.Repo, pr.Number, action.Name)
	case ActionNotify:
		return e.notifier.Notify(
			fmt.Sprintf("Watch fired: %s#%d", pr.Repo, pr.Number),
			pr.Title,
		)
	default:
		return fmt.Errorf("unknown action type: %q", action.Type)
	}
}

// splitRepo splits "owner/repo" into separate owner and repo components.
// If the string contains no slash, it is returned as owner with an empty repo.
func splitRepo(full string) (owner, repo string) {
	parts := strings.SplitN(full, "/", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return full, ""
}
