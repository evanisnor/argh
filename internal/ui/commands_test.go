package ui

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/evanisnor/argh/internal/persistence"
)

// ── Fakes ─────────────────────────────────────────────────────────────────────

// fakePRMutator records calls and returns configured errors.
type fakePRMutator struct {
	approveErr          error
	requestReviewErr    error
	postCommentErr      error
	addLabelErr         error
	removeLabelErr      error
	mergePRErr          error
	closePRErr          error
	reopenPRErr         error
	markReadyErr        error
	convertToDraftErr   error

	approveCalled          bool
	requestReviewCalled    bool
	postCommentCalled      bool
	addLabelCalled         bool
	removeLabelCalled      bool
	mergePRCalled          bool
	closePRCalled          bool
	reopenPRCalled         bool
	markReadyCalled        bool
	convertToDraftCalled   bool

	lastRequestUsers []string
	lastCommentBody  string
	lastLabel        string
}

func (f *fakePRMutator) Approve(_ context.Context, _ string, _ int) error {
	f.approveCalled = true
	return f.approveErr
}
func (f *fakePRMutator) RequestReview(_ context.Context, _ string, _ int, users []string) error {
	f.requestReviewCalled = true
	f.lastRequestUsers = users
	return f.requestReviewErr
}
func (f *fakePRMutator) PostComment(_ context.Context, _ string, _ int, body string) error {
	f.postCommentCalled = true
	f.lastCommentBody = body
	return f.postCommentErr
}
func (f *fakePRMutator) AddLabel(_ context.Context, _ string, _ int, label string) error {
	f.addLabelCalled = true
	f.lastLabel = label
	return f.addLabelErr
}
func (f *fakePRMutator) RemoveLabel(_ context.Context, _ string, _ int, label string) error {
	f.removeLabelCalled = true
	f.lastLabel = label
	return f.removeLabelErr
}
func (f *fakePRMutator) MergePR(_ context.Context, _ string, _ int, _ string) error {
	f.mergePRCalled = true
	return f.mergePRErr
}
func (f *fakePRMutator) ClosePR(_ context.Context, _ string, _ int) error {
	f.closePRCalled = true
	return f.closePRErr
}
func (f *fakePRMutator) ReopenPR(_ context.Context, _ string, _ int) error {
	f.reopenPRCalled = true
	return f.reopenPRErr
}
func (f *fakePRMutator) MarkReadyForReview(_ context.Context, _ string, _ int, _ string) error {
	f.markReadyCalled = true
	return f.markReadyErr
}
func (f *fakePRMutator) ConvertToDraft(_ context.Context, _ string, _ int, _ string) error {
	f.convertToDraftCalled = true
	return f.convertToDraftErr
}

// fakePRStore returns a fixed list of PRs and session IDs.
type fakePRStore struct {
	prs        []persistence.PullRequest
	sessionIDs map[string]string // URL → sessionID
	listErr    error
}

func (f *fakePRStore) ListPullRequests() ([]persistence.PullRequest, error) {
	return f.prs, f.listErr
}

func (f *fakePRStore) GetSessionID(prURL string) (string, error) {
	if f.sessionIDs != nil {
		if sid, ok := f.sessionIDs[prURL]; ok {
			return sid, nil
		}
	}
	return "", fmt.Errorf("not found")
}

// fakePollTrigger records ForcePoll calls.
type fakePollTrigger struct{ called bool }

func (f *fakePollTrigger) ForcePoll() { f.called = true }

// fakeBrowserOpener records Open calls.
type fakeBrowserOpener struct {
	lastURL string
	openErr error
}

func (f *fakeBrowserOpener) Open(url string) error {
	f.lastURL = url
	return f.openErr
}

// fakeDiffViewer records ShowDiff calls.
type fakeDiffViewer struct {
	called  bool
	showErr error
}

func (f *fakeDiffViewer) ShowDiff(_ string, _ int) error {
	f.called = true
	return f.showErr
}

// fakeDNDController records DND calls.
type fakeDNDController struct {
	setDNDDuration time.Duration
	setDNDErr      error
	wakeCalled     bool
	wakeErr        error
}

func (f *fakeDNDController) SetDND(d time.Duration) error {
	f.setDNDDuration = d
	return f.setDNDErr
}
func (f *fakeDNDController) Wake() error {
	f.wakeCalled = true
	return f.wakeErr
}

// fakeWatchEngine records watch management calls.
type fakeWatchEngine struct {
	addWatchErr    error
	listWatchesErr error
	cancelWatchErr error

	addCalled    bool
	listCalled   bool
	cancelCalled bool

	lastCancelID string
	watches      []persistence.Watch
}

func (f *fakeWatchEngine) AddWatch(_ string, _ int, _ string, _, _ string) error {
	f.addCalled = true
	return f.addWatchErr
}

func (f *fakeWatchEngine) ListWatches() ([]persistence.Watch, error) {
	f.listCalled = true
	return f.watches, f.listWatchesErr
}

func (f *fakeWatchEngine) CancelWatch(id string) error {
	f.cancelCalled = true
	f.lastCancelID = id
	return f.cancelWatchErr
}

// fakeHelpOverlay records Show calls.
type fakeHelpOverlay struct{ called bool }

func (f *fakeHelpOverlay) Show() { f.called = true }

// ── Helpers ───────────────────────────────────────────────────────────────────

// samplePR returns a deterministic PullRequest for tests.
func samplePR() persistence.PullRequest {
	return persistence.PullRequest{
		ID:       "pr-1",
		Repo:     "owner/repo",
		Number:   42,
		Title:    "Fix login bug",
		Status:   "open",
		GlobalID: "MDExOlB1bGxSZXF1ZXN0MQ==",
		URL:      "https://github.com/owner/repo/pull/42",
	}
}

func samplePRs() []persistence.PullRequest {
	pr1 := samplePR()
	pr2 := persistence.PullRequest{
		ID:       "pr-2",
		Repo:     "owner/repo",
		Number:   99,
		Title:    "Add dark mode",
		Status:   "open",
		GlobalID: "MDExOlB1bGxSZXF1ZXN0Mg==",
		URL:      "https://github.com/owner/repo/pull/99",
	}
	return []persistence.PullRequest{pr1, pr2}
}

func sampleSessionIDs() map[string]string {
	return map[string]string{
		"https://github.com/owner/repo/pull/42": "a",
		"https://github.com/owner/repo/pull/99": "b",
	}
}

// newExec builds a CommandExecutor with all fakes wired.
func newExec(mut *fakePRMutator, store *fakePRStore) *CommandExecutor {
	return NewCommandExecutor(CommandExecutorConfig{
		Mutator: mut,
		Store:   store,
		Poll:    &fakePollTrigger{},
		Browser: &fakeBrowserOpener{},
		Diff:    &fakeDiffViewer{},
		DND:     &fakeDNDController{},
		Watches: &fakeWatchEngine{},
		Help:    &fakeHelpOverlay{},
	})
}

