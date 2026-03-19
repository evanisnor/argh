package api

import (
	"context"
	"errors"
	"testing"

	"github.com/google/go-github/v69/github"
	"github.com/shurcooL/githubv4"
)

// ── parseRepo ─────────────────────────────────────────────────────────────────

func TestParseRepo(t *testing.T) {
	tests := []struct {
		name      string
		repo      string
		wantOwner string
		wantName  string
		wantErr   bool
	}{
		{name: "valid", repo: "owner/repo", wantOwner: "owner", wantName: "repo"},
		{name: "no slash", repo: "ownerrepo", wantErr: true},
		{name: "empty owner", repo: "/repo", wantErr: true},
		{name: "empty name", repo: "owner/", wantErr: true},
		{name: "empty string", repo: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, name, err := parseRepo(tt.repo)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseRepo(%q) error = %v, wantErr %v", tt.repo, err, tt.wantErr)
			}
			if !tt.wantErr {
				if owner != tt.wantOwner {
					t.Errorf("owner = %q, want %q", owner, tt.wantOwner)
				}
				if name != tt.wantName {
					t.Errorf("name = %q, want %q", name, tt.wantName)
				}
			}
		})
	}
}

// ── helpers ────────────────────────────────────────────────────────────────────

func newMutator(prs *StubPullRequestsService, issues *StubIssuesService, gql *StubGraphQLMutator, audit *StubAuditLogger) *Mutator {
	return NewMutator(prs, issues, gql, audit)
}

// ── Approve ──────────────────────────────────────────────────────────────────

func TestMutator_Approve_Success(t *testing.T) {
	var gotOwner, gotRepo string
	var gotNumber int
	var gotEvent string

	prs := NewStubPullRequestsService()
	prs.CreateReviewFunc = func(_ context.Context, owner, repo string, number int, review *github.PullRequestReviewRequest) (*github.PullRequestReview, *github.Response, error) {
		gotOwner = owner
		gotRepo = repo
		gotNumber = number
		gotEvent = review.GetEvent()
		return nil, nil, nil
	}
	audit := NewStubAuditLogger()
	m := newMutator(prs, NewStubIssuesService(), NewStubGraphQLMutator(), audit)

	if err := m.Approve(context.Background(), "owner/repo", 42); err != nil {
		t.Fatalf("Approve() error = %v", err)
	}
	if gotOwner != "owner" || gotRepo != "repo" || gotNumber != 42 {
		t.Errorf("CreateReview called with owner=%q repo=%q number=%d", gotOwner, gotRepo, gotNumber)
	}
	if gotEvent != "APPROVE" {
		t.Errorf("review event = %q, want APPROVE", gotEvent)
	}
	if len(audit.Entries) != 1 || audit.Entries[0].Action != "approve" {
		t.Errorf("audit entries = %v, want 1 approve entry", audit.Entries)
	}
}

func TestMutator_Approve_APIError(t *testing.T) {
	apiErr := errors.New("api error")
	prs := NewStubPullRequestsService()
	prs.CreateReviewFunc = func(_ context.Context, _, _ string, _ int, _ *github.PullRequestReviewRequest) (*github.PullRequestReview, *github.Response, error) {
		return nil, nil, apiErr
	}
	audit := NewStubAuditLogger()
	m := newMutator(prs, NewStubIssuesService(), NewStubGraphQLMutator(), audit)

	err := m.Approve(context.Background(), "owner/repo", 42)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if len(audit.Entries) != 0 {
		t.Errorf("audit should not be written on API error, got %d entries", len(audit.Entries))
	}
}

// ── RequestReview ─────────────────────────────────────────────────────────────

func TestMutator_RequestReview_Success(t *testing.T) {
	var gotReviewers github.ReviewersRequest
	prs := NewStubPullRequestsService()
	prs.RequestReviewersFunc = func(_ context.Context, _, _ string, _ int, r github.ReviewersRequest) (*github.PullRequest, *github.Response, error) {
		gotReviewers = r
		return nil, nil, nil
	}
	audit := NewStubAuditLogger()
	m := newMutator(prs, NewStubIssuesService(), NewStubGraphQLMutator(), audit)

	if err := m.RequestReview(context.Background(), "owner/repo", 1, []string{"alice", "bob"}); err != nil {
		t.Fatalf("RequestReview() error = %v", err)
	}
	if len(gotReviewers.Reviewers) != 2 || gotReviewers.Reviewers[0] != "alice" || gotReviewers.Reviewers[1] != "bob" {
		t.Errorf("reviewers = %v", gotReviewers.Reviewers)
	}
	if len(audit.Entries) != 1 || audit.Entries[0].Action != "request-review" {
		t.Errorf("audit entries = %v", audit.Entries)
	}
}

