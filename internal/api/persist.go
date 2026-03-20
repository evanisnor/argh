package api

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/evanisnor/argh/internal/eventbus"
	"github.com/evanisnor/argh/internal/persistence"
)

// PersistPR writes a PR and its associated check runs and reviews to the DB,
// emitting events for new or changed PRs. This is the shared persist+publish
// path used by both the GraphQL and gh CLI fetchers.
func PersistPR(store PRStore, bus Publisher, pr persistence.PullRequest, runs []CheckRunData, reviews []ReviewData) error {
	existing, err := store.GetPullRequest(pr.Repo, pr.Number)
	isNew := errors.Is(err, sql.ErrNoRows)
	if err != nil && !isNew {
		return fmt.Errorf("reading existing PR %s#%d: %w", pr.Repo, pr.Number, err)
	}

	ciChanged := !isNew && existing.CIState != pr.CIState

	if err := store.UpsertPullRequest(pr); err != nil {
		return fmt.Errorf("upserting PR %s#%d: %w", pr.Repo, pr.Number, err)
	}

	for _, run := range runs {
		cr := persistence.CheckRun{
			PRID:       pr.ID,
			Name:       run.Name,
			State:      run.Status,
			Conclusion: run.Conclusion,
			URL:        run.URL,
		}
		if err := store.UpsertCheckRun(cr); err != nil {
			return fmt.Errorf("upserting check run %s: %w", run.Name, err)
		}
	}

	for _, rev := range reviews {
		r := persistence.Reviewer{
			PRID:  pr.ID,
			Login: rev.Login,
			State: rev.State,
		}
		if err := store.UpsertReviewer(r); err != nil {
			return fmt.Errorf("upserting reviewer %s: %w", rev.Login, err)
		}
	}

	if isNew {
		bus.Publish(eventbus.Event{
			Type:   eventbus.PRUpdated,
			Before: nil,
			After:  pr,
		})
	} else if ciChanged {
		bus.Publish(eventbus.Event{
			Type:   eventbus.CIChanged,
			Before: existing,
			After:  pr,
		})
	} else if !PRsEqual(existing, pr) {
		bus.Publish(eventbus.Event{
			Type:   eventbus.PRUpdated,
			Before: existing,
			After:  pr,
		})
	}

	return nil
}

// PersistRQPR writes a review-queue PR and its associated check runs, reviews,
// and commit timeline events to the DB, emitting events for new or changed PRs.
func PersistRQPR(store ReviewQueueStore, bus Publisher, pr persistence.PullRequest, runs []CheckRunData, reviews []ReviewData, commits []CommitData) error {
	existing, err := store.GetPullRequest(pr.Repo, pr.Number)
	isNew := errors.Is(err, sql.ErrNoRows)
	if err != nil && !isNew {
		return fmt.Errorf("reading existing PR %s#%d: %w", pr.Repo, pr.Number, err)
	}

	ciChanged := !isNew && existing.CIState != pr.CIState

	if err := store.UpsertPullRequest(pr); err != nil {
		return fmt.Errorf("upserting PR %s#%d: %w", pr.Repo, pr.Number, err)
	}

	for _, run := range runs {
		cr := persistence.CheckRun{
			PRID:       pr.ID,
			Name:       run.Name,
			State:      run.Status,
			Conclusion: run.Conclusion,
			URL:        run.URL,
		}
		if err := store.UpsertCheckRun(cr); err != nil {
			return fmt.Errorf("upserting check run %s: %w", run.Name, err)
		}
	}

	for _, rev := range reviews {
		r := persistence.Reviewer{
			PRID:  pr.ID,
			Login: rev.Login,
			State: rev.State,
		}
		if err := store.UpsertReviewer(r); err != nil {
			return fmt.Errorf("upserting reviewer %s: %w", rev.Login, err)
		}
	}

	for _, c := range commits {
		te := persistence.TimelineEvent{
			PRID:        pr.ID,
			EventType:   "commit",
			Actor:       c.AuthorLogin,
			CreatedAt:   c.CommittedDate.Time,
			PayloadJSON: "{}",
		}
		if err := store.InsertTimelineEvent(te); err != nil {
			return fmt.Errorf("inserting commit timeline event: %w", err)
		}
	}

	if isNew {
		bus.Publish(eventbus.Event{
			Type:   eventbus.PRUpdated,
			Before: nil,
			After:  pr,
		})
	} else if ciChanged {
		bus.Publish(eventbus.Event{
			Type:   eventbus.CIChanged,
			Before: existing,
			After:  pr,
		})
	} else if !PRsEqual(existing, pr) {
		bus.Publish(eventbus.Event{
			Type:   eventbus.PRUpdated,
			Before: existing,
			After:  pr,
		})
	}

	return nil
}