// runCmd executes a tea.Cmd and returns the resulting tea.Msg.
func runCmd(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected non-nil tea.Cmd")
	}
	return cmd()
}

// ── ParseCommand ──────────────────────────────────────────────────────────────

func TestParseCommand(t *testing.T) {
	tests := []struct {
		input    string
		wantCmd  string
		wantArgs []string
	}{
		{":approve a", ":approve", []string{"a"}},
		{":merge #42", ":merge", []string{"#42"}},
		{":request a @alice @bob", ":request", []string{"a", "@alice", "@bob"}},
		{":reload", ":reload", nil},
		{" :quit ", ":quit", nil},
		{"", "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			cmd, args := ParseCommand(tt.input)
			if cmd != tt.wantCmd {
				t.Errorf("cmd = %q, want %q", cmd, tt.wantCmd)
			}
			if len(args) != len(tt.wantArgs) {
				t.Errorf("args = %v, want %v", args, tt.wantArgs)
				return
			}
			for i, a := range args {
				if a != tt.wantArgs[i] {
					t.Errorf("args[%d] = %q, want %q", i, a, tt.wantArgs[i])
				}
			}
		})
	}
}

// ── PR reference resolution ───────────────────────────────────────────────────

func TestResolvePR(t *testing.T) {
	prs := samplePRs()
	sids := sampleSessionIDs()
	store := &fakePRStore{prs: prs, sessionIDs: sids}
	exec := newExec(&fakePRMutator{}, store)

	tests := []struct {
		name    string
		ref     string
		wantNum int
		wantErr bool
	}{
		{"session ID a", "a", 42, false},
		{"session ID b", "b", 99, false},
		{"#number hash", "#42", 42, false},
		{"bare number", "42", 42, false},
		{"title fragment", "login", 42, false},
		{"unknown session ID", "z", 0, true},
		{"unknown number", "#999", 0, true},
		{"unknown fragment", "xyzzy9999", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pr, err := exec.resolvePR(tt.ref)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error for ref %q, got nil", tt.ref)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if pr.Number != tt.wantNum {
				t.Errorf("PR.Number = %d, want %d", pr.Number, tt.wantNum)
			}
		})
	}
}

func TestResolvePR_StoreError(t *testing.T) {
	store := &fakePRStore{listErr: errors.New("db down")}
	exec := newExec(&fakePRMutator{}, store)
	_, err := exec.resolvePR("a")
	if err == nil {
		t.Error("expected error when store.ListPullRequests fails")
	}
}

func TestResolvePR_NilStore(t *testing.T) {
	exec := NewCommandExecutor(CommandExecutorConfig{})
	_, err := exec.resolvePR("a")
	if err == nil {
		t.Error("expected error when store is nil")
	}
}

// ── :quit ─────────────────────────────────────────────────────────────────────

func TestExecute_Quit(t *testing.T) {
	exec := newExec(&fakePRMutator{}, &fakePRStore{})
	for _, cmd := range []string{":quit", "q"} {
		t.Run(cmd, func(t *testing.T) {
			teaCmd := exec.Execute(cmd, nil)
			if teaCmd == nil {
				t.Fatal("expected non-nil Cmd for :quit")
			}
			// tea.Quit returns tea.QuitMsg when invoked.
			msg := teaCmd()
			if _, ok := msg.(tea.QuitMsg); !ok {
				t.Errorf("expected QuitMsg, got %T", msg)
			}
		})
	}
}

// ── :reload ───────────────────────────────────────────────────────────────────

func TestExecute_Reload(t *testing.T) {
	poll := &fakePollTrigger{}
	exec := NewCommandExecutor(CommandExecutorConfig{Poll: poll, Store: &fakePRStore{}})
	teaCmd := exec.Execute(":reload", nil)
	msg := runCmd(t, teaCmd)
	if _, ok := msg.(ForceReloadMsg); !ok {
		t.Errorf("expected ForceReloadMsg, got %T", msg)
	}
	if !poll.called {
		t.Error("ForcePoll should have been called")
	}
}

func TestExecute_Reload_NilPoll(t *testing.T) {
	exec := NewCommandExecutor(CommandExecutorConfig{Store: &fakePRStore{}})
	teaCmd := exec.Execute(":reload", nil)
	msg := runCmd(t, teaCmd)
	// Should still return ForceReloadMsg even with nil PollTrigger.
	if _, ok := msg.(ForceReloadMsg); !ok {
		t.Errorf("expected ForceReloadMsg, got %T", msg)
	}
}

// ── :wake ─────────────────────────────────────────────────────────────────────

func TestExecute_Wake(t *testing.T) {
	dnd := &fakeDNDController{}
	exec := NewCommandExecutor(CommandExecutorConfig{DND: dnd, Store: &fakePRStore{}})
	msg := runCmd(t, exec.Execute(":wake", nil))
	r, ok := msg.(CommandResultMsg)
	if !ok {
		t.Fatalf("expected CommandResultMsg, got %T", msg)
	}
	if r.Err != nil {
		t.Errorf("unexpected error: %v", r.Err)
	}
	if !dnd.wakeCalled {
		t.Error("Wake should have been called")
	}
}

func TestExecute_Wake_Error(t *testing.T) {
	dnd := &fakeDNDController{wakeErr: errors.New("wake failed")}
	exec := NewCommandExecutor(CommandExecutorConfig{DND: dnd, Store: &fakePRStore{}})
	msg := runCmd(t, exec.Execute(":wake", nil))
	r := msg.(CommandResultMsg)
	if r.Err == nil {
		t.Error("expected error from Wake")
	}
}

func TestExecute_Wake_NilDND(t *testing.T) {
	exec := NewCommandExecutor(CommandExecutorConfig{Store: &fakePRStore{}})
	msg := runCmd(t, exec.Execute(":wake", nil))
	r, ok := msg.(CommandResultMsg)
	if !ok {
		t.Fatalf("expected CommandResultMsg, got %T", msg)
	}
	if r.Err != nil {
		t.Error("nil DND should return status message, not error")
	}
}

// ── :dnd ──────────────────────────────────────────────────────────────────────