func TestMutator_RequestReview_APIError(t *testing.T) {
	prs := NewStubPullRequestsService()
	prs.RequestReviewersFunc = func(_ context.Context, _, _ string, _ int, _ github.ReviewersRequest) (*github.PullRequest, *github.Response, error) {
		return nil, nil, errors.New("api error")
	}
	audit := NewStubAuditLogger()
	m := newMutator(prs, NewStubIssuesService(), NewStubGraphQLMutator(), audit)

	if err := m.RequestReview(context.Background(), "owner/repo", 1, []string{"alice"}); err == nil {
		t.Fatal("expected error")
	}
	if len(audit.Entries) != 0 {
		t.Errorf("audit should not be written on API error")
	}
}

// ── PostComment ───────────────────────────────────────────────────────────────

func TestMutator_PostComment_Success(t *testing.T) {
	var gotBody string
	issues := NewStubIssuesService()
	issues.CreateCommentFunc = func(_ context.Context, _, _ string, _ int, comment *github.IssueComment) (*github.IssueComment, *github.Response, error) {
		gotBody = comment.GetBody()
		return nil, nil, nil
	}
	audit := NewStubAuditLogger()
	m := newMutator(NewStubPullRequestsService(), issues, NewStubGraphQLMutator(), audit)

	if err := m.PostComment(context.Background(), "owner/repo", 5, "hello world"); err != nil {
		t.Fatalf("PostComment() error = %v", err)
	}
	if gotBody != "hello world" {
		t.Errorf("comment body = %q, want %q", gotBody, "hello world")
	}
	if len(audit.Entries) != 1 || audit.Entries[0].Action != "comment" {
		t.Errorf("audit entries = %v", audit.Entries)
	}
}

func TestMutator_PostComment_APIError(t *testing.T) {
	issues := NewStubIssuesService()
	issues.CreateCommentFunc = func(_ context.Context, _, _ string, _ int, _ *github.IssueComment) (*github.IssueComment, *github.Response, error) {
		return nil, nil, errors.New("api error")
	}
	audit := NewStubAuditLogger()
	m := newMutator(NewStubPullRequestsService(), issues, NewStubGraphQLMutator(), audit)

	if err := m.PostComment(context.Background(), "owner/repo", 5, "hello"); err == nil {
		t.Fatal("expected error")
	}
	if len(audit.Entries) != 0 {
		t.Errorf("audit should not be written on API error")
	}
}

// ── AddLabel ─────────────────────────────────────────────────────────────────

func TestMutator_AddLabel_Success(t *testing.T) {
	var gotLabels []string
	issues := NewStubIssuesService()
	issues.AddLabelsToIssueFunc = func(_ context.Context, _, _ string, _ int, labels []string) ([]*github.Label, *github.Response, error) {
		gotLabels = labels
		return nil, nil, nil
	}
	audit := NewStubAuditLogger()
	m := newMutator(NewStubPullRequestsService(), issues, NewStubGraphQLMutator(), audit)

	if err := m.AddLabel(context.Background(), "owner/repo", 10, "bug"); err != nil {
		t.Fatalf("AddLabel() error = %v", err)
	}
	if len(gotLabels) != 1 || gotLabels[0] != "bug" {
		t.Errorf("labels = %v", gotLabels)
	}
	if len(audit.Entries) != 1 || audit.Entries[0].Action != "label-add" || audit.Entries[0].Details != "bug" {
		t.Errorf("audit entries = %v", audit.Entries)
	}
}

