package ghcli

import (
	"context"
	"errors"
	"testing"
)

// stubAuditLogger records audit log calls.
type stubAuditLogger struct {
	entries []auditEntry
}

type auditEntry struct {
	action, owner, repo string
	number              int
	details             string
}

func (s *stubAuditLogger) Log(_ context.Context, action, owner, repo string, number int, details string) error {
	s.entries = append(s.entries, auditEntry{action, owner, repo, number, details})
	return nil
}

func newTestMutator(runFunc func(ctx context.Context, args []string) ([]byte, error)) (*GHCLIMutator, *StubCommandRunner, *stubAuditLogger) {
	runner := NewStubCommandRunner()
	runner.RunFunc = runFunc
	audit := &stubAuditLogger{}
	return &GHCLIMutator{Runner: runner, Audit: audit}, runner, audit
}

func successRunner(_ context.Context, _ []string) ([]byte, error) {
	return nil, nil
}

func TestGHCLIMutator_Approve(t *testing.T) {
	m, runner, audit := newTestMutator(successRunner)

	if err := m.Approve(context.Background(), "owner/repo", 42); err != nil {
		t.Fatalf("error = %v", err)
	}

	call := runner.FindCall("pr", "review", "--approve")
	if call == nil {
		t.Error("expected pr review --approve call")
	}
	if len(audit.entries) != 1 || audit.entries[0].action != "approve" {
		t.Errorf("audit = %+v", audit.entries)
	}
}

func TestGHCLIMutator_RequestReview(t *testing.T) {
	m, runner, audit := newTestMutator(successRunner)

	if err := m.RequestReview(context.Background(), "owner/repo", 42, []string{"alice", "bob"}); err != nil {
		t.Fatalf("error = %v", err)
	}

	call := runner.FindCall("--add-reviewer", "alice,bob")
	if call == nil {
		t.Error("expected --add-reviewer call")
	}
	if audit.entries[0].details != "alice, bob" {
		t.Errorf("details = %q", audit.entries[0].details)
	}
}

func TestGHCLIMutator_PostComment(t *testing.T) {
	m, runner, audit := newTestMutator(successRunner)

	if err := m.PostComment(context.Background(), "owner/repo", 42, "LGTM"); err != nil {
		t.Fatalf("error = %v", err)
	}

	call := runner.FindCall("pr", "comment", "-b", "LGTM")
	if call == nil {
		t.Error("expected pr comment -b call")
	}
	if audit.entries[0].action != "comment" {
		t.Errorf("action = %q", audit.entries[0].action)
	}
}

func TestGHCLIMutator_AddLabel(t *testing.T) {
	m, runner, _ := newTestMutator(successRunner)

	if err := m.AddLabel(context.Background(), "owner/repo", 42, "bug"); err != nil {
		t.Fatalf("error = %v", err)
	}

	call := runner.FindCall("--add-label", "bug")
	if call == nil {
		t.Error("expected --add-label call")
	}
}

func TestGHCLIMutator_RemoveLabel(t *testing.T) {
	m, runner, _ := newTestMutator(successRunner)

	if err := m.RemoveLabel(context.Background(), "owner/repo", 42, "wip"); err != nil {
		t.Fatalf("error = %v", err)
	}

	call := runner.FindCall("--remove-label", "wip")
	if call == nil {
		t.Error("expected --remove-label call")
	}
}

func TestGHCLIMutator_MergePR_Methods(t *testing.T) {
	tests := []struct {
		method  string
		wantArg string
	}{
		{"squash", "--squash"},
		{"rebase", "--rebase"},
		{"merge", "--merge"},
		{"", ""}, // default: no flag
	}

	for _, tt := range tests {
		t.Run("method="+tt.method, func(t *testing.T) {
			m, runner, _ := newTestMutator(successRunner)

			if err := m.MergePR(context.Background(), "owner/repo", 42, tt.method); err != nil {
				t.Fatalf("error = %v", err)
			}

			call := runner.FindCall("pr", "merge")
			if call == nil {
				t.Fatal("expected pr merge call")
			}
			if tt.wantArg != "" {
				if runner.FindCall(tt.wantArg) == nil {
					t.Errorf("expected %s flag in args", tt.wantArg)
				}
			}
		})
	}
}

func TestGHCLIMutator_ClosePR(t *testing.T) {
	m, runner, _ := newTestMutator(successRunner)

	if err := m.ClosePR(context.Background(), "owner/repo", 42); err != nil {
		t.Fatalf("error = %v", err)
	}

	if runner.FindCall("pr", "close") == nil {
		t.Error("expected pr close call")
	}
}

func TestGHCLIMutator_ReopenPR(t *testing.T) {
	m, runner, _ := newTestMutator(successRunner)

	if err := m.ReopenPR(context.Background(), "owner/repo", 42); err != nil {
		t.Fatalf("error = %v", err)
	}

	if runner.FindCall("pr", "reopen") == nil {
		t.Error("expected pr reopen call")
	}
}

func TestGHCLIMutator_MarkReadyForReview(t *testing.T) {
	m, runner, _ := newTestMutator(successRunner)

	if err := m.MarkReadyForReview(context.Background(), "owner/repo", 42, "GID_123"); err != nil {
		t.Fatalf("error = %v", err)
	}

	call := runner.FindCall("pr", "ready")
	if call == nil {
		t.Error("expected pr ready call")
	}
	// Should NOT have --undo
	if runner.FindCall("--undo") != nil {
		t.Error("should not have --undo flag")
	}
}