func TestExecute_DND(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantDur  time.Duration
		wantErr  bool
	}{
		{"default 30m", nil, 30 * time.Minute, false},
		{"explicit 1h", []string{"1h"}, time.Hour, false},
		{"invalid duration", []string{"notaduration"}, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dnd := &fakeDNDController{}
			exec := NewCommandExecutor(CommandExecutorConfig{DND: dnd, Store: &fakePRStore{}})
			msg := runCmd(t, exec.Execute(":dnd", tt.args))
			r, ok := msg.(CommandResultMsg)
			if !ok {
				t.Fatalf("expected CommandResultMsg, got %T", msg)
			}
			if tt.wantErr {
				if r.Err == nil {
					t.Error("expected error")
				}
				return
			}
			if r.Err != nil {
				t.Errorf("unexpected error: %v", r.Err)
			}
			if dnd.setDNDDuration != tt.wantDur {
				t.Errorf("SetDND duration = %v, want %v", dnd.setDNDDuration, tt.wantDur)
			}
		})
	}
}

func TestExecute_DND_Error(t *testing.T) {
	dnd := &fakeDNDController{setDNDErr: errors.New("dnd failed")}
	exec := NewCommandExecutor(CommandExecutorConfig{DND: dnd, Store: &fakePRStore{}})
	msg := runCmd(t, exec.Execute(":dnd", nil))
	r := msg.(CommandResultMsg)
	if r.Err == nil {
		t.Error("expected error from SetDND")
	}
}

func TestExecute_DND_NilDND(t *testing.T) {
	exec := NewCommandExecutor(CommandExecutorConfig{Store: &fakePRStore{}})
	msg := runCmd(t, exec.Execute(":dnd", nil))
	r, ok := msg.(CommandResultMsg)
	if !ok {
		t.Fatalf("expected CommandResultMsg, got %T", msg)
	}
	if r.Err == nil {
		t.Error("expected error when DND controller is nil")
	}
}

// ── :help ─────────────────────────────────────────────────────────────────────

func TestExecute_Help(t *testing.T) {
	overlay := &fakeHelpOverlay{}
	exec := NewCommandExecutor(CommandExecutorConfig{Help: overlay, Store: &fakePRStore{}})
	msg := runCmd(t, exec.Execute(":help", nil))
	if _, ok := msg.(CommandResultMsg); !ok {
		t.Fatalf("expected CommandResultMsg, got %T", msg)
	}
	if !overlay.called {
		t.Error("HelpOverlay.Show should have been called")
	}
}

func TestExecute_Help_NilOverlay(t *testing.T) {
	exec := NewCommandExecutor(CommandExecutorConfig{Store: &fakePRStore{}})
	msg := runCmd(t, exec.Execute(":help", nil))
	if _, ok := msg.(CommandResultMsg); !ok {
		t.Fatalf("expected CommandResultMsg, got %T", msg)
	}
	// Should not panic with nil overlay.
}

// ── :open ─────────────────────────────────────────────────────────────────────

func TestExecute_Open(t *testing.T) {
	browser := &fakeBrowserOpener{}
	store := &fakePRStore{prs: samplePRs(), sessionIDs: sampleSessionIDs()}
	exec := NewCommandExecutor(CommandExecutorConfig{Browser: browser, Store: store})
	msg := runCmd(t, exec.Execute(":open", []string{"a"}))
	r, ok := msg.(CommandResultMsg)
	if !ok {
		t.Fatalf("expected CommandResultMsg, got %T", msg)
	}
	if r.Err != nil {
		t.Errorf("unexpected error: %v", r.Err)
	}
	if browser.lastURL == "" {
		t.Error("Open should have been called with a URL")
	}
}

func TestExecute_Open_BrowserError(t *testing.T) {
	browser := &fakeBrowserOpener{openErr: errors.New("cannot open")}
	store := &fakePRStore{prs: samplePRs(), sessionIDs: sampleSessionIDs()}
	exec := NewCommandExecutor(CommandExecutorConfig{Browser: browser, Store: store})
	msg := runCmd(t, exec.Execute(":open", []string{"a"}))
	r := msg.(CommandResultMsg)
	if r.Err == nil {
		t.Error("expected error from browser.Open")
	}
}

func TestExecute_Open_NilBrowser(t *testing.T) {
	store := &fakePRStore{prs: samplePRs(), sessionIDs: sampleSessionIDs()}
	exec := NewCommandExecutor(CommandExecutorConfig{Store: store})
	msg := runCmd(t, exec.Execute(":open", []string{"a"}))
	r := msg.(CommandResultMsg)
	if r.Err == nil {
		t.Error("expected error when browser is nil")
	}
}

func TestExecute_Open_PRNotFound(t *testing.T) {
	store := &fakePRStore{prs: samplePRs(), sessionIDs: sampleSessionIDs()}
	exec := NewCommandExecutor(CommandExecutorConfig{Browser: &fakeBrowserOpener{}, Store: store})
	msg := runCmd(t, exec.Execute(":open", []string{"z"}))
	r := msg.(CommandResultMsg)
	if r.Err == nil {
		t.Error("expected error for unknown PR ref")
	}
}

// ── :diff ─────────────────────────────────────────────────────────────────────

func TestExecute_Diff(t *testing.T) {
	diff := &fakeDiffViewer{}
	store := &fakePRStore{prs: samplePRs(), sessionIDs: sampleSessionIDs()}
	exec := NewCommandExecutor(CommandExecutorConfig{Diff: diff, Store: store})
	msg := runCmd(t, exec.Execute(":diff", []string{"a"}))
	r, ok := msg.(CommandResultMsg)
	if !ok {
		t.Fatalf("expected CommandResultMsg, got %T", msg)
	}
	if r.Err != nil {
		t.Errorf("unexpected error: %v", r.Err)
	}
	if !diff.called {
		t.Error("ShowDiff should have been called")
	}
}

func TestExecute_Diff_Error(t *testing.T) {
	diff := &fakeDiffViewer{showErr: errors.New("delta not found")}
	store := &fakePRStore{prs: samplePRs(), sessionIDs: sampleSessionIDs()}
	exec := NewCommandExecutor(CommandExecutorConfig{Diff: diff, Store: store})
	msg := runCmd(t, exec.Execute(":diff", []string{"a"}))
	r := msg.(CommandResultMsg)
	if r.Err == nil {
		t.Error("expected error from ShowDiff")
	}
}

func TestExecute_Diff_NilDiffViewer(t *testing.T) {
	store := &fakePRStore{prs: samplePRs(), sessionIDs: sampleSessionIDs()}
	exec := NewCommandExecutor(CommandExecutorConfig{Store: store})
	msg := runCmd(t, exec.Execute(":diff", []string{"a"}))
	r := msg.(CommandResultMsg)
	if r.Err == nil {
		t.Error("expected error when diff viewer is nil")
	}
}

// ── :approve ──────────────────────────────────────────────────────────────────

func TestExecute_Approve(t *testing.T) {
	mut := &fakePRMutator{}
	store := &fakePRStore{prs: samplePRs(), sessionIDs: sampleSessionIDs()}
	exec := newExec(mut, store)
	msg := runCmd(t, exec.Execute(":approve", []string{"a"}))
	r, ok := msg.(CommandResultMsg)
	if !ok {
		t.Fatalf("expected CommandResultMsg, got %T", msg)
	}
	if r.Err != nil {
		t.Errorf("unexpected error: %v", r.Err)
	}
	if !mut.approveCalled {
		t.Error("Approve should have been called")
	}
}