func TestMutator_AddLabel_APIError(t *testing.T) {
	issues := NewStubIssuesService()
	issues.AddLabelsToIssueFunc = func(_ context.Context, _, _ string, _ int, _ []string) ([]*github.Label, *github.Response, error) {
		return nil, nil, errors.New("api error")
	}
	audit := NewStubAuditLogger()
	m := newMutator(NewStubPullRequestsService(), issues, NewStubGraphQLMutator(), audit)

	if err := m.AddLabel(context.Background(), "owner/repo", 10, "bug"); err == nil {
		t.Fatal("expected error")
	}
	if len(audit.Entries) != 0 {
		t.Errorf("audit should not be written on API error")
	}
}

// ── RemoveLabel ───────────────────────────────────────────────────────────────

func TestMutator_RemoveLabel_Success(t *testing.T) {
	var gotLabel string
	issues := NewStubIssuesService()
	issues.RemoveLabelForIssueFunc = func(_ context.Context, _, _ string, _ int, label string) (*github.Response, error) {
		gotLabel = label
		return nil, nil
	}
	audit := NewStubAuditLogger()
	m := newMutator(NewStubPullRequestsService(), issues, NewStubGraphQLMutator(), audit)

	if err := m.RemoveLabel(context.Background(), "owner/repo", 3, "wip"); err != nil {
		t.Fatalf("RemoveLabel() error = %v", err)
	}
	if gotLabel != "wip" {
		t.Errorf("label = %q, want wip", gotLabel)
	}
	if len(audit.Entries) != 1 || audit.Entries[0].Action != "label-remove" {
		t.Errorf("audit entries = %v", audit.Entries)
	}
}

func TestMutator_RemoveLabel_APIError(t *testing.T) {
	issues := NewStubIssuesService()
	issues.RemoveLabelForIssueFunc = func(_ context.Context, _, _ string, _ int, _ string) (*github.Response, error) {
		return nil, errors.New("api error")
	}
	audit := NewStubAuditLogger()
	m := newMutator(NewStubPullRequestsService(), issues, NewStubGraphQLMutator(), audit)

	if err := m.RemoveLabel(context.Background(), "owner/repo", 3, "wip"); err == nil {
		t.Fatal("expected error")
	}
	if len(audit.Entries) != 0 {
		t.Errorf("audit should not be written on API error")
	}
}

// ── MergePR ───────────────────────────────────────────────────────────────────

func TestMutator_MergePR(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		wantMethod string
	}{
		{name: "squash", method: "squash", wantMethod: "squash"},
		{name: "merge", method: "merge", wantMethod: "merge"},
		{name: "rebase", method: "rebase", wantMethod: "rebase"},
		{name: "default (empty method)", method: "", wantMethod: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotMethod string
			prs := NewStubPullRequestsService()
			prs.MergeFunc = func(_ context.Context, _, _ string, _ int, _ string, opts *github.PullRequestOptions) (*github.PullRequestMergeResult, *github.Response, error) {
				gotMethod = opts.MergeMethod
				return nil, nil, nil
			}
			audit := NewStubAuditLogger()
			m := newMutator(prs, NewStubIssuesService(), NewStubGraphQLMutator(), audit)

			if err := m.MergePR(context.Background(), "owner/repo", 7, tt.method); err != nil {
				t.Fatalf("MergePR() error = %v", err)
			}
			if gotMethod != tt.wantMethod {
				t.Errorf("merge method = %q, want %q", gotMethod, tt.wantMethod)
			}
			if len(audit.Entries) != 1 || audit.Entries[0].Action != "merge" {
				t.Errorf("audit entries = %v", audit.Entries)
			}
		})
	}
}

func TestMutator_MergePR_APIError(t *testing.T) {
	prs := NewStubPullRequestsService()
	prs.MergeFunc = func(_ context.Context, _, _ string, _ int, _ string, _ *github.PullRequestOptions) (*github.PullRequestMergeResult, *github.Response, error) {
		return nil, nil, errors.New("api error")
	}
	audit := NewStubAuditLogger()
	m := newMutator(prs, NewStubIssuesService(), NewStubGraphQLMutator(), audit)

	if err := m.MergePR(context.Background(), "owner/repo", 7, "squash"); err == nil {
		t.Fatal("expected error")
	}
	if len(audit.Entries) != 0 {
		t.Errorf("audit should not be written on API error")
	}
}

