package ui

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/evanisnor/argh/internal/persistence"
)

// ── Interfaces ────────────────────────────────────────────────────────────────

// PRMutator defines the write operations that commands can perform on pull
// requests. Implemented in production by *api.Mutator.
type PRMutator interface {
	Approve(ctx context.Context, repo string, number int) error
	RequestReview(ctx context.Context, repo string, number int, users []string) error
	PostComment(ctx context.Context, repo string, number int, body string) error
	AddLabel(ctx context.Context, repo string, number int, label string) error
	RemoveLabel(ctx context.Context, repo string, number int, label string) error
	MergePR(ctx context.Context, repo string, number int, method string) error
	ClosePR(ctx context.Context, repo string, number int) error
	ReopenPR(ctx context.Context, repo string, number int) error
	MarkReadyForReview(ctx context.Context, repo string, number int, globalID string) error
	ConvertToDraft(ctx context.Context, repo string, number int, globalID string) error
}

// PRStore is the persistence interface used by the command executor to resolve
// PR references (session ID → PR, #number → PR, title fragment → PR).
type PRStore interface {
	ListPullRequests() ([]persistence.PullRequest, error)
	GetSessionID(prURL string) (string, error)
}

// PollTrigger triggers an immediate poll cycle.
type PollTrigger interface {
	ForcePoll()
}

// BrowserOpener opens a URL in the system browser.
type BrowserOpener interface {
	Open(url string) error
}

// DiffViewer shows a diff for a pull request (task 23 — stub for now).
type DiffViewer interface {
	ShowDiff(repo string, number int) error
}

// DNDController manages do-not-disturb mode (task 32 — stub for now).
type DNDController interface {
	SetDND(duration time.Duration) error
	Wake() error
}

// WatchEngine manages watches for pull requests.
type WatchEngine interface {
	AddWatch(repo string, number int, prURL string, triggerExpr, actionExpr string) error
	ListWatches() ([]persistence.Watch, error)
	CancelWatch(id string) error
}

// ReviewSuggester suggests reviewers for a pull request when no explicit
// @users are provided to the :request command.
type ReviewSuggester interface {
	SuggestReviewers(ctx context.Context, repo string, number int) ([]string, error)
}

// ── Messages ──────────────────────────────────────────────────────────────────

// CommandResultMsg carries the outcome of a command execution back to the UI.
type CommandResultMsg struct {
	// Err is non-nil when the command failed.
	Err error
	// Status is a short human-readable description shown in the status bar.
	Status string
}

// CommandComposeMsg is sent when a command needs a body composed by the user
// (e.g. :review, :comment). The UI should open a ComposeModel for this.
type CommandComposeMsg struct {
	// Prompt describes what is being composed.
	Prompt string
	// OnSubmit is called with the composed body when the user submits.
	OnSubmit func(body string) tea.Cmd
}

// ReviewSuggestionsMsg is sent when reviewer suggestions are ready to be shown
// to the user. The UI should update the collaborator list and pre-fill the
// command bar so the user can select from the suggestions.
type ReviewSuggestionsMsg struct {
	PR          persistence.PullRequest
	Suggestions []string
	// InputPrefix is the pre-filled command bar text, e.g. ":request #42 @".
	InputPrefix string
}

// ── CommandExecutor ───────────────────────────────────────────────────────────

// CommandExecutor dispatches parsed command-bar input to the appropriate
// action. All external boundaries are injected as interfaces.
type CommandExecutor struct {
	mutator   PRMutator
	store     PRStore
	poll      PollTrigger
	browser   BrowserOpener
	diff      DiffViewer
	dnd       DNDController
	watches   WatchEngine
	suggester ReviewSuggester
}

// CommandExecutorConfig groups all dependencies for NewCommandExecutor.
type CommandExecutorConfig struct {
	Mutator   PRMutator
	Store     PRStore
	Poll      PollTrigger
	Browser   BrowserOpener
	Diff      DiffViewer
	DND       DNDController
	Watches   WatchEngine
	Suggester ReviewSuggester
}

// NewCommandExecutor creates a CommandExecutor with the given dependencies.
func NewCommandExecutor(cfg CommandExecutorConfig) *CommandExecutor {
	return &CommandExecutor{
		mutator:   cfg.Mutator,
		store:     cfg.Store,
		poll:      cfg.Poll,
		browser:   cfg.Browser,
		diff:      cfg.Diff,
		dnd:       cfg.DND,
		watches:   cfg.Watches,
		suggester: cfg.Suggester,
	}
}

