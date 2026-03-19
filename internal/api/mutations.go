package api

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/go-github/v69/github"
	"github.com/shurcooL/githubv4"
)

// AuditLogger records mutations to the audit log.
type AuditLogger interface {
	Log(ctx context.Context, action, owner, repo string, number int, details string) error
}

// PullRequestsService abstracts GitHub REST write operations on pull requests.
type PullRequestsService interface {
	CreateReview(ctx context.Context, owner, repo string, number int, review *github.PullRequestReviewRequest) (*github.PullRequestReview, *github.Response, error)
	RequestReviewers(ctx context.Context, owner, repo string, number int, reviewers github.ReviewersRequest) (*github.PullRequest, *github.Response, error)
	Merge(ctx context.Context, owner, repo string, number int, commitMessage string, options *github.PullRequestOptions) (*github.PullRequestMergeResult, *github.Response, error)
}

// IssuesService abstracts GitHub REST write operations on issues (PRs share the issues API).
type IssuesService interface {
	CreateComment(ctx context.Context, owner, repo string, number int, comment *github.IssueComment) (*github.IssueComment, *github.Response, error)
	AddLabelsToIssue(ctx context.Context, owner, repo string, number int, labels []string) ([]*github.Label, *github.Response, error)
	RemoveLabelForIssue(ctx context.Context, owner, repo string, number int, label string) (*github.Response, error)
	Edit(ctx context.Context, owner, repo string, number int, issue *github.IssueRequest) (*github.Issue, *github.Response, error)
}

// GraphQLMutator executes GitHub GraphQL mutations.
type GraphQLMutator interface {
	Mutate(ctx context.Context, m interface{}, input githubv4.Input, variables map[string]interface{}) error
}

// Mutator implements all PR write operations via REST and GraphQL.
type Mutator struct {
	prs    PullRequestsService
	issues IssuesService
	gql    GraphQLMutator
	audit  AuditLogger
}

// NewMutator returns a new Mutator.
func NewMutator(prs PullRequestsService, issues IssuesService, gql GraphQLMutator, audit AuditLogger) *Mutator {
	return &Mutator{prs: prs, issues: issues, gql: gql, audit: audit}
}

// parseRepo splits "owner/repo" into its component parts.
func parseRepo(repo string) (owner, name string, err error) {
	parts := strings.SplitN(repo, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid repo %q: expected owner/name", repo)
	}
	return parts[0], parts[1], nil
}

// Approve submits an APPROVE review on the given PR.
func (m *Mutator) Approve(ctx context.Context, repo string, number int) error {
	owner, name, err := parseRepo(repo)
	if err != nil {
		return err
	}
	event := "APPROVE"
	if _, _, err = m.prs.CreateReview(ctx, owner, name, number, &github.PullRequestReviewRequest{
		Event: &event,
	}); err != nil {
		return fmt.Errorf("approving PR %s#%d: %w", repo, number, err)
	}
	return m.audit.Log(ctx, "approve", owner, name, number, "")
}

// RequestReview requests reviews from the given users on the given PR.
func (m *Mutator) RequestReview(ctx context.Context, repo string, number int, users []string) error {
	owner, name, err := parseRepo(repo)
	if err != nil {
		return err
	}
	if _, _, err = m.prs.RequestReviewers(ctx, owner, name, number, github.ReviewersRequest{
		Reviewers: users,
	}); err != nil {
		return fmt.Errorf("requesting reviewers for PR %s#%d: %w", repo, number, err)
	}
	return m.audit.Log(ctx, "request-review", owner, name, number, strings.Join(users, ","))
}

// PostComment posts a comment on the given PR.
func (m *Mutator) PostComment(ctx context.Context, repo string, number int, body string) error {
	owner, name, err := parseRepo(repo)
	if err != nil {
		return err
	}
	if _, _, err = m.issues.CreateComment(ctx, owner, name, number, &github.IssueComment{
		Body: &body,
	}); err != nil {
		return fmt.Errorf("posting comment on PR %s#%d: %w", repo, number, err)
	}
	return m.audit.Log(ctx, "comment", owner, name, number, body)
}

// AddLabel adds a label to the given PR.
func (m *Mutator) AddLabel(ctx context.Context, repo string, number int, label string) error {
	owner, name, err := parseRepo(repo)
	if err != nil {
		return err
	}
	if _, _, err = m.issues.AddLabelsToIssue(ctx, owner, name, number, []string{label}); err != nil {
		return fmt.Errorf("adding label %q to PR %s#%d: %w", label, repo, number, err)
	}
	return m.audit.Log(ctx, "label-add", owner, name, number, label)
}

// RemoveLabel removes a label from the given PR.
func (m *Mutator) RemoveLabel(ctx context.Context, repo string, number int, label string) error {
	owner, name, err := parseRepo(repo)
	if err != nil {
		return err
	}
	if _, err = m.issues.RemoveLabelForIssue(ctx, owner, name, number, label); err != nil {
		return fmt.Errorf("removing label %q from PR %s#%d: %w", label, repo, number, err)
	}
	return m.audit.Log(ctx, "label-remove", owner, name, number, label)
}