func TestExecute_Approve_MutatorError(t *testing.T) {
	mut := &fakePRMutator{approveErr: errors.New("forbidden")}
	store := &fakePRStore{prs: samplePRs(), sessionIDs: sampleSessionIDs()}
	exec := newExec(mut, store)
	msg := runCmd(t, exec.Execute(":approve", []string{"a"}))
	r := msg.(CommandResultMsg)
	if r.Err == nil {
		t.Error("expected error from Approve")
	}
}

func TestExecute_Approve_NilMutator(t *testing.T) {
	store := &fakePRStore{prs: samplePRs(), sessionIDs: sampleSessionIDs()}
	exec := NewCommandExecutor(CommandExecutorConfig{Store: store})
	msg := runCmd(t, exec.Execute(":approve", []string{"a"}))
	r := msg.(CommandResultMsg)
	if r.Err == nil {
		t.Error("expected error when mutator is nil")
	}
}

// ── :merge ────────────────────────────────────────────────────────────────────

func TestExecute_Merge(t *testing.T) {
	mut := &fakePRMutator{}
	store := &fakePRStore{prs: samplePRs(), sessionIDs: sampleSessionIDs()}
	exec := newExec(mut, store)
	msg := runCmd(t, exec.Execute(":merge", []string{"#42"}))
	r, ok := msg.(CommandResultMsg)
	if !ok {
		t.Fatalf("expected CommandResultMsg, got %T", msg)
	}
	if r.Err != nil {
		t.Errorf("unexpected error: %v", r.Err)
	}
	if !mut.mergePRCalled {
		t.Error("MergePR should have been called")
	}
}

func TestExecute_Merge_Error(t *testing.T) {
	mut := &fakePRMutator{mergePRErr: errors.New("conflict")}
	store := &fakePRStore{prs: samplePRs(), sessionIDs: sampleSessionIDs()}
	exec := newExec(mut, store)
	msg := runCmd(t, exec.Execute(":merge", []string{"a"}))
	r := msg.(CommandResultMsg)
	if r.Err == nil {
		t.Error("expected error from MergePR")
	}
}

func TestExecute_Merge_NilMutator(t *testing.T) {
	store := &fakePRStore{prs: samplePRs(), sessionIDs: sampleSessionIDs()}
	exec := NewCommandExecutor(CommandExecutorConfig{Store: store})
	msg := runCmd(t, exec.Execute(":merge", []string{"a"}))
	r := msg.(CommandResultMsg)
	if r.Err == nil {
		t.Error("expected error when mutator is nil")
	}
}

// ── :close ────────────────────────────────────────────────────────────────────

func TestExecute_Close(t *testing.T) {
	mut := &fakePRMutator{}
	store := &fakePRStore{prs: samplePRs(), sessionIDs: sampleSessionIDs()}
	exec := newExec(mut, store)
	msg := runCmd(t, exec.Execute(":close", []string{"b"}))
	r, ok := msg.(CommandResultMsg)
	if !ok {
		t.Fatalf("expected CommandResultMsg, got %T", msg)
	}
	if r.Err != nil {
		t.Errorf("unexpected error: %v", r.Err)
	}
	if !mut.closePRCalled {
		t.Error("ClosePR should have been called")
	}
}

func TestExecute_Close_Error(t *testing.T) {
	mut := &fakePRMutator{closePRErr: errors.New("already closed")}
	store := &fakePRStore{prs: samplePRs(), sessionIDs: sampleSessionIDs()}
	exec := newExec(mut, store)
	msg := runCmd(t, exec.Execute(":close", []string{"a"}))
	r := msg.(CommandResultMsg)
	if r.Err == nil {
		t.Error("expected error from ClosePR")
	}
}

func TestExecute_Close_NilMutator(t *testing.T) {
	store := &fakePRStore{prs: samplePRs(), sessionIDs: sampleSessionIDs()}
	exec := NewCommandExecutor(CommandExecutorConfig{Store: store})
	msg := runCmd(t, exec.Execute(":close", []string{"a"}))
	r := msg.(CommandResultMsg)
	if r.Err == nil {
		t.Error("expected error when mutator is nil")
	}
}

// ── :reopen ───────────────────────────────────────────────────────────────────

func TestExecute_Reopen(t *testing.T) {
	mut := &fakePRMutator{}
	store := &fakePRStore{prs: samplePRs(), sessionIDs: sampleSessionIDs()}
	exec := newExec(mut, store)
	msg := runCmd(t, exec.Execute(":reopen", []string{"a"}))
	r, ok := msg.(CommandResultMsg)
	if !ok {
		t.Fatalf("expected CommandResultMsg, got %T", msg)
	}
	if r.Err != nil {
		t.Errorf("unexpected error: %v", r.Err)
	}
	if !mut.reopenPRCalled {
		t.Error("ReopenPR should have been called")
	}
}

func TestExecute_Reopen_Error(t *testing.T) {
	mut := &fakePRMutator{reopenPRErr: errors.New("not found")}
	store := &fakePRStore{prs: samplePRs(), sessionIDs: sampleSessionIDs()}
	exec := newExec(mut, store)
	msg := runCmd(t, exec.Execute(":reopen", []string{"a"}))
	r := msg.(CommandResultMsg)
	if r.Err == nil {
		t.Error("expected error from ReopenPR")
	}
}

func TestExecute_Reopen_NilMutator(t *testing.T) {
	store := &fakePRStore{prs: samplePRs(), sessionIDs: sampleSessionIDs()}
	exec := NewCommandExecutor(CommandExecutorConfig{Store: store})
	msg := runCmd(t, exec.Execute(":reopen", []string{"a"}))
	r := msg.(CommandResultMsg)
	if r.Err == nil {
		t.Error("expected error when mutator is nil")
	}
}

// ── :ready ────────────────────────────────────────────────────────────────────

func TestExecute_Ready(t *testing.T) {
	mut := &fakePRMutator{}
	store := &fakePRStore{prs: samplePRs(), sessionIDs: sampleSessionIDs()}
	exec := newExec(mut, store)
	msg := runCmd(t, exec.Execute(":ready", []string{"a"}))
	r, ok := msg.(CommandResultMsg)
	if !ok {
		t.Fatalf("expected CommandResultMsg, got %T", msg)
	}
	if r.Err != nil {
		t.Errorf("unexpected error: %v", r.Err)
	}
	if !mut.markReadyCalled {
		t.Error("MarkReadyForReview should have been called")
	}
}