// Execute parses cmd+args and returns a tea.Cmd that performs the action.
// The returned tea.Cmd may produce a CommandResultMsg, CommandComposeMsg,
// tea.QuitMsg, or ForceReloadMsg.
func (e *CommandExecutor) Execute(cmd string, args []string) tea.Cmd {
	switch cmd {
	case ":quit", "q":
		return tea.Quit

	case ":reload":
		return func() tea.Msg {
			if e.poll != nil {
				e.poll.ForcePoll()
			}
			return ForceReloadMsg{}
		}

	case ":wake":
		return func() tea.Msg {
			if e.dnd == nil {
				return CommandResultMsg{Status: ":wake: no DND controller"}
			}
			if err := e.dnd.Wake(); err != nil {
				return CommandResultMsg{Err: err}
			}
			return CommandResultMsg{Status: "polling resumed"}
		}

	case ":dnd":
		return e.execDND(args)

	case ":help":
		return func() tea.Msg {
			return ShowHelpMsg{}
		}

	case ":open":
		return e.execWithPR(args, func(pr persistence.PullRequest) tea.Msg {
			if e.browser == nil {
				return CommandResultMsg{Err: fmt.Errorf(":open: no browser opener configured")}
			}
			if err := e.browser.Open(pr.URL); err != nil {
				return CommandResultMsg{Err: err}
			}
			return CommandResultMsg{Status: fmt.Sprintf("opened %s", pr.URL)}
		})

	case ":diff":
		return e.execWithPR(args, func(pr persistence.PullRequest) tea.Msg {
			if e.diff == nil {
				return CommandResultMsg{Err: fmt.Errorf(":diff: no diff viewer configured")}
			}
			if err := e.diff.ShowDiff(pr.Repo, pr.Number); err != nil {
				return CommandResultMsg{Err: err}
			}
			return CommandResultMsg{Status: fmt.Sprintf("diff %s#%d", pr.Repo, pr.Number)}
		})

	case ":approve":
		return e.execWithPR(args, func(pr persistence.PullRequest) tea.Msg {
			if e.mutator == nil {
				return CommandResultMsg{Err: fmt.Errorf(":approve: no mutator configured")}
			}
			if err := e.mutator.Approve(context.Background(), pr.Repo, pr.Number); err != nil {
				return CommandResultMsg{Err: err}
			}
			return CommandResultMsg{Status: fmt.Sprintf("approved %s#%d", pr.Repo, pr.Number)}
		})

	case ":merge":
		return e.execWithPR(args, func(pr persistence.PullRequest) tea.Msg {
			if e.mutator == nil {
				return CommandResultMsg{Err: fmt.Errorf(":merge: no mutator configured")}
			}
			if err := e.mutator.MergePR(context.Background(), pr.Repo, pr.Number, ""); err != nil {
				return CommandResultMsg{Err: err}
			}
			return CommandResultMsg{Status: fmt.Sprintf("merged %s#%d", pr.Repo, pr.Number)}
		})

	case ":close":
		return e.execWithPR(args, func(pr persistence.PullRequest) tea.Msg {
			if e.mutator == nil {
				return CommandResultMsg{Err: fmt.Errorf(":close: no mutator configured")}
			}
			if err := e.mutator.ClosePR(context.Background(), pr.Repo, pr.Number); err != nil {
				return CommandResultMsg{Err: err}
			}
			return CommandResultMsg{Status: fmt.Sprintf("closed %s#%d", pr.Repo, pr.Number)}
		})

	case ":reopen":
		return e.execWithPR(args, func(pr persistence.PullRequest) tea.Msg {
			if e.mutator == nil {
				return CommandResultMsg{Err: fmt.Errorf(":reopen: no mutator configured")}
			}
			if err := e.mutator.ReopenPR(context.Background(), pr.Repo, pr.Number); err != nil {
				return CommandResultMsg{Err: err}
			}
			return CommandResultMsg{Status: fmt.Sprintf("reopened %s#%d", pr.Repo, pr.Number)}
		})

	case ":ready":
		return e.execWithPR(args, func(pr persistence.PullRequest) tea.Msg {
			if e.mutator == nil {
				return CommandResultMsg{Err: fmt.Errorf(":ready: no mutator configured")}
			}
			if err := e.mutator.MarkReadyForReview(context.Background(), pr.Repo, pr.Number, pr.GlobalID); err != nil {
				return CommandResultMsg{Err: err}
			}
			return CommandResultMsg{Status: fmt.Sprintf("marked ready %s#%d", pr.Repo, pr.Number)}
		})

	case ":draft":
		return e.execWithPR(args, func(pr persistence.PullRequest) tea.Msg {
			if e.mutator == nil {
				return CommandResultMsg{Err: fmt.Errorf(":draft: no mutator configured")}
			}
			if err := e.mutator.ConvertToDraft(context.Background(), pr.Repo, pr.Number, pr.GlobalID); err != nil {
				return CommandResultMsg{Err: err}
			}
			return CommandResultMsg{Status: fmt.Sprintf("converted to draft %s#%d", pr.Repo, pr.Number)}
		})

	case ":request":
		return e.execRequest(args)

	case ":label":
		return e.execLabel(args)

	case ":comment":
		return e.execCompose(args, ":comment", func(pr persistence.PullRequest, body string) tea.Cmd {
			return func() tea.Msg {
				if e.mutator == nil {
					return CommandResultMsg{Err: fmt.Errorf(":comment: no mutator configured")}
				}
				if err := e.mutator.PostComment(context.Background(), pr.Repo, pr.Number, body); err != nil {
					return CommandResultMsg{Err: err}
				}
				return CommandResultMsg{Status: fmt.Sprintf("commented on %s#%d", pr.Repo, pr.Number)}
			}
		})

	case ":review":
		return e.execCompose(args, ":review", func(pr persistence.PullRequest, body string) tea.Cmd {
			return func() tea.Msg {
				if e.mutator == nil {
					return CommandResultMsg{Err: fmt.Errorf(":review: no mutator configured")}
				}
				if err := e.mutator.PostComment(context.Background(), pr.Repo, pr.Number, body); err != nil {
					return CommandResultMsg{Err: err}
				}
				return CommandResultMsg{Status: fmt.Sprintf("review posted on %s#%d", pr.Repo, pr.Number)}
			}
		})

	case ":watch":
		return e.execWatch(args)

	default:
		return func() tea.Msg {
			return CommandResultMsg{Err: fmt.Errorf("unknown command: %s", cmd)}
		}
	}
}

