// Package status computes the condensed status line for --status mode.
package status

import (
	"fmt"
	"time"

	"github.com/evanisnor/argh/internal/persistence"
)

// StaleThreshold is the age of the most recent last_activity_at beyond which
// the data is considered stale.
const StaleThreshold = time.Hour

// Reader is the subset of persistence.DB methods needed by Compute.
type Reader interface {
	ListPullRequests() ([]persistence.PullRequest, error)
	CountPRsWithPendingReview() (int, error)
	MaxLastActivityAt() (time.Time, bool, error)
	Close() error
}

// StatusLine holds the computed counts for the status bar output.
type StatusLine struct {
	PRCount     int
	CIFailCount int
	ReviewCount int
	Stale       bool
	StaleAge    time.Duration
}

// Compute reads the PR state from r and computes the status line.
// now is injected for testability.
func Compute(r Reader, now func() time.Time) (StatusLine, error) {
	prs, err := r.ListPullRequests()
	if err != nil {
		return StatusLine{}, fmt.Errorf("listing pull requests: %w", err)
	}

	ciFailCount := 0
	for _, pr := range prs {
		if pr.CIState == "failing" {
			ciFailCount++
		}
	}

	reviewCount, err := r.CountPRsWithPendingReview()
	if err != nil {
		return StatusLine{}, fmt.Errorf("counting pending reviews: %w", err)
	}

	maxActivity, hasData, err := r.MaxLastActivityAt()
	if err != nil {
		return StatusLine{}, fmt.Errorf("querying last activity: %w", err)
	}

	stale := false
	staleAge := time.Duration(0)
	if hasData {
		age := now().Sub(maxActivity)
		if age > StaleThreshold {
			stale = true
			staleAge = age
		}
	}

	return StatusLine{
		PRCount:     len(prs),
		CIFailCount: ciFailCount,
		ReviewCount: reviewCount,
		Stale:       stale,
		StaleAge:    staleAge,
	}, nil
}

// String formats the status as a single line suitable for tmux or a shell prompt.
func (s StatusLine) String() string {
	base := fmt.Sprintf("↑%d PRs  ✗%d CI  ↓%d review", s.PRCount, s.CIFailCount, s.ReviewCount)
	if s.Stale {
		mins := int(s.StaleAge.Minutes())
		return fmt.Sprintf("%s  (%dm ago)", base, mins)
	}
	return base
}