// ── ClosePR / ReopenPR ────────────────────────────────────────────────────────

func TestMutator_ClosePR_Success(t *testing.T) {
	var gotState string
	issues := NewStubIssuesService()
	issues.EditFunc = func(_ context.Context, _, _ string, _ int, req *github.IssueRequest) (*github.Issue, *github.Response, error) {
		gotState = req.GetState()
		return nil, nil, nil
	}
	audit := NewStubAuditLogger()
	m := newMutator(NewStubPullRequestsService(), issues, NewStubGraphQLMutator(), audit)

	if err := m.ClosePR(context.Background(), "owner/repo", 9); err != nil {
		t.Fatalf("ClosePR() error = %v", err)
	}
	if gotState != "closed" {
		t.Errorf("state = %q, want closed", gotState)
	}
	if len(audit.Entries) != 1 || audit.Entries[0].Action != "close" {
		t.Errorf("audit entries = %v", audit.Entries)
	}
}

func TestMutator_ClosePR_APIError(t *testing.T) {
	issues := NewStubIssuesService()
	issues.EditFunc = func(_ context.Context, _, _ string, _ int, _ *github.IssueRequest) (*github.Issue, *github.Response, error) {
		return nil, nil, errors.New("api error")
	}
	audit := NewStubAuditLogger()
	m := newMutator(NewStubPullRequestsService(), issues, NewStubGraphQLMutator(), audit)

	if err := m.ClosePR(context.Background(), "owner/repo", 9); err == nil {
		t.Fatal("expected error")
	}
	if len(audit.Entries) != 0 {
		t.Errorf("audit should not be written on API error")
	}
}

func TestMutator_ReopenPR_Success(t *testing.T) {
	var gotState string
	issues := NewStubIssuesService()
	issues.EditFunc = func(_ context.Context, _, _ string, _ int, req *github.IssueRequest) (*github.Issue, *github.Response, error) {
		gotState = req.GetState()
		return nil, nil, nil
	}
	audit := NewStubAuditLogger()
	m := newMutator(NewStubPullRequestsService(), issues, NewStubGraphQLMutator(), audit)

	if err := m.ReopenPR(context.Background(), "owner/repo", 9); err != nil {
		t.Fatalf("ReopenPR() error = %v", err)
	}
	if gotState != "open" {
		t.Errorf("state = %q, want open", gotState)
	}
	if len(audit.Entries) != 1 || audit.Entries[0].Action != "reopen" {
		t.Errorf("audit entries = %v", audit.Entries)
	}
}

func TestMutator_ReopenPR_APIError(t *testing.T) {
	issues := NewStubIssuesService()
	issues.EditFunc = func(_ context.Context, _, _ string, _ int, _ *github.IssueRequest) (*github.Issue, *github.Response, error) {
		return nil, nil, errors.New("api error")
	}
	audit := NewStubAuditLogger()
	m := newMutator(NewStubPullRequestsService(), issues, NewStubGraphQLMutator(), audit)

	if err := m.ReopenPR(context.Background(), "owner/repo", 9); err == nil {
		t.Fatal("expected error")
	}
	if len(audit.Entries) != 0 {
		t.Errorf("audit should not be written on API error")
	}
}

// ── MarkReadyForReview ────────────────────────────────────────────────────────

func TestMutator_MarkReadyForReview_Success(t *testing.T) {
	var gotInput githubv4.Input
	gql := NewStubGraphQLMutator()
	gql.MutateFunc = func(_ context.Context, _ interface{}, input githubv4.Input, _ map[string]interface{}) error {
		gotInput = input
		return nil
	}
	audit := NewStubAuditLogger()
	m := newMutator(NewStubPullRequestsService(), NewStubIssuesService(), gql, audit)

	if err := m.MarkReadyForReview(context.Background(), "owner/repo", 11, "PR_global_id"); err != nil {
		t.Fatalf("MarkReadyForReview() error = %v", err)
	}
	in, ok := gotInput.(markReadyForReviewInput)
	if !ok {
		t.Fatalf("input type = %T, want markReadyForReviewInput", gotInput)
	}
	if in.PullRequestID != githubv4.ID("PR_global_id") {
		t.Errorf("PullRequestID = %v, want PR_global_id", in.PullRequestID)
	}
	if len(audit.Entries) != 1 || audit.Entries[0].Action != "ready-for-review" {
		t.Errorf("audit entries = %v", audit.Entries)
	}
}