// ── PR reference resolution ───────────────────────────────────────────────────

// resolvePR resolves a PR reference string to a PullRequest.
// Accepted formats:
//   - session ID (e.g. "a", "b")
//   - "#42"  (PR number with hash prefix)
//   - "42"   (bare PR number)
//   - title fragment (fuzzy — uses prMatches)
func (e *CommandExecutor) resolvePR(ref string) (persistence.PullRequest, error) {
	if e.store == nil {
		return persistence.PullRequest{}, fmt.Errorf("no store configured")
	}
	prs, err := e.store.ListPullRequests()
	if err != nil {
		return persistence.PullRequest{}, fmt.Errorf("listing PRs: %w", err)
	}

	// Build PRRef slice for prMatches.
	refs := make([]PRRef, len(prs))
	for i, pr := range prs {
		sid, _ := e.store.GetSessionID(pr.URL)
		refs[i] = PRRef{
			SessionID: sid,
			Number:    pr.Number,
			Title:     pr.Title,
			Repo:      pr.Repo,
			URL:       pr.URL,
		}
	}

	// Normalise: strip leading "#" for number lookup.
	lookup := ref
	if strings.HasPrefix(lookup, "#") {
		lookup = lookup[1:]
	}

	// Try numeric match first (works for "42" and "#42" after strip).
	if num, err2 := strconv.Atoi(lookup); err2 == nil {
		for _, pr := range prs {
			if pr.Number == num {
				return pr, nil
			}
		}
		return persistence.PullRequest{}, fmt.Errorf("PR #%d not found", num)
	}

	// Try exact session ID match.
	for i, r := range refs {
		if r.SessionID == ref {
			return prs[i], nil
		}
	}

	// Fuzzy title match via prMatches — returns session ID or "#number" strings.
	matches := prMatches(ref, refs)
	if len(matches) == 0 {
		return persistence.PullRequest{}, fmt.Errorf("PR %q not found", ref)
	}
	// Re-resolve the first match back to a PullRequest.
	firstMatch := matches[0]
	return e.resolvePR(firstMatch)
}