func TestExecute_Ready_Error(t *testing.T) {
	mut := &fakePRMutator{markReadyErr: errors.New("gql error")}
	store := &fakePRStore{prs: samplePRs(), sessionIDs: sampleSessionIDs()}
	exec := newExec(mut, store)
	msg := runCmd(t, exec.Execute(":ready", []string{"a"}))
	r := msg.(CommandResultMsg)
	if r.Err == nil {
		t.Error("expected error from MarkReadyForReview")
	}
}

func TestExecute_Ready_NilMutator(t *testing.T) {
	store := &fakePRStore{prs: samplePRs(), sessionIDs: sampleSessionIDs()}
	exec := NewCommandExecutor(CommandExecutorConfig{Store: store})
	msg := runCmd(t, exec.Execute(":ready", []string{"a"}))
	r := msg.(CommandResultMsg)
	if r.Err == nil {
		t.Error("expected error when mutator is nil")
	}
}

// ── :draft ────────────────────────────────────────────────────────────────────

func TestExecute_Draft(t *testing.T) {
	mut := &fakePRMutator{}
	store := &fakePRStore{prs: samplePRs(), sessionIDs: sampleSessionIDs()}
	exec := newExec(mut, store)
	msg := runCmd(t, exec.Execute(":draft", []string{"a"}))
	r, ok := msg.(CommandResultMsg)
	if !ok {
		t.Fatalf("expected CommandResultMsg, got %T", msg)
	}
	if r.Err != nil {
		t.Errorf("unexpected error: %v", r.Err)
	}
	if !mut.convertToDraftCalled {
		t.Error("ConvertToDraft should have been called")
	}
}

func TestExecute_Draft_Error(t *testing.T) {
	mut := &fakePRMutator{convertToDraftErr: errors.New("gql error")}
	store := &fakePRStore{prs: samplePRs(), sessionIDs: sampleSessionIDs()}
	exec := newExec(mut, store)
	msg := runCmd(t, exec.Execute(":draft", []string{"a"}))
	r := msg.(CommandResultMsg)
	if r.Err == nil {
		t.Error("expected error from ConvertToDraft")
	}
}

func TestExecute_Draft_NilMutator(t *testing.T) {
	store := &fakePRStore{prs: samplePRs(), sessionIDs: sampleSessionIDs()}
	exec := NewCommandExecutor(CommandExecutorConfig{Store: store})
	msg := runCmd(t, exec.Execute(":draft", []string{"a"}))
	r := msg.(CommandResultMsg)
	if r.Err == nil {
		t.Error("expected error when mutator is nil")
	}
}

// ── :request ──────────────────────────────────────────────────────────────────

func TestExecute_Request(t *testing.T) {
	mut := &fakePRMutator{}
	store := &fakePRStore{prs: samplePRs(), sessionIDs: sampleSessionIDs()}
	exec := newExec(mut, store)
	msg := runCmd(t, exec.Execute(":request", []string{"a", "@alice", "@bob"}))
	r, ok := msg.(CommandResultMsg)
	if !ok {
		t.Fatalf("expected CommandResultMsg, got %T", msg)
	}
	if r.Err != nil {
		t.Errorf("unexpected error: %v", r.Err)
	}
	if !mut.requestReviewCalled {
		t.Error("RequestReview should have been called")
	}
	if len(mut.lastRequestUsers) != 2 {
		t.Errorf("expected 2 users, got %v", mut.lastRequestUsers)
	}
}

func TestExecute_Request_NoArgs(t *testing.T) {
	exec := newExec(&fakePRMutator{}, &fakePRStore{})
	msg := runCmd(t, exec.Execute(":request", nil))
	r := msg.(CommandResultMsg)
	if r.Err == nil {
		t.Error("expected error when no args given")
	}
}

func TestExecute_Request_NoReviewers(t *testing.T) {
	store := &fakePRStore{prs: samplePRs(), sessionIDs: sampleSessionIDs()}
	exec := newExec(&fakePRMutator{}, store)
	msg := runCmd(t, exec.Execute(":request", []string{"a"}))
	r := msg.(CommandResultMsg)
	if r.Err == nil {
		t.Error("expected error when no reviewers specified")
	}
}

func TestExecute_Request_NilMutator(t *testing.T) {
	store := &fakePRStore{prs: samplePRs(), sessionIDs: sampleSessionIDs()}
	exec := NewCommandExecutor(CommandExecutorConfig{Store: store})
	msg := runCmd(t, exec.Execute(":request", []string{"a", "@alice"}))
	r := msg.(CommandResultMsg)
	if r.Err == nil {
		t.Error("expected error when mutator is nil")
	}
}

func TestExecute_Request_Error(t *testing.T) {
	mut := &fakePRMutator{requestReviewErr: errors.New("no such user")}
	store := &fakePRStore{prs: samplePRs(), sessionIDs: sampleSessionIDs()}
	exec := newExec(mut, store)
	msg := runCmd(t, exec.Execute(":request", []string{"a", "@alice"}))
	r := msg.(CommandResultMsg)
	if r.Err == nil {
		t.Error("expected error from RequestReview")
	}
}

func TestExecute_Request_PRNotFound(t *testing.T) {
	store := &fakePRStore{prs: samplePRs(), sessionIDs: sampleSessionIDs()}
	exec := newExec(&fakePRMutator{}, store)
	msg := runCmd(t, exec.Execute(":request", []string{"z", "@alice"}))
	r := msg.(CommandResultMsg)
	if r.Err == nil {
		t.Error("expected error for unknown PR ref")
	}
}

// ── :request with suggester ───────────────────────────────────────────────────

type fakeReviewSuggester struct {
	suggestions []string
	err         error
}

func (f *fakeReviewSuggester) SuggestReviewers(_ context.Context, _ string, _ int) ([]string, error) {
	return f.suggestions, f.err
}

func TestExecute_Request_WithSuggester_ReturnsSuggestions(t *testing.T) {
	store := &fakePRStore{prs: samplePRs(), sessionIDs: sampleSessionIDs()}
	suggester := &fakeReviewSuggester{suggestions: []string{"alice", "bob"}}
	exec := NewCommandExecutor(CommandExecutorConfig{
		Store:     store,
		Suggester: suggester,
	})
	msg := runCmd(t, exec.Execute(":request", []string{"a"}))
	r, ok := msg.(ReviewSuggestionsMsg)
	if !ok {
		t.Fatalf("expected ReviewSuggestionsMsg, got %T: %v", msg, msg)
	}
	if len(r.Suggestions) != 2 {
		t.Errorf("want 2 suggestions, got %v", r.Suggestions)
	}
	if r.Suggestions[0] != "alice" || r.Suggestions[1] != "bob" {
		t.Errorf("unexpected suggestions: %v", r.Suggestions)
	}
	// Input prefix should use session ID "a" (sampleSessionIDs maps samplePRs()[0].URL → "a")
	if r.InputPrefix == "" {
		t.Error("InputPrefix should be non-empty")
	}
}

