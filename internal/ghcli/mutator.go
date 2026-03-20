package ghcli

import (
	"context"
	"fmt"
	"strings"
)

// AuditLogger logs mutation actions for audit trail.
type AuditLogger interface {
	Log(ctx context.Context, action, owner, repo string, number int, details string) error
}

// GHCLIMutator implements PRMutator, ActionExecutor, and ThreadResolver
// by delegating all mutations to the gh CLI.
type GHCLIMutator struct {
	Runner CommandRunner
	Audit  AuditLogger
}

func (m *GHCLIMutator) Approve(ctx context.Context, repo string, number int) error {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return err
	}
	if _, err := m.Runner.Run(ctx, []string{"pr", "review", fmt.Sprint(number), "--approve", "-R", repo}); err != nil {
		return fmt.Errorf("approving PR %s#%d: %w", repo, number, err)
	}
	m.Audit.Log(ctx, "approve", owner, name, number, "")
	return nil
}

func (m *GHCLIMutator) RequestReview(ctx context.Context, repo string, number int, users []string) error {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return err
	}
	userList := strings.Join(users, ",")
	if _, err := m.Runner.Run(ctx, []string{"pr", "edit", fmt.Sprint(number), "--add-reviewer", userList, "-R", repo}); err != nil {
		return fmt.Errorf("requesting review on %s#%d: %w", repo, number, err)
	}
	m.Audit.Log(ctx, "request_review", owner, name, number, strings.Join(users, ", "))
	return nil
}

func (m *GHCLIMutator) PostComment(ctx context.Context, repo string, number int, body string) error {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return err
	}
	if _, err := m.Runner.Run(ctx, []string{"pr", "comment", fmt.Sprint(number), "-b", body, "-R", repo}); err != nil {
		return fmt.Errorf("commenting on %s#%d: %w", repo, number, err)
	}
	m.Audit.Log(ctx, "comment", owner, name, number, body)
	return nil
}

func (m *GHCLIMutator) AddLabel(ctx context.Context, repo string, number int, label string) error {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return err
	}
	if _, err := m.Runner.Run(ctx, []string{"pr", "edit", fmt.Sprint(number), "--add-label", label, "-R", repo}); err != nil {
		return fmt.Errorf("adding label to %s#%d: %w", repo, number, err)
	}
	m.Audit.Log(ctx, "add_label", owner, name, number, label)
	return nil
}

func (m *GHCLIMutator) RemoveLabel(ctx context.Context, repo string, number int, label string) error {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return err
	}
	if _, err := m.Runner.Run(ctx, []string{"pr", "edit", fmt.Sprint(number), "--remove-label", label, "-R", repo}); err != nil {
		return fmt.Errorf("removing label from %s#%d: %w", repo, number, err)
	}
	m.Audit.Log(ctx, "remove_label", owner, name, number, label)
	return nil
}

func (m *GHCLIMutator) MergePR(ctx context.Context, repo string, number int, method string) error {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return err
	}
	args := []string{"pr", "merge", fmt.Sprint(number), "-R", repo}
	switch method {
	case "squash":
		args = append(args, "--squash")
	case "rebase":
		args = append(args, "--rebase")
	case "merge":
		args = append(args, "--merge")
	default:
		// No flag — use repo default.
	}
	if _, err := m.Runner.Run(ctx, args); err != nil {
		return fmt.Errorf("merging %s#%d: %w", repo, number, err)
	}
	m.Audit.Log(ctx, "merge", owner, name, number, method)
	return nil
}

func (m *GHCLIMutator) ClosePR(ctx context.Context, repo string, number int) error {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return err
	}
	if _, err := m.Runner.Run(ctx, []string{"pr", "close", fmt.Sprint(number), "-R", repo}); err != nil {
		return fmt.Errorf("closing %s#%d: %w", repo, number, err)
	}
	m.Audit.Log(ctx, "close", owner, name, number, "")
	return nil
}

func (m *GHCLIMutator) ReopenPR(ctx context.Context, repo string, number int) error {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return err
	}
	if _, err := m.Runner.Run(ctx, []string{"pr", "reopen", fmt.Sprint(number), "-R", repo}); err != nil {
		return fmt.Errorf("reopening %s#%d: %w", repo, number, err)
	}
	m.Audit.Log(ctx, "reopen", owner, name, number, "")
	return nil
}

func (m *GHCLIMutator) MarkReadyForReview(ctx context.Context, repo string, number int, _ string) error {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return err
	}
	if _, err := m.Runner.Run(ctx, []string{"pr", "ready", fmt.Sprint(number), "-R", repo}); err != nil {
		return fmt.Errorf("marking %s#%d ready: %w", repo, number, err)
	}
	m.Audit.Log(ctx, "ready", owner, name, number, "")
	return nil
}

func (m *GHCLIMutator) ConvertToDraft(ctx context.Context, repo string, number int, _ string) error {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return err
	}
	if _, err := m.Runner.Run(ctx, []string{"pr", "ready", fmt.Sprint(number), "--undo", "-R", repo}); err != nil {
		return fmt.Errorf("converting %s#%d to draft: %w", repo, number, err)
	}
	m.Audit.Log(ctx, "draft", owner, name, number, "")
	return nil
}

func (m *GHCLIMutator) ResolveReviewThread(ctx context.Context, threadID string) error {
	query := fmt.Sprintf(`mutation{resolveReviewThread(input:{threadId:"%s"}){thread{id}}}`, threadID)
	if _, err := m.Runner.Run(ctx, []string{"api", "graphql", "-f", "query=" + query}); err != nil {
		return fmt.Errorf("resolving thread %s: %w", threadID, err)
	}
	return nil
}

// splitRepo splits "owner/name" into owner and name components.
func splitRepo(repo string) (owner, name string, err error) {
	parts := strings.SplitN(repo, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid repo format %q: expected owner/name", repo)
	}
	return parts[0], parts[1], nil
}