// ── Command helpers ───────────────────────────────────────────────────────────

// execWithPR resolves the first arg as a PR reference and calls fn with the result.
func (e *CommandExecutor) execWithPR(args []string, fn func(persistence.PullRequest) tea.Msg) tea.Cmd {
	return func() tea.Msg {
		pr, err := e.resolvePR(firstArg(args))
		if err != nil {
			return CommandResultMsg{Err: err}
		}
		return fn(pr)
	}
}

// execDND handles :dnd [duration].
func (e *CommandExecutor) execDND(args []string) tea.Cmd {
	return func() tea.Msg {
		if e.dnd == nil {
			return CommandResultMsg{Err: fmt.Errorf(":dnd: no DND controller configured")}
		}
		dur := 30 * time.Minute
		if len(args) > 0 && args[0] != "" {
			parsed, err := time.ParseDuration(args[0])
			if err != nil {
				return CommandResultMsg{Err: fmt.Errorf(":dnd: invalid duration %q: %w", args[0], err)}
			}
			dur = parsed
		}
		if err := e.dnd.SetDND(dur); err != nil {
			return CommandResultMsg{Err: err}
		}
		return CommandResultMsg{Status: fmt.Sprintf("DND active for %s", dur)}
	}
}

// execRequest handles :request [#pr] @user...
// When no @users are provided, the suggester is queried and a
// ReviewSuggestionsMsg is returned so the user can select from suggestions.
func (e *CommandExecutor) execRequest(args []string) tea.Cmd {
	return func() tea.Msg {
		if len(args) == 0 {
			return CommandResultMsg{Err: fmt.Errorf(":request: usage: :request [#pr] @user...")}
		}
		pr, err := e.resolvePR(args[0])
		if err != nil {
			return CommandResultMsg{Err: err}
		}
		// Collect @user args (strip leading @).
		var users []string
		for _, a := range args[1:] {
			u := strings.TrimPrefix(a, "@")
			if u != "" {
				users = append(users, u)
			}
		}
		if len(users) == 0 {
			// No users specified — fetch suggestions if available.
			if e.suggester != nil {
				suggestions, err := e.suggester.SuggestReviewers(context.Background(), pr.Repo, pr.Number)
				if err != nil {
					return CommandResultMsg{Err: fmt.Errorf(":request: fetching suggestions: %w", err)}
				}
				// Build input prefix using session ID when available.
				prefix := fmt.Sprintf(":request #%d @", pr.Number)
				if sid, err2 := e.store.GetSessionID(pr.URL); err2 == nil && sid != "" {
					prefix = fmt.Sprintf(":request %s @", sid)
				}
				return ReviewSuggestionsMsg{PR: pr, Suggestions: suggestions, InputPrefix: prefix}
			}
			return CommandResultMsg{Err: fmt.Errorf(":request: no reviewers specified")}
		}
		if e.mutator == nil {
			return CommandResultMsg{Err: fmt.Errorf(":request: no mutator configured")}
		}
		if err := e.mutator.RequestReview(context.Background(), pr.Repo, pr.Number, users); err != nil {
			return CommandResultMsg{Err: err}
		}
		return CommandResultMsg{Status: fmt.Sprintf("requested review from %s on %s#%d", strings.Join(users, ", "), pr.Repo, pr.Number)}
	}
}

// execLabel handles :label [#pr] [label]
// If label starts with "-" it is removed; otherwise added.
func (e *CommandExecutor) execLabel(args []string) tea.Cmd {
	return func() tea.Msg {
		if len(args) < 2 {
			return CommandResultMsg{Err: fmt.Errorf(":label: usage: :label [#pr] [label]")}
		}
		pr, err := e.resolvePR(args[0])
		if err != nil {
			return CommandResultMsg{Err: err}
		}
		label := args[1]
		if e.mutator == nil {
			return CommandResultMsg{Err: fmt.Errorf(":label: no mutator configured")}
		}
		if strings.HasPrefix(label, "-") {
			label = label[1:]
			if err := e.mutator.RemoveLabel(context.Background(), pr.Repo, pr.Number, label); err != nil {
				return CommandResultMsg{Err: err}
			}
			return CommandResultMsg{Status: fmt.Sprintf("removed label %q from %s#%d", label, pr.Repo, pr.Number)}
		}
		if err := e.mutator.AddLabel(context.Background(), pr.Repo, pr.Number, label); err != nil {
			return CommandResultMsg{Err: err}
		}
		return CommandResultMsg{Status: fmt.Sprintf("added label %q to %s#%d", label, pr.Repo, pr.Number)}
	}
}