func TestExecute_Request_WithSuggester_UsesNumberWhenNoSessionID(t *testing.T) {
	// Store without session IDs so GetSessionID returns an error.
	store := &fakePRStore{prs: samplePRs()}
	suggester := &fakeReviewSuggester{suggestions: []string{"carol"}}
	exec := NewCommandExecutor(CommandExecutorConfig{
		Store:     store,
		Suggester: suggester,
	})
	msg := runCmd(t, exec.Execute(":request", []string{"42"}))
	r, ok := msg.(ReviewSuggestionsMsg)
	if !ok {
		t.Fatalf("expected ReviewSuggestionsMsg, got %T: %v", msg, msg)
	}
	// InputPrefix should fall back to #number format.
	if r.InputPrefix != ":request #42 @" {
		t.Errorf("InputPrefix = %q, want %q", r.InputPrefix, ":request #42 @")
	}
}

func TestExecute_Request_WithSuggester_Error(t *testing.T) {
	store := &fakePRStore{prs: samplePRs(), sessionIDs: sampleSessionIDs()}
	suggester := &fakeReviewSuggester{err: errors.New("API down")}
	exec := NewCommandExecutor(CommandExecutorConfig{
		Store:     store,
		Suggester: suggester,
	})
	msg := runCmd(t, exec.Execute(":request", []string{"a"}))
	r, ok := msg.(CommandResultMsg)
	if !ok {
		t.Fatalf("expected CommandResultMsg, got %T", msg)
	}
	if r.Err == nil {
		t.Error("expected error from suggester")
	}
}

// ── :label ────────────────────────────────────────────────────────────────────

func TestExecute_Label_Add(t *testing.T) {
	mut := &fakePRMutator{}
	store := &fakePRStore{prs: samplePRs(), sessionIDs: sampleSessionIDs()}
	exec := newExec(mut, store)
	msg := runCmd(t, exec.Execute(":label", []string{"a", "bug"}))
	r, ok := msg.(CommandResultMsg)
	if !ok {
		t.Fatalf("expected CommandResultMsg, got %T", msg)
	}
	if r.Err != nil {
		t.Errorf("unexpected error: %v", r.Err)
	}
	if !mut.addLabelCalled {
		t.Error("AddLabel should have been called")
	}
	if mut.lastLabel != "bug" {
		t.Errorf("expected label 'bug', got %q", mut.lastLabel)
	}
}

func TestExecute_Label_Remove(t *testing.T) {
	mut := &fakePRMutator{}
	store := &fakePRStore{prs: samplePRs(), sessionIDs: sampleSessionIDs()}
	exec := newExec(mut, store)
	msg := runCmd(t, exec.Execute(":label", []string{"a", "-bug"}))
	r, ok := msg.(CommandResultMsg)
	if !ok {
		t.Fatalf("expected CommandResultMsg, got %T", msg)
	}
	if r.Err != nil {
		t.Errorf("unexpected error: %v", r.Err)
	}
	if !mut.removeLabelCalled {
		t.Error("RemoveLabel should have been called")
	}
	if mut.lastLabel != "bug" {
		t.Errorf("expected label 'bug', got %q", mut.lastLabel)
	}
}

func TestExecute_Label_TooFewArgs(t *testing.T) {
	store := &fakePRStore{prs: samplePRs(), sessionIDs: sampleSessionIDs()}
	exec := newExec(&fakePRMutator{}, store)
	msg := runCmd(t, exec.Execute(":label", []string{"a"}))
	r := msg.(CommandResultMsg)
	if r.Err == nil {
		t.Error("expected error when label is missing")
	}
}

func TestExecute_Label_NilMutator(t *testing.T) {
	store := &fakePRStore{prs: samplePRs(), sessionIDs: sampleSessionIDs()}
	exec := NewCommandExecutor(CommandExecutorConfig{Store: store})
	msg := runCmd(t, exec.Execute(":label", []string{"a", "bug"}))
	r := msg.(CommandResultMsg)
	if r.Err == nil {
		t.Error("expected error when mutator is nil")
	}
}

func TestExecute_Label_AddError(t *testing.T) {
	mut := &fakePRMutator{addLabelErr: errors.New("label error")}
	store := &fakePRStore{prs: samplePRs(), sessionIDs: sampleSessionIDs()}
	exec := newExec(mut, store)
	msg := runCmd(t, exec.Execute(":label", []string{"a", "bug"}))
	r := msg.(CommandResultMsg)
	if r.Err == nil {
		t.Error("expected error from AddLabel")
	}
}

func TestExecute_Label_RemoveError(t *testing.T) {
	mut := &fakePRMutator{removeLabelErr: errors.New("label error")}
	store := &fakePRStore{prs: samplePRs(), sessionIDs: sampleSessionIDs()}
	exec := newExec(mut, store)
	msg := runCmd(t, exec.Execute(":label", []string{"a", "-bug"}))
	r := msg.(CommandResultMsg)
	if r.Err == nil {
		t.Error("expected error from RemoveLabel")
	}
}

func TestExecute_Label_PRNotFound(t *testing.T) {
	store := &fakePRStore{prs: samplePRs(), sessionIDs: sampleSessionIDs()}
	exec := newExec(&fakePRMutator{}, store)
	msg := runCmd(t, exec.Execute(":label", []string{"z", "bug"}))
	r := msg.(CommandResultMsg)
	if r.Err == nil {
		t.Error("expected error for unknown PR ref")
	}
}

// ── :comment ─────────────────────────────────────────────────────────────────

func TestExecute_Comment_ReturnsComposeMsg(t *testing.T) {
	store := &fakePRStore{prs: samplePRs(), sessionIDs: sampleSessionIDs()}
	exec := newExec(&fakePRMutator{}, store)
	msg := runCmd(t, exec.Execute(":comment", []string{"a"}))
	compose, ok := msg.(CommandComposeMsg)
	if !ok {
		t.Fatalf("expected CommandComposeMsg, got %T", msg)
	}
	if compose.Prompt == "" {
		t.Error("expected non-empty prompt")
	}
	if compose.OnSubmit == nil {
		t.Error("expected non-nil OnSubmit")
	}
}