func TestMutator_MarkReadyForReview_APIError(t *testing.T) {
	gql := NewStubGraphQLMutator()
	gql.MutateFunc = func(_ context.Context, _ interface{}, _ githubv4.Input, _ map[string]interface{}) error {
		return errors.New("graphql error")
	}
	audit := NewStubAuditLogger()
	m := newMutator(NewStubPullRequestsService(), NewStubIssuesService(), gql, audit)

	if err := m.MarkReadyForReview(context.Background(), "owner/repo", 11, "id"); err == nil {
		t.Fatal("expected error")
	}
	if len(audit.Entries) != 0 {
		t.Errorf("audit should not be written on API error")
	}
}

// ── ConvertToDraft ─────────────────────────────────────────────────────────────

func TestMutator_ConvertToDraft_Success(t *testing.T) {
	var gotInput githubv4.Input
	gql := NewStubGraphQLMutator()
	gql.MutateFunc = func(_ context.Context, _ interface{}, input githubv4.Input, _ map[string]interface{}) error {
		gotInput = input
		return nil
	}
	audit := NewStubAuditLogger()
	m := newMutator(NewStubPullRequestsService(), NewStubIssuesService(), gql, audit)

	if err := m.ConvertToDraft(context.Background(), "owner/repo", 12, "PR_node_id"); err != nil {
		t.Fatalf("ConvertToDraft() error = %v", err)
	}
	in, ok := gotInput.(convertToDraftInput)
	if !ok {
		t.Fatalf("input type = %T, want convertToDraftInput", gotInput)
	}
	if in.PullRequestID != githubv4.ID("PR_node_id") {
		t.Errorf("PullRequestID = %v, want PR_node_id", in.PullRequestID)
	}
	if len(audit.Entries) != 1 || audit.Entries[0].Action != "convert-to-draft" {
		t.Errorf("audit entries = %v", audit.Entries)
	}
}

func TestMutator_ConvertToDraft_APIError(t *testing.T) {
	gql := NewStubGraphQLMutator()
	gql.MutateFunc = func(_ context.Context, _ interface{}, _ githubv4.Input, _ map[string]interface{}) error {
		return errors.New("graphql error")
	}
	audit := NewStubAuditLogger()
	m := newMutator(NewStubPullRequestsService(), NewStubIssuesService(), gql, audit)

	if err := m.ConvertToDraft(context.Background(), "owner/repo", 12, "id"); err == nil {
		t.Fatal("expected error")
	}
	if len(audit.Entries) != 0 {
		t.Errorf("audit should not be written on API error")
	}
}

// ── ResolveReviewThread ────────────────────────────────────────────────────────

func TestMutator_ResolveReviewThread_Success(t *testing.T) {
	var gotInput githubv4.Input
	gql := NewStubGraphQLMutator()
	gql.MutateFunc = func(_ context.Context, _ interface{}, input githubv4.Input, _ map[string]interface{}) error {
		gotInput = input
		return nil
	}
	audit := NewStubAuditLogger()
	m := newMutator(NewStubPullRequestsService(), NewStubIssuesService(), gql, audit)

	if err := m.ResolveReviewThread(context.Background(), "thread_abc"); err != nil {
		t.Fatalf("ResolveReviewThread() error = %v", err)
	}
	in, ok := gotInput.(resolveReviewThreadInput)
	if !ok {
		t.Fatalf("input type = %T, want resolveReviewThreadInput", gotInput)
	}
	if in.ThreadID != githubv4.ID("thread_abc") {
		t.Errorf("ThreadID = %v, want thread_abc", in.ThreadID)
	}
	if len(audit.Entries) != 1 || audit.Entries[0].Action != "resolve-thread" || audit.Entries[0].Details != "thread_abc" {
		t.Errorf("audit entries = %v", audit.Entries)
	}
}