func TestGHCLIMutator_ConvertToDraft(t *testing.T) {
	m, runner, _ := newTestMutator(successRunner)

	if err := m.ConvertToDraft(context.Background(), "owner/repo", 42, "GID_123"); err != nil {
		t.Fatalf("error = %v", err)
	}

	if runner.FindCall("pr", "ready", "--undo") == nil {
		t.Error("expected pr ready --undo call")
	}
}

func TestGHCLIMutator_ResolveReviewThread(t *testing.T) {
	m, runner, _ := newTestMutator(successRunner)

	if err := m.ResolveReviewThread(context.Background(), "PRRT_abc123"); err != nil {
		t.Fatalf("error = %v", err)
	}

	call := runner.FindCall("api", "graphql")
	if call == nil {
		t.Error("expected api graphql call")
	}
}

// ── Error tests ────────────────────────────────────────────────────────────

func TestGHCLIMutator_CommandError(t *testing.T) {
	cmdErr := errors.New("gh failed")
	m, _, _ := newTestMutator(func(_ context.Context, _ []string) ([]byte, error) {
		return nil, cmdErr
	})

	methods := []struct {
		name string
		call func() error
	}{
		{"Approve", func() error { return m.Approve(context.Background(), "o/r", 1) }},
		{"RequestReview", func() error { return m.RequestReview(context.Background(), "o/r", 1, []string{"u"}) }},
		{"PostComment", func() error { return m.PostComment(context.Background(), "o/r", 1, "x") }},
		{"AddLabel", func() error { return m.AddLabel(context.Background(), "o/r", 1, "l") }},
		{"RemoveLabel", func() error { return m.RemoveLabel(context.Background(), "o/r", 1, "l") }},
		{"MergePR", func() error { return m.MergePR(context.Background(), "o/r", 1, "") }},
		{"ClosePR", func() error { return m.ClosePR(context.Background(), "o/r", 1) }},
		{"ReopenPR", func() error { return m.ReopenPR(context.Background(), "o/r", 1) }},
		{"MarkReadyForReview", func() error { return m.MarkReadyForReview(context.Background(), "o/r", 1, "g") }},
		{"ConvertToDraft", func() error { return m.ConvertToDraft(context.Background(), "o/r", 1, "g") }},
		{"ResolveReviewThread", func() error { return m.ResolveReviewThread(context.Background(), "t") }},
	}

	for _, tt := range methods {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.call()
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !errors.Is(err, cmdErr) {
				t.Errorf("error = %v, want to wrap %v", err, cmdErr)
			}
		})
	}
}

func TestGHCLIMutator_InvalidRepo(t *testing.T) {
	m, _, _ := newTestMutator(successRunner)

	methods := []struct {
		name string
		call func() error
	}{
		{"Approve", func() error { return m.Approve(context.Background(), "badrepo", 1) }},
		{"RequestReview", func() error { return m.RequestReview(context.Background(), "badrepo", 1, []string{"u"}) }},
		{"PostComment", func() error { return m.PostComment(context.Background(), "badrepo", 1, "x") }},
		{"AddLabel", func() error { return m.AddLabel(context.Background(), "badrepo", 1, "l") }},
		{"RemoveLabel", func() error { return m.RemoveLabel(context.Background(), "badrepo", 1, "l") }},
		{"MergePR", func() error { return m.MergePR(context.Background(), "badrepo", 1, "") }},
		{"ClosePR", func() error { return m.ClosePR(context.Background(), "badrepo", 1) }},
		{"ReopenPR", func() error { return m.ReopenPR(context.Background(), "badrepo", 1) }},
		{"MarkReadyForReview", func() error { return m.MarkReadyForReview(context.Background(), "badrepo", 1, "g") }},
		{"ConvertToDraft", func() error { return m.ConvertToDraft(context.Background(), "badrepo", 1, "g") }},
	}

	for _, tt := range methods {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.call(); err == nil {
				t.Fatal("expected error for invalid repo, got nil")
			}
		})
	}
}

func TestSplitRepo(t *testing.T) {
	tests := []struct {
		repo      string
		wantOwner string
		wantName  string
		wantErr   bool
	}{
		{"owner/repo", "owner", "repo", false},
		{"org/my-project", "org", "my-project", false},
		{"badrepo", "", "", true},
		{"/repo", "", "", true},
		{"owner/", "", "", true},
		{"", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.repo, func(t *testing.T) {
			owner, name, err := splitRepo(tt.repo)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if owner != tt.wantOwner || name != tt.wantName {
				t.Errorf("splitRepo(%q) = (%q, %q), want (%q, %q)", tt.repo, owner, name, tt.wantOwner, tt.wantName)
			}
		})
	}
}

func TestGHCLIMutator_AuditLogging(t *testing.T) {
	m, _, audit := newTestMutator(successRunner)

	m.Approve(context.Background(), "owner/repo", 42)
	m.MergePR(context.Background(), "owner/repo", 42, "squash")

	if len(audit.entries) != 2 {
		t.Fatalf("expected 2 audit entries, got %d", len(audit.entries))
	}

	entry := audit.entries[0]
	if entry.owner != "owner" || entry.repo != "repo" || entry.number != 42 {
		t.Errorf("audit[0] = %+v", entry)
	}

	entry = audit.entries[1]
	if entry.action != "merge" || entry.details != "squash" {
		t.Errorf("audit[1] = %+v", entry)
	}
}