func TestExecute_Comment_OnSubmit_Success(t *testing.T) {
	mut := &fakePRMutator{}
	store := &fakePRStore{prs: samplePRs(), sessionIDs: sampleSessionIDs()}
	exec := newExec(mut, store)
	msg := runCmd(t, exec.Execute(":comment", []string{"a"}))
	compose := msg.(CommandComposeMsg)
	result := runCmd(t, compose.OnSubmit("hello world"))
	r, ok := result.(CommandResultMsg)
	if !ok {
		t.Fatalf("expected CommandResultMsg, got %T", result)
	}
	if r.Err != nil {
		t.Errorf("unexpected error: %v", r.Err)
	}
	if mut.lastCommentBody != "hello world" {
		t.Errorf("expected body 'hello world', got %q", mut.lastCommentBody)
	}
}

func TestExecute_Comment_OnSubmit_MutatorError(t *testing.T) {
	mut := &fakePRMutator{postCommentErr: errors.New("comment failed")}
	store := &fakePRStore{prs: samplePRs(), sessionIDs: sampleSessionIDs()}
	exec := newExec(mut, store)
	msg := runCmd(t, exec.Execute(":comment", []string{"a"}))
	compose := msg.(CommandComposeMsg)
	result := runCmd(t, compose.OnSubmit("hello"))
	r := result.(CommandResultMsg)
	if r.Err == nil {
		t.Error("expected error from PostComment")
	}
}

func TestExecute_Comment_OnSubmit_NilMutator(t *testing.T) {
	store := &fakePRStore{prs: samplePRs(), sessionIDs: sampleSessionIDs()}
	exec := NewCommandExecutor(CommandExecutorConfig{Store: store})
	msg := runCmd(t, exec.Execute(":comment", []string{"a"}))
	compose := msg.(CommandComposeMsg)
	result := runCmd(t, compose.OnSubmit("hello"))
	r := result.(CommandResultMsg)
	if r.Err == nil {
		t.Error("expected error when mutator is nil")
	}
}

func TestExecute_Comment_PRNotFound(t *testing.T) {
	store := &fakePRStore{prs: samplePRs(), sessionIDs: sampleSessionIDs()}
	exec := newExec(&fakePRMutator{}, store)
	msg := runCmd(t, exec.Execute(":comment", []string{"z"}))
	r, ok := msg.(CommandResultMsg)
	if !ok {
		t.Fatalf("expected CommandResultMsg (error), got %T", msg)
	}
	if r.Err == nil {
		t.Error("expected error for unknown PR ref")
	}
}

// ── :review ───────────────────────────────────────────────────────────────────

func TestExecute_Review_ReturnsComposeMsg(t *testing.T) {
	store := &fakePRStore{prs: samplePRs(), sessionIDs: sampleSessionIDs()}
	exec := newExec(&fakePRMutator{}, store)
	msg := runCmd(t, exec.Execute(":review", []string{"a"}))
	compose, ok := msg.(CommandComposeMsg)
	if !ok {
		t.Fatalf("expected CommandComposeMsg, got %T", msg)
	}
	if compose.OnSubmit == nil {
		t.Error("expected non-nil OnSubmit")
	}
}

func TestExecute_Review_OnSubmit_Success(t *testing.T) {
	mut := &fakePRMutator{}
	store := &fakePRStore{prs: samplePRs(), sessionIDs: sampleSessionIDs()}
	exec := newExec(mut, store)
	msg := runCmd(t, exec.Execute(":review", []string{"a"}))
	compose := msg.(CommandComposeMsg)
	result := runCmd(t, compose.OnSubmit("LGTM"))
	r, ok := result.(CommandResultMsg)
	if !ok {
		t.Fatalf("expected CommandResultMsg, got %T", result)
	}
	if r.Err != nil {
		t.Errorf("unexpected error: %v", r.Err)
	}
}

func TestExecute_Review_OnSubmit_MutatorError(t *testing.T) {
	mut := &fakePRMutator{postCommentErr: errors.New("review failed")}
	store := &fakePRStore{prs: samplePRs(), sessionIDs: sampleSessionIDs()}
	exec := newExec(mut, store)
	msg := runCmd(t, exec.Execute(":review", []string{"a"}))
	compose := msg.(CommandComposeMsg)
	result := runCmd(t, compose.OnSubmit("LGTM"))
	r := result.(CommandResultMsg)
	if r.Err == nil {
		t.Error("expected error from PostComment in :review")
	}
}

func TestExecute_Review_OnSubmit_NilMutator(t *testing.T) {
	store := &fakePRStore{prs: samplePRs(), sessionIDs: sampleSessionIDs()}
	exec := NewCommandExecutor(CommandExecutorConfig{Store: store})
	msg := runCmd(t, exec.Execute(":review", []string{"a"}))
	compose := msg.(CommandComposeMsg)
	result := runCmd(t, compose.OnSubmit("LGTM"))
	r := result.(CommandResultMsg)
	if r.Err == nil {
		t.Error("expected error when mutator is nil")
	}
}

func TestExecute_Review_PRNotFound(t *testing.T) {
	store := &fakePRStore{prs: samplePRs(), sessionIDs: sampleSessionIDs()}
	exec := newExec(&fakePRMutator{}, store)
	msg := runCmd(t, exec.Execute(":review", []string{"z"}))
	r, ok := msg.(CommandResultMsg)
	if !ok {
		t.Fatalf("expected CommandResultMsg (error), got %T", msg)
	}
	if r.Err == nil {
		t.Error("expected error for unknown PR ref")
	}
}

// ── :watch ────────────────────────────────────────────────────────────────────

func TestExecute_Watch(t *testing.T) {
	we := &fakeWatchEngine{}
	store := &fakePRStore{prs: samplePRs(), sessionIDs: sampleSessionIDs()}
	exec := NewCommandExecutor(CommandExecutorConfig{Watches: we, Store: store})
	msg := runCmd(t, exec.Execute(":watch", []string{"a", "on:ci-pass", "merge"}))
	r, ok := msg.(CommandResultMsg)
	if !ok {
		t.Fatalf("expected CommandResultMsg, got %T", msg)
	}
	if r.Err != nil {
		t.Errorf("unexpected error: %v", r.Err)
	}
	if !we.addCalled {
		t.Error("AddWatch should have been called")
	}
}

func TestExecute_Watch_TooFewArgs(t *testing.T) {
	we := &fakeWatchEngine{}
	store := &fakePRStore{prs: samplePRs(), sessionIDs: sampleSessionIDs()}
	exec := NewCommandExecutor(CommandExecutorConfig{Watches: we, Store: store})
	msg := runCmd(t, exec.Execute(":watch", []string{"a", "on:ci"}))
	r := msg.(CommandResultMsg)
	if r.Err == nil {
		t.Error("expected error when watch args are insufficient")
	}
}

func TestExecute_Watch_NilEngine(t *testing.T) {
	store := &fakePRStore{prs: samplePRs(), sessionIDs: sampleSessionIDs()}
	exec := NewCommandExecutor(CommandExecutorConfig{Store: store})
	msg := runCmd(t, exec.Execute(":watch", []string{"a", "on:ci-pass", "merge"}))
	r := msg.(CommandResultMsg)
	if r.Err == nil {
		t.Error("expected error when watch engine is nil")
	}
}