// execCompose resolves the PR ref and returns a CommandComposeMsg so the UI
// can open an inline compose view. onSubmit is called with the typed body.
func (e *CommandExecutor) execCompose(args []string, cmdName string, onSubmit func(persistence.PullRequest, string) tea.Cmd) tea.Cmd {
	return func() tea.Msg {
		pr, err := e.resolvePR(firstArg(args))
		if err != nil {
			return CommandResultMsg{Err: err}
		}
		return CommandComposeMsg{
			Prompt:   fmt.Sprintf("%s %s#%d — type body, Enter to submit", cmdName, pr.Repo, pr.Number),
			OnSubmit: func(body string) tea.Cmd { return onSubmit(pr, body) },
		}
	}
}

// execWatch dispatches :watch sub-commands:
//
//	:watch [#pr] <trigger> <action>  — add a watch
//	:watch list                      — list active (non-cancelled) watches
//	:watch cancel <id>               — cancel a watch by ID
func (e *CommandExecutor) execWatch(args []string) tea.Cmd {
	if e.watches == nil {
		return func() tea.Msg {
			return CommandResultMsg{Err: fmt.Errorf(":watch: no watch engine configured")}
		}
	}
	if len(args) == 0 {
		return func() tea.Msg {
			return CommandResultMsg{Err: fmt.Errorf(":watch: usage: :watch [#pr] <trigger> <action> | :watch list | :watch cancel <id>")}
		}
	}
	switch args[0] {
	case "list":
		return func() tea.Msg {
			watches, err := e.watches.ListWatches()
			if err != nil {
				return CommandResultMsg{Err: err}
			}
			if len(watches) == 0 {
				return CommandResultMsg{Status: "no active watches"}
			}
			lines := make([]string, len(watches))
			for i, w := range watches {
				lines[i] = fmt.Sprintf("[%s] %s#%d  trigger:%s  action:%s  status:%s",
					w.ID, w.Repo, w.PRNumber, w.TriggerExpr, w.ActionExpr, w.Status)
			}
			return CommandResultMsg{Status: strings.Join(lines, "\n")}
		}
	case "cancel":
		if len(args) < 2 {
			return func() tea.Msg {
				return CommandResultMsg{Err: fmt.Errorf(":watch cancel: usage: :watch cancel <id>")}
			}
		}
		id := args[1]
		return func() tea.Msg {
			if err := e.watches.CancelWatch(id); err != nil {
				return CommandResultMsg{Err: err}
			}
			return CommandResultMsg{Status: fmt.Sprintf("watch %s cancelled", id)}
		}
	default:
		// :watch [#pr] <trigger> <action>
		if len(args) < 3 {
			return func() tea.Msg {
				return CommandResultMsg{Err: fmt.Errorf(":watch: usage: :watch [#pr] <trigger> <action>")}
			}
		}
		return func() tea.Msg {
			pr, err := e.resolvePR(args[0])
			if err != nil {
				return CommandResultMsg{Err: err}
			}
			trigger := args[1]
			action := args[2]
			if err := e.watches.AddWatch(pr.Repo, pr.Number, pr.URL, trigger, action); err != nil {
				return CommandResultMsg{Err: err}
			}
			return CommandResultMsg{Status: fmt.Sprintf("watch added for %s#%d", pr.Repo, pr.Number)}
		}
	}
}

// ── Parsing helper ────────────────────────────────────────────────────────────

// ParseCommand splits a raw command-bar input string (e.g. ":approve a") into
// the command name and its arguments.
func ParseCommand(input string) (cmd string, args []string) {
	parts := strings.Fields(strings.TrimSpace(input))
	if len(parts) == 0 {
		return "", nil
	}
	return parts[0], parts[1:]
}

// firstArg returns the first argument or an empty string when args is empty.
func firstArg(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}