func TestMutator_ResolveReviewThread_APIError(t *testing.T) {
	gql := NewStubGraphQLMutator()
	gql.MutateFunc = func(_ context.Context, _ interface{}, _ githubv4.Input, _ map[string]interface{}) error {
		return errors.New("graphql error")
	}
	audit := NewStubAuditLogger()
	m := newMutator(NewStubPullRequestsService(), NewStubIssuesService(), gql, audit)

	if err := m.ResolveReviewThread(context.Background(), "thread_abc"); err == nil {
		t.Fatal("expected error")
	}
	if len(audit.Entries) != 0 {
		t.Errorf("audit should not be written on API error")
	}
}

// ── stub defaults ─────────────────────────────────────────────────────────────

// TestStubDefaults verifies that stub constructors return no-op implementations
// that do not panic and return nil errors when no function override is set.
func TestStubDefaults(t *testing.T) {
	ctx := context.Background()

	prs := NewStubPullRequestsService()
	if _, _, err := prs.CreateReview(ctx, "", "", 0, nil); err != nil {
		t.Fatalf("default CreateReview: %v", err)
	}
	if _, _, err := prs.RequestReviewers(ctx, "", "", 0, github.ReviewersRequest{}); err != nil {
		t.Fatalf("default RequestReviewers: %v", err)
	}
	if _, _, err := prs.Merge(ctx, "", "", 0, "", nil); err != nil {
		t.Fatalf("default Merge: %v", err)
	}

	issues := NewStubIssuesService()
	if _, _, err := issues.CreateComment(ctx, "", "", 0, nil); err != nil {
		t.Fatalf("default CreateComment: %v", err)
	}
	if _, _, err := issues.AddLabelsToIssue(ctx, "", "", 0, nil); err != nil {
		t.Fatalf("default AddLabelsToIssue: %v", err)
	}
	if _, err := issues.RemoveLabelForIssue(ctx, "", "", 0, ""); err != nil {
		t.Fatalf("default RemoveLabelForIssue: %v", err)
	}
	if _, _, err := issues.Edit(ctx, "", "", 0, nil); err != nil {
		t.Fatalf("default Edit: %v", err)
	}

	gql := NewStubGraphQLMutator()
	if err := gql.Mutate(ctx, nil, nil, nil); err != nil {
		t.Fatalf("default Mutate: %v", err)
	}

	audit := NewStubAuditLogger()
	if err := audit.Log(ctx, "", "", "", 0, ""); err != nil {
		t.Fatalf("default Log: %v", err)
	}
}

// ── invalid repo format ───────────────────────────────────────────────────────

func TestMutator_InvalidRepo(t *testing.T) {
	m := newMutator(NewStubPullRequestsService(), NewStubIssuesService(), NewStubGraphQLMutator(), NewStubAuditLogger())
	ctx := context.Background()

	if err := m.Approve(ctx, "badrepo", 1); err == nil {
		t.Error("Approve with bad repo: expected error")
	}
	if err := m.RequestReview(ctx, "badrepo", 1, []string{"alice"}); err == nil {
		t.Error("RequestReview with bad repo: expected error")
	}
	if err := m.PostComment(ctx, "badrepo", 1, "hi"); err == nil {
		t.Error("PostComment with bad repo: expected error")
	}
	if err := m.AddLabel(ctx, "badrepo", 1, "bug"); err == nil {
		t.Error("AddLabel with bad repo: expected error")
	}
	if err := m.RemoveLabel(ctx, "badrepo", 1, "bug"); err == nil {
		t.Error("RemoveLabel with bad repo: expected error")
	}
	if err := m.MergePR(ctx, "badrepo", 1, ""); err == nil {
		t.Error("MergePR with bad repo: expected error")
	}
	if err := m.ClosePR(ctx, "badrepo", 1); err == nil {
		t.Error("ClosePR with bad repo: expected error")
	}
	if err := m.ReopenPR(ctx, "badrepo", 1); err == nil {
		t.Error("ReopenPR with bad repo: expected error")
	}
	if err := m.MarkReadyForReview(ctx, "badrepo", 1, "id"); err == nil {
		t.Error("MarkReadyForReview with bad repo: expected error")
	}
	if err := m.ConvertToDraft(ctx, "badrepo", 1, "id"); err == nil {
		t.Error("ConvertToDraft with bad repo: expected error")
	}
}