func TestExecute_Watch_NoArgs(t *testing.T) {
	we := &fakeWatchEngine{}
	store := &fakePRStore{prs: samplePRs(), sessionIDs: sampleSessionIDs()}
	exec := NewCommandExecutor(CommandExecutorConfig{Watches: we, Store: store})
	msg := runCmd(t, exec.Execute(":watch", nil))
	r := msg.(CommandResultMsg)
	if r.Err == nil {
		t.Error("expected error when no args given")
	}
}

func TestExecute_Watch_Error(t *testing.T) {
	we := &fakeWatchEngine{addWatchErr: errors.New("watch error")}
	store := &fakePRStore{prs: samplePRs(), sessionIDs: sampleSessionIDs()}
	exec := NewCommandExecutor(CommandExecutorConfig{Watches: we, Store: store})
	msg := runCmd(t, exec.Execute(":watch", []string{"a", "on:ci-pass", "merge"}))
	r := msg.(CommandResultMsg)
	if r.Err == nil {
		t.Error("expected error from AddWatch")
	}
}

func TestExecute_Watch_PRNotFound(t *testing.T) {
	we := &fakeWatchEngine{}
	store := &fakePRStore{prs: samplePRs(), sessionIDs: sampleSessionIDs()}
	exec := NewCommandExecutor(CommandExecutorConfig{Watches: we, Store: store})
	msg := runCmd(t, exec.Execute(":watch", []string{"z", "on:ci-pass", "merge"}))
	r := msg.(CommandResultMsg)
	if r.Err == nil {
		t.Error("expected error for unknown PR ref")
	}
}

// ── :watch list ────────────────────────────────────────────────────────────────

func TestExecute_WatchList_NoWatches(t *testing.T) {
	we := &fakeWatchEngine{watches: nil}
	store := &fakePRStore{prs: samplePRs(), sessionIDs: sampleSessionIDs()}
	exec := NewCommandExecutor(CommandExecutorConfig{Watches: we, Store: store})
	msg := runCmd(t, exec.Execute(":watch", []string{"list"}))
	r, ok := msg.(CommandResultMsg)
	if !ok {
		t.Fatalf("expected CommandResultMsg, got %T", msg)
	}
	if r.Err != nil {
		t.Errorf("unexpected error: %v", r.Err)
	}
	if !we.listCalled {
		t.Error("ListWatches should have been called")
	}
}

func TestExecute_WatchList_WithWatches(t *testing.T) {
	we := &fakeWatchEngine{
		watches: []persistence.Watch{
			{ID: "w1", Repo: "owner/repo", PRNumber: 42, TriggerExpr: "on:ci-pass", ActionExpr: "merge", Status: "waiting"},
			{ID: "w2", Repo: "owner/repo", PRNumber: 99, TriggerExpr: "on:approved", ActionExpr: "notify", Status: "waiting"},
		},
	}
	store := &fakePRStore{prs: samplePRs(), sessionIDs: sampleSessionIDs()}
	exec := NewCommandExecutor(CommandExecutorConfig{Watches: we, Store: store})
	msg := runCmd(t, exec.Execute(":watch", []string{"list"}))
	r, ok := msg.(CommandResultMsg)
	if !ok {
		t.Fatalf("expected CommandResultMsg, got %T", msg)
	}
	if r.Err != nil {
		t.Errorf("unexpected error: %v", r.Err)
	}
	if r.Status == "" {
		t.Error("expected non-empty status with watch list")
	}
}

func TestExecute_WatchList_Error(t *testing.T) {
	we := &fakeWatchEngine{listWatchesErr: errors.New("db error")}
	store := &fakePRStore{}
	exec := NewCommandExecutor(CommandExecutorConfig{Watches: we, Store: store})
	msg := runCmd(t, exec.Execute(":watch", []string{"list"}))
	r := msg.(CommandResultMsg)
	if r.Err == nil {
		t.Error("expected error from ListWatches")
	}
}

// ── :watch cancel ─────────────────────────────────────────────────────────────

func TestExecute_WatchCancel_Success(t *testing.T) {
	we := &fakeWatchEngine{}
	store := &fakePRStore{}
	exec := NewCommandExecutor(CommandExecutorConfig{Watches: we, Store: store})
	msg := runCmd(t, exec.Execute(":watch", []string{"cancel", "w42"}))
	r, ok := msg.(CommandResultMsg)
	if !ok {
		t.Fatalf("expected CommandResultMsg, got %T", msg)
	}
	if r.Err != nil {
		t.Errorf("unexpected error: %v", r.Err)
	}
	if !we.cancelCalled {
		t.Error("CancelWatch should have been called")
	}
	if we.lastCancelID != "w42" {
		t.Errorf("CancelWatch called with ID %q, want %q", we.lastCancelID, "w42")
	}
}

func TestExecute_WatchCancel_NoID(t *testing.T) {
	we := &fakeWatchEngine{}
	store := &fakePRStore{}
	exec := NewCommandExecutor(CommandExecutorConfig{Watches: we, Store: store})
	msg := runCmd(t, exec.Execute(":watch", []string{"cancel"}))
	r := msg.(CommandResultMsg)
	if r.Err == nil {
		t.Error("expected error when cancel ID is missing")
	}
}

func TestExecute_WatchCancel_Error(t *testing.T) {
	we := &fakeWatchEngine{cancelWatchErr: errors.New("cancel failed")}
	store := &fakePRStore{}
	exec := NewCommandExecutor(CommandExecutorConfig{Watches: we, Store: store})
	msg := runCmd(t, exec.Execute(":watch", []string{"cancel", "w1"}))
	r := msg.(CommandResultMsg)
	if r.Err == nil {
		t.Error("expected error from CancelWatch")
	}
}

// ── unknown command ───────────────────────────────────────────────────────────

func TestExecute_UnknownCommand(t *testing.T) {
	exec := newExec(&fakePRMutator{}, &fakePRStore{})
	msg := runCmd(t, exec.Execute(":notacommand", nil))
	r, ok := msg.(CommandResultMsg)
	if !ok {
		t.Fatalf("expected CommandResultMsg, got %T", msg)
	}
	if r.Err == nil {
		t.Error("expected error for unknown command")
	}
}

// ── firstArg helper ───────────────────────────────────────────────────────────

func TestFirstArg(t *testing.T) {
	if firstArg(nil) != "" {
		t.Error("firstArg(nil) should return empty string")
	}
	if firstArg([]string{}) != "" {
		t.Error("firstArg([]) should return empty string")
	}
	if firstArg([]string{"a", "b"}) != "a" {
		t.Error("firstArg should return first element")
	}
}
