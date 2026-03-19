package status

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/evanisnor/argh/internal/persistence"
)

// ── fakeReader ───────────────────────────────────────────────────────────────

type fakeReader struct {
	prs             []persistence.PullRequest
	prsErr          error
	pendingCount    int
	pendingCountErr error
	maxActivity     time.Time
	hasData         bool
	maxActivityErr  error
}

func (f *fakeReader) ListPullRequests() ([]persistence.PullRequest, error) {
	return f.prs, f.prsErr
}

func (f *fakeReader) CountPRsWithPendingReview() (int, error) {
	return f.pendingCount, f.pendingCountErr
}

func (f *fakeReader) MaxLastActivityAt() (time.Time, bool, error) {
	return f.maxActivity, f.hasData, f.maxActivityErr
}

func (f *fakeReader) Close() error { return nil }

// ── Helpers ──────────────────────────────────────────────────────────────────

func fixedNow(t time.Time) func() time.Time { return func() time.Time { return t } }

func makePR(ciState string) persistence.PullRequest {
	return persistence.PullRequest{
		ID: "pr-1", Repo: "r/r", Number: 1, Title: "t",
		Status: "open", CIState: ciState, Author: "me",
		CreatedAt: time.Now(), UpdatedAt: time.Now(), LastActivityAt: time.Now(),
		URL: "https://github.com/r/r/pull/1", GlobalID: "pr-1",
	}
}

// ── Compute ───────────────────────────────────────────────────────────────────

func TestCompute_NoPRs(t *testing.T) {
	r := &fakeReader{}
	line, err := Compute(r, fixedNow(time.Now()))
	if err != nil {
		t.Fatalf("Compute() error: %v", err)
	}
	if line.PRCount != 0 || line.CIFailCount != 0 || line.ReviewCount != 0 {
		t.Errorf("expected all zeros, got %+v", line)
	}
	if line.Stale {
		t.Error("Stale = true, want false when no PRs")
	}
}

func TestCompute_CountsPRsAndCIFails(t *testing.T) {
	now := time.Now()
	recent := now.Add(-30 * time.Minute)

	r := &fakeReader{
		prs: []persistence.PullRequest{
			{CIState: "failing", LastActivityAt: recent},
			{CIState: "passing", LastActivityAt: recent},
			{CIState: "failing", LastActivityAt: recent},
			{CIState: "none", LastActivityAt: recent},
		},
		pendingCount: 2,
		hasData:      true,
		maxActivity:  recent,
	}

	line, err := Compute(r, fixedNow(now))
	if err != nil {
		t.Fatalf("Compute() error: %v", err)
	}
	if line.PRCount != 4 {
		t.Errorf("PRCount = %d, want 4", line.PRCount)
	}
	if line.CIFailCount != 2 {
		t.Errorf("CIFailCount = %d, want 2", line.CIFailCount)
	}
	if line.ReviewCount != 2 {
		t.Errorf("ReviewCount = %d, want 2", line.ReviewCount)
	}
}

func TestCompute_NotStaleWhenRecent(t *testing.T) {
	now := time.Now()
	recent := now.Add(-30 * time.Minute)

	r := &fakeReader{
		prs:         []persistence.PullRequest{makePR("passing")},
		hasData:     true,
		maxActivity: recent,
	}
	line, err := Compute(r, fixedNow(now))
	if err != nil {
		t.Fatalf("Compute() error: %v", err)
	}
	if line.Stale {
		t.Error("Stale = true, want false when last activity was 30m ago")
	}
}

func TestCompute_StaleWhenOld(t *testing.T) {
	now := time.Now()
	old := now.Add(-2 * time.Hour)

	r := &fakeReader{
		prs:         []persistence.PullRequest{makePR("passing")},
		hasData:     true,
		maxActivity: old,
	}
	line, err := Compute(r, fixedNow(now))
	if err != nil {
		t.Fatalf("Compute() error: %v", err)
	}
	if !line.Stale {
		t.Error("Stale = false, want true when last activity was 2h ago")
	}
	if line.StaleAge < time.Hour {
		t.Errorf("StaleAge = %v, want >= 1h", line.StaleAge)
	}
}

func TestCompute_NotStaleWhenNoData(t *testing.T) {
	r := &fakeReader{hasData: false}
	line, err := Compute(r, fixedNow(time.Now()))
	if err != nil {
		t.Fatalf("Compute() error: %v", err)
	}
	if line.Stale {
		t.Error("Stale = true, want false when hasData=false")
	}
}

func TestCompute_ListPRsError(t *testing.T) {
	r := &fakeReader{prsErr: errors.New("db closed")}
	_, err := Compute(r, fixedNow(time.Now()))
	if err == nil {
		t.Fatal("expected error from ListPullRequests failure")
	}
	if !strings.Contains(err.Error(), "db closed") {
		t.Errorf("error = %v, want to contain 'db closed'", err)
	}
}

func TestCompute_CountPendingError(t *testing.T) {
	r := &fakeReader{
		prs:             []persistence.PullRequest{makePR("passing")},
		pendingCountErr: errors.New("scan error"),
	}
	_, err := Compute(r, fixedNow(time.Now()))
	if err == nil {
		t.Fatal("expected error from CountPRsWithPendingReview failure")
	}
	if !strings.Contains(err.Error(), "scan error") {
		t.Errorf("error = %v, want to contain 'scan error'", err)
	}
}

func TestCompute_MaxLastActivityError(t *testing.T) {
	r := &fakeReader{
		prs:            []persistence.PullRequest{makePR("passing")},
		maxActivityErr: errors.New("query failed"),
	}
	_, err := Compute(r, fixedNow(time.Now()))
	if err == nil {
		t.Fatal("expected error from MaxLastActivityAt failure")
	}
	if !strings.Contains(err.Error(), "query failed") {
		t.Errorf("error = %v, want to contain 'query failed'", err)
	}
}

// ── StatusLine.String ─────────────────────────────────────────────────────────

func TestStatusLine_String_Normal(t *testing.T) {
	line := StatusLine{PRCount: 3, CIFailCount: 1, ReviewCount: 2}
	got := line.String()
	want := "↑3 PRs  ✗1 CI  ↓2 review"
	if got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestStatusLine_String_Zeros(t *testing.T) {
	line := StatusLine{}
	got := line.String()
	want := "↑0 PRs  ✗0 CI  ↓0 review"
	if got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestStatusLine_String_Stale(t *testing.T) {
	line := StatusLine{
		PRCount: 3, CIFailCount: 0, ReviewCount: 1,
		Stale: true, StaleAge: 90 * time.Minute,
	}
	got := line.String()
	if !strings.HasPrefix(got, "↑3 PRs  ✗0 CI  ↓1 review") {
		t.Errorf("String() = %q, want prefix '↑3 PRs  ✗0 CI  ↓1 review'", got)
	}
	if !strings.Contains(got, "90m ago") {
		t.Errorf("String() = %q, want to contain '90m ago'", got)
	}
}