// MergePR merges the given PR.
// method may be "squash", "merge", "rebase", or "" to use the repo default.
func (m *Mutator) MergePR(ctx context.Context, repo string, number int, method string) error {
	owner, name, err := parseRepo(repo)
	if err != nil {
		return err
	}
	opts := &github.PullRequestOptions{}
	if method != "" {
		opts.MergeMethod = method
	}
	if _, _, err = m.prs.Merge(ctx, owner, name, number, "", opts); err != nil {
		return fmt.Errorf("merging PR %s#%d: %w", repo, number, err)
	}
	return m.audit.Log(ctx, "merge", owner, name, number, method)
}

// ClosePR closes the given PR.
func (m *Mutator) ClosePR(ctx context.Context, repo string, number int) error {
	owner, name, err := parseRepo(repo)
	if err != nil {
		return err
	}
	state := "closed"
	if _, _, err = m.issues.Edit(ctx, owner, name, number, &github.IssueRequest{
		State: &state,
	}); err != nil {
		return fmt.Errorf("closing PR %s#%d: %w", repo, number, err)
	}
	return m.audit.Log(ctx, "close", owner, name, number, "")
}

// ReopenPR reopens the given PR.
func (m *Mutator) ReopenPR(ctx context.Context, repo string, number int) error {
	owner, name, err := parseRepo(repo)
	if err != nil {
		return err
	}
	state := "open"
	if _, _, err = m.issues.Edit(ctx, owner, name, number, &github.IssueRequest{
		State: &state,
	}); err != nil {
		return fmt.Errorf("reopening PR %s#%d: %w", repo, number, err)
	}
	return m.audit.Log(ctx, "reopen", owner, name, number, "")
}

// ── GraphQL mutation input structs ────────────────────────────────────────────

type markReadyForReviewInput struct {
	PullRequestID githubv4.ID `json:"pullRequestId"`
}

type convertToDraftInput struct {
	PullRequestID githubv4.ID `json:"pullRequestId"`
}

type resolveReviewThreadInput struct {
	ThreadID githubv4.ID `json:"threadId"`
}

// ── GraphQL mutations ─────────────────────────────────────────────────────────

// MarkReadyForReview marks a draft PR as ready for review via GraphQL mutation.
// globalID is the PR's node ID (GlobalID field in the persistence layer).
func (m *Mutator) MarkReadyForReview(ctx context.Context, repo string, number int, globalID string) error {
	owner, name, err := parseRepo(repo)
	if err != nil {
		return err
	}
	var mutation struct {
		MarkPullRequestReadyForReview struct {
			PullRequest struct {
				ID githubv4.ID
			}
		} `graphql:"markPullRequestReadyForReview(input: $input)"`
	}
	if err = m.gql.Mutate(ctx, &mutation, markReadyForReviewInput{
		PullRequestID: githubv4.ID(globalID),
	}, nil); err != nil {
		return fmt.Errorf("marking PR %s#%d ready for review: %w", repo, number, err)
	}
	return m.audit.Log(ctx, "ready-for-review", owner, name, number, "")
}

// ConvertToDraft converts a PR to a draft via GraphQL mutation.
// globalID is the PR's node ID (GlobalID field in the persistence layer).
func (m *Mutator) ConvertToDraft(ctx context.Context, repo string, number int, globalID string) error {
	owner, name, err := parseRepo(repo)
	if err != nil {
		return err
	}
	var mutation struct {
		ConvertPullRequestToDraft struct {
			PullRequest struct {
				ID githubv4.ID
			}
		} `graphql:"convertPullRequestToDraft(input: $input)"`
	}
	if err = m.gql.Mutate(ctx, &mutation, convertToDraftInput{
		PullRequestID: githubv4.ID(globalID),
	}, nil); err != nil {
		return fmt.Errorf("converting PR %s#%d to draft: %w", repo, number, err)
	}
	return m.audit.Log(ctx, "convert-to-draft", owner, name, number, "")
}

// ResolveReviewThread marks a review thread as resolved via GraphQL mutation.
func (m *Mutator) ResolveReviewThread(ctx context.Context, threadID string) error {
	var mutation struct {
		ResolveReviewThread struct {
			Thread struct {
				ID githubv4.ID
			}
		} `graphql:"resolveReviewThread(input: $input)"`
	}
	if err := m.gql.Mutate(ctx, &mutation, resolveReviewThreadInput{
		ThreadID: githubv4.ID(threadID),
	}, nil); err != nil {
		return fmt.Errorf("resolving review thread %s: %w", threadID, err)
	}
	return m.audit.Log(ctx, "resolve-thread", "", "", 0, threadID)
}
