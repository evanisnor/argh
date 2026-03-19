package ui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"

	"github.com/evanisnor/argh/internal/persistence"
)

// ── test doubles ──────────────────────────────────────────────────────────────

// stubMarkdownRenderer captures Render calls and returns a fixed string.
type stubMarkdownRenderer struct {
	output string
	err    error
}

func (s *stubMarkdownRenderer) Render(in string) (string, error) {
	if s.err != nil {
		return "", s.err
	}
	return s.output, nil
}

// stubThreadResolver captures ResolveReviewThread calls.
type stubThreadResolver struct {
	called   []string // thread IDs resolved
	err      error
}

func (s *stubThreadResolver) ResolveReviewThread(_ context.Context, threadID string) error {
	if s.err != nil {
		return s.err
	}
	s.called = append(s.called, threadID)
	return nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

func makeDetailPane(resolver ThreadResolver, mdOut string) *DetailPane {
	md := &stubMarkdownRenderer{output: mdOut}
	return newDetailPaneWithRenderer(resolver, md)
}

func makePR(id, title string) persistence.PullRequest {
	return persistence.PullRequest{ID: id, Title: title, Repo: "owner/repo", Number: 1}
}

func makeThread(id, path string, resolved bool) persistence.ReviewThread {
	return persistence.ReviewThread{ID: id, Path: path, Resolved: resolved, Body: "comment body", Line: 10}
}

func makeFocusMsg(pr persistence.PullRequest, threads []persistence.ReviewThread) PRFocusedMsg {
	return PRFocusedMsg{
		PR:      pr,
		Threads: threads,
	}
}

// sendMsg drives Update and returns the updated pane.
func sendMsg(p *DetailPane, msg tea.Msg) *DetailPane {
	updated, _ := p.Update(msg)
	return updated.(*DetailPane)
}

// ── tests ─────────────────────────────────────────────────────────────────────

// TestDetailPane_CollapsedByDefault verifies the pane is not visible initially.
func TestDetailPane_CollapsedByDefault(t *testing.T) {
	p := makeDetailPane(nil, "")
	if p.visible {
		t.Error("pane should be collapsed by default")
	}
}

// TestDetailPane_ViewEmptyWhenNotVisible verifies View() returns empty string when hidden.
func TestDetailPane_ViewEmptyWhenNotVisible(t *testing.T) {
	p := makeDetailPane(nil, "rendered markdown")
	view := p.View()
	if view != "" {
		t.Errorf("View() should be empty when not visible, got: %q", view)
	}
}

// TestDetailPane_Toggle verifies Toggle() flips visibility.
func TestDetailPane_Toggle(t *testing.T) {
	p := makeDetailPane(nil, "")

	p.Toggle()
	if !p.visible {
		t.Error("pane should be visible after Toggle()")
	}

	p.Toggle()
	if p.visible {
		t.Error("pane should be hidden after second Toggle()")
	}
}

// TestDetailPane_EnterAndPKeysToggleViaModel verifies that Enter and p in model.go
// toggle detailOpen (which is tested by existing model tests; here we verify the
// detail pane's own Toggle mechanism since model.go calls it at the root level).
func TestDetailPane_EnterAndPKeysToggleViaModel(t *testing.T) {
	tests := []struct {
		name string
		key  tea.KeyMsg
	}{
		{"Enter", tea.KeyMsg{Type: tea.KeyEnter}},
		{"p", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			detail := newStub("detail", false)
			m, _ := newTestModel(
				newStub("myPRs", true), newStub("reviewQueue", true),
				newStub("watches", false), detail, newStub("cmdBar", false),
			)

			if m.detailOpen {
				t.Fatal("detail should be closed initially")
			}
			m = applyMsg(m, tt.key)
			if !m.detailOpen {
				t.Errorf("detail should be open after pressing %s", tt.name)
			}
			m = applyMsg(m, tt.key)
			if m.detailOpen {
				t.Errorf("detail should be closed after pressing %s again", tt.name)
			}
		})
	}
}

// TestDetailPane_PRDescriptionRenderedAsMarkdown verifies that a PR description
// is sent through the markdown renderer and the output appears in the view.
func TestDetailPane_PRDescriptionRenderedAsMarkdown(t *testing.T) {
	const markdownOut = "**rendered markdown output**"
	p := makeDetailPane(nil, markdownOut)
	p.visible = true

	pr := makePR("pr1", "My PR Title")
	p = sendMsg(p, makeFocusMsg(pr, nil))

	view := p.View()
	if !strings.Contains(view, markdownOut) {
		t.Errorf("View() should contain rendered markdown %q, got:\n%s", markdownOut, view)
	}
}

// TestDetailPane_MarkdownRendererError falls back to raw title on renderer error.
func TestDetailPane_MarkdownRendererError(t *testing.T) {
	md := &stubMarkdownRenderer{err: errors.New("render failed")}
	p := newDetailPaneWithRenderer(nil, md)
	p.visible = true

	pr := makePR("pr1", "My Raw Title")
	p = sendMsg(p, makeFocusMsg(pr, nil))

	view := p.View()
	if !strings.Contains(view, "My Raw Title") {
		t.Errorf("View() should contain raw title when renderer fails, got:\n%s", view)
	}
}

// TestDetailPane_NoThreads_NNavIsNoop verifies n/N are no-ops when no threads.
func TestDetailPane_NoThreads_NNavIsNoop(t *testing.T) {
	p := makeDetailPane(nil, "")
	p.visible = true
	p = sendMsg(p, makeFocusMsg(makePR("pr1", "PR"), nil))

	before := p.currentThread

	p = sendMsg(p, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if p.currentThread != before {
		t.Errorf("currentThread changed on n with no threads: %d → %d", before, p.currentThread)
	}

	p = sendMsg(p, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("N")})
	if p.currentThread != before {
		t.Errorf("currentThread changed on N with no threads: %d → %d", before, p.currentThread)
	}
}

// TestDetailPane_NNavigation_CyclesOpenThreads verifies n advances through open
// threads only (resolved threads are excluded) and wraps around.
func TestDetailPane_NNavigation_CyclesOpenThreads(t *testing.T) {
	p := makeDetailPane(nil, "")
	p.visible = true

	threads := []persistence.ReviewThread{
		makeThread("t1", "file.go", false), // open
		makeThread("t2", "file.go", true),  // resolved — should be skipped
		makeThread("t3", "file.go", false), // open
	}
	p = sendMsg(p, makeFocusMsg(makePR("pr1", "PR"), threads))

	// Initially at index 0 (t1).
	if p.currentThread != 0 {
		t.Fatalf("expected currentThread=0, got %d", p.currentThread)
	}
	if len(p.openThreads) != 2 {
		t.Fatalf("expected 2 open threads, got %d", len(p.openThreads))
	}
	if p.openThreads[0].ID != "t1" {
		t.Errorf("expected openThreads[0]=t1, got %s", p.openThreads[0].ID)
	}
	if p.openThreads[1].ID != "t3" {
		t.Errorf("expected openThreads[1]=t3, got %s", p.openThreads[1].ID)
	}

	// n → index 1 (t3)
	p = sendMsg(p, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if p.currentThread != 1 {
		t.Errorf("expected currentThread=1, got %d", p.currentThread)
	}

	// n → wraps to 0 (t1)
	p = sendMsg(p, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if p.currentThread != 0 {
		t.Errorf("expected currentThread=0 (wrap), got %d", p.currentThread)
	}
}

// TestDetailPane_NUpperNavigation_CyclesBackward verifies N moves backward
// through open threads and wraps around.
func TestDetailPane_NUpperNavigation_CyclesBackward(t *testing.T) {
	p := makeDetailPane(nil, "")
	p.visible = true

	threads := []persistence.ReviewThread{
		makeThread("t1", "a.go", false),
		makeThread("t2", "b.go", false),
		makeThread("t3", "c.go", false),
	}
	p = sendMsg(p, makeFocusMsg(makePR("pr1", "PR"), threads))

	// N from 0 → wraps to 2
	p = sendMsg(p, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("N")})
	if p.currentThread != 2 {
		t.Errorf("expected currentThread=2 (wrap backward), got %d", p.currentThread)
	}

	// N → 1
	p = sendMsg(p, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("N")})
	if p.currentThread != 1 {
		t.Errorf("expected currentThread=1, got %d", p.currentThread)
	}
}

// TestDetailPane_MarkResolved_CallsResolver verifies that pressing r sends the
// resolveReviewThread mutation with the correct thread ID.
func TestDetailPane_MarkResolved_CallsResolver(t *testing.T) {
	resolver := &stubThreadResolver{}
	p := makeDetailPane(resolver, "")
	p.visible = true

	threads := []persistence.ReviewThread{
		makeThread("thread-abc", "file.go", false),
	}
	p = sendMsg(p, makeFocusMsg(makePR("pr1", "PR"), threads))

	// Press r to resolve.
	_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if cmd == nil {
		t.Fatal("resolveCurrentThread should return a non-nil Cmd")
	}

	// Execute the Cmd (calls the resolver).
	msg := cmd()
	resolved, ok := msg.(ThreadResolvedMsg)
	if !ok {
		t.Fatalf("cmd() returned %T, want ThreadResolvedMsg", msg)
	}
	if resolved.ThreadID != "thread-abc" {
		t.Errorf("ThreadID = %q, want thread-abc", resolved.ThreadID)
	}
	if len(resolver.called) != 1 || resolver.called[0] != "thread-abc" {
		t.Errorf("resolver.called = %v, want [thread-abc]", resolver.called)
	}
}

// TestDetailPane_MarkResolved_ResolverError returns nil msg on error.
func TestDetailPane_MarkResolved_ResolverError(t *testing.T) {
	resolver := &stubThreadResolver{err: errors.New("network error")}
	p := makeDetailPane(resolver, "")
	p.visible = true

	threads := []persistence.ReviewThread{makeThread("t1", "f.go", false)}
	p = sendMsg(p, makeFocusMsg(makePR("pr1", "PR"), threads))

	_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if cmd == nil {
		t.Fatal("expected non-nil cmd even when resolver will fail")
	}

	msg := cmd()
	if msg != nil {
		t.Errorf("cmd() should return nil on resolver error, got %T", msg)
	}
}

// TestDetailPane_MarkResolved_NilResolver is a no-op when resolver is nil.
func TestDetailPane_MarkResolved_NilResolver(t *testing.T) {
	p := makeDetailPane(nil, "")
	p.visible = true

	threads := []persistence.ReviewThread{makeThread("t1", "f.go", false)}
	p = sendMsg(p, makeFocusMsg(makePR("pr1", "PR"), threads))

	_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if cmd != nil {
		t.Error("resolveCurrentThread should return nil when resolver is nil")
	}
}

// TestDetailPane_ThreadResolvedMsg_DisappearsFromNav verifies that after a
// ThreadResolvedMsg, the resolved thread is excluded from n/N navigation.
func TestDetailPane_ThreadResolvedMsg_DisappearsFromNav(t *testing.T) {
	resolver := &stubThreadResolver{}
	p := makeDetailPane(resolver, "")
	p.visible = true

	threads := []persistence.ReviewThread{
		makeThread("t1", "a.go", false),
		makeThread("t2", "b.go", false),
	}
	p = sendMsg(p, makeFocusMsg(makePR("pr1", "PR"), threads))

	if len(p.openThreads) != 2 {
		t.Fatalf("expected 2 open threads initially, got %d", len(p.openThreads))
	}

	// Resolve t1 via ThreadResolvedMsg.
	p = sendMsg(p, ThreadResolvedMsg{ThreadID: "t1"})

	if len(p.openThreads) != 1 {
		t.Fatalf("expected 1 open thread after resolve, got %d", len(p.openThreads))
	}
	if p.openThreads[0].ID != "t2" {
		t.Errorf("expected remaining thread to be t2, got %s", p.openThreads[0].ID)
	}
}

// TestDetailPane_ThreadResolvedMsg_ClampsIndex verifies the cursor is clamped
// when the resolved thread was the last in the list.
func TestDetailPane_ThreadResolvedMsg_ClampsIndex(t *testing.T) {
	p := makeDetailPane(nil, "")
	p.visible = true

	threads := []persistence.ReviewThread{
		makeThread("t1", "a.go", false),
		makeThread("t2", "b.go", false),
	}
	p = sendMsg(p, makeFocusMsg(makePR("pr1", "PR"), threads))

	// Move to last thread.
	p = sendMsg(p, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if p.currentThread != 1 {
		t.Fatalf("expected currentThread=1, got %d", p.currentThread)
	}

	// Resolve t2 (the currently focused one, now at index 1).
	p = sendMsg(p, ThreadResolvedMsg{ThreadID: "t2"})

	if p.currentThread != 0 {
		t.Errorf("expected currentThread clamped to 0, got %d", p.currentThread)
	}
}

// TestDetailPane_ThreadResolvedMsg_AllResolved verifies cursor resets to 0 when
// all threads become resolved.
func TestDetailPane_ThreadResolvedMsg_AllResolved(t *testing.T) {
	p := makeDetailPane(nil, "")
	p.visible = true

	threads := []persistence.ReviewThread{makeThread("t1", "a.go", false)}
	p = sendMsg(p, makeFocusMsg(makePR("pr1", "PR"), threads))
	p = sendMsg(p, ThreadResolvedMsg{ThreadID: "t1"})

	if len(p.openThreads) != 0 {
		t.Errorf("expected 0 open threads, got %d", len(p.openThreads))
	}
	if p.currentThread != 0 {
		t.Errorf("expected currentThread=0, got %d", p.currentThread)
	}
}

// TestDetailPane_CheckRunsInView verifies check runs appear in the rendered view.
func TestDetailPane_CheckRunsInView(t *testing.T) {
	p := makeDetailPane(nil, "md")
	p.visible = true

	msg := PRFocusedMsg{
		PR: makePR("pr1", "PR"),
		CheckRuns: []persistence.CheckRun{
			{PRID: "pr1", Name: "lint", State: "completed", Conclusion: "success"},
			{PRID: "pr1", Name: "build", State: "in_progress", Conclusion: ""},
		},
	}
	p = sendMsg(p, msg)

	view := p.View()
	if !strings.Contains(view, "lint") {
		t.Errorf("expected 'lint' in view; got:\n%s", view)
	}
	if !strings.Contains(view, "build") {
		t.Errorf("expected 'build' in view; got:\n%s", view)
	}
}

// TestDetailPane_WatchesInView verifies active watches appear in the rendered view.
func TestDetailPane_WatchesInView(t *testing.T) {
	p := makeDetailPane(nil, "md")
	p.visible = true

	msg := PRFocusedMsg{
		PR: makePR("pr1", "PR"),
		Watches: []persistence.Watch{
			{ID: "w1", TriggerExpr: "on:ci-pass", ActionExpr: "merge", Status: "waiting"},
			{ID: "w2", TriggerExpr: "on:label", ActionExpr: "comment", Status: "fired"}, // not active
		},
	}
	p = sendMsg(p, msg)

	view := p.View()
	if !strings.Contains(view, "on:ci-pass") {
		t.Errorf("expected 'on:ci-pass' in view; got:\n%s", view)
	}
	// Fired watch should not appear in active section.
	if strings.Contains(view, "on:label") {
		t.Errorf("fired watch should not appear in active watches section; got:\n%s", view)
	}
}

// TestDetailPane_TimelineInView verifies timeline events appear in the view.
func TestDetailPane_TimelineInView(t *testing.T) {
	p := makeDetailPane(nil, "md")
	p.visible = true

	msg := PRFocusedMsg{
		PR: makePR("pr1", "PR"),
		TimelineEvents: []persistence.TimelineEvent{
			{PRID: "pr1", EventType: "commit", Actor: "alice"},
		},
	}
	p = sendMsg(p, msg)

	view := p.View()
	if !strings.Contains(view, "commit") {
		t.Errorf("expected 'commit' in view; got:\n%s", view)
	}
	if !strings.Contains(view, "alice") {
		t.Errorf("expected 'alice' in view; got:\n%s", view)
	}
}

// TestDetailPane_EmptyStateMessages verifies the empty-state strings appear
// when check runs, threads, watches, and timeline are all absent.
func TestDetailPane_EmptyStateMessages(t *testing.T) {
	p := makeDetailPane(nil, "md")
	p.visible = true
	p = sendMsg(p, makeFocusMsg(makePR("pr1", "PR"), nil))

	view := p.View()
	for _, want := range []string{
		"no check runs",
		"no open threads",
		"no active watches",
		"no timeline events",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("expected %q in empty-state view; got:\n%s", want, view)
		}
	}
}

// TestDetailPane_CheckRunStateSymbol verifies checkRunStateSymbol covers all branches.
func TestDetailPane_CheckRunStateSymbol(t *testing.T) {
	tests := []struct {
		state, conclusion, want string
	}{
		{"completed", "success", "✓"},
		{"completed", "failure", "✗"},
		{"completed", "timed_out", "✗"},
		{"in_progress", "", "⟳"},
		{"queued", "", "⟳"},
		{"unknown", "unknown", "—"},
	}
	for _, tt := range tests {
		got := checkRunStateSymbol(tt.state, tt.conclusion)
		if got != tt.want {
			t.Errorf("checkRunStateSymbol(%q,%q) = %q, want %q", tt.state, tt.conclusion, got, tt.want)
		}
	}
}

// TestDetailPane_Truncate verifies the truncate helper.
func TestDetailPane_Truncate(t *testing.T) {
	tests := []struct {
		input string
		max   int
		want  string
	}{
		{"hello", 10, "hello"},
		{"hello world", 5, "hello…"},
		{"hi", 2, "hi"},
		{"abc", 2, "ab…"},
	}
	for _, tt := range tests {
		got := truncate(tt.input, tt.max)
		if got != tt.want {
			t.Errorf("truncate(%q,%d) = %q, want %q", tt.input, tt.max, got, tt.want)
		}
	}
}

// TestDetailPane_ThreadNavigationInView verifies the focused thread marker (>)
// appears next to the current thread in the view.
func TestDetailPane_ThreadNavigationInView(t *testing.T) {
	p := makeDetailPane(nil, "")
	p.visible = true

	threads := []persistence.ReviewThread{
		makeThread("t1", "a.go", false),
		makeThread("t2", "b.go", false),
	}
	p = sendMsg(p, makeFocusMsg(makePR("pr1", "PR"), threads))

	view := p.View()
	if !strings.Contains(view, "> [thread 1]") {
		t.Errorf("expected '> [thread 1]' marker for focused thread; got:\n%s", view)
	}
}

// TestDetailPane_HasContent verifies HasContent always returns false.
func TestDetailPane_HasContent(t *testing.T) {
	p := makeDetailPane(nil, "")
	if p.HasContent() {
		t.Error("HasContent() should always return false")
	}
}

// TestDetailPane_Init verifies Init returns nil.
func TestDetailPane_Init(t *testing.T) {
	p := makeDetailPane(nil, "")
	cmd := p.Init()
	if cmd != nil {
		t.Error("Init() should return nil")
	}
}

// TestDetailPane_NewDetailPane verifies the exported constructor does not panic.
func TestDetailPane_NewDetailPane(t *testing.T) {
	resolver := &stubThreadResolver{}
	p := NewDetailPane(resolver)
	if p == nil {
		t.Fatal("NewDetailPane returned nil")
	}
	if p.visible {
		t.Error("pane should be hidden by default")
	}
}

// TestDetailPane_MarkResolved_NoThreadsIsNoop verifies r is a no-op when there are
// no open threads (resolver not called).
func TestDetailPane_MarkResolved_NoThreadsIsNoop(t *testing.T) {
	resolver := &stubThreadResolver{}
	p := makeDetailPane(resolver, "")
	p.visible = true

	// No threads loaded.
	_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if cmd != nil {
		t.Error("r should be a no-op when openThreads is empty")
	}
	if len(resolver.called) != 0 {
		t.Errorf("resolver should not be called: %v", resolver.called)
	}
}

// TestDetailPane_NNkeyRoutedThroughModel verifies that the root model routes
// n and N to the detail pane when detailOpen is true.
func TestDetailPane_NNkeyRoutedThroughModel(t *testing.T) {
	detail := newStub("detail", false)
	m, _ := newTestModel(
		newStub("myPRs", true), newStub("reviewQueue", true),
		newStub("watches", false), detail, newStub("cmdBar", false),
	)
	m.detailOpen = true

	m = applyMsg(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if m.detailPane.(*stubSubModel).lastMsg == nil {
		t.Error("detail pane should receive 'n' key when detailOpen is true")
	}

	detail2 := newStub("detail2", false)
	m2, _ := newTestModel(
		newStub("myPRs", true), newStub("reviewQueue", true),
		newStub("watches", false), detail2, newStub("cmdBar", false),
	)
	m2.detailOpen = true

	m2 = applyMsg(m2, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("N")})
	if m2.detailPane.(*stubSubModel).lastMsg == nil {
		t.Error("detail pane should receive 'N' key when detailOpen is true")
	}
}

// TestDetailPane_NNkeyNotRoutedWhenClosed verifies n/N do NOT go to detail pane
// when it is closed.
func TestDetailPane_NNkeyNotRoutedWhenClosed(t *testing.T) {
	detail := newStub("detail", false)
	m, _ := newTestModel(
		newStub("myPRs", true), newStub("reviewQueue", true),
		newStub("watches", false), detail, newStub("cmdBar", false),
	)
	// detailOpen is false by default

	m = applyMsg(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if m.detailPane.(*stubSubModel).lastMsg != nil {
		t.Error("detail pane should NOT receive 'n' key when detailOpen is false")
	}
}

// TestDetailPane_MarkdownNilRenderer verifies the pane handles a nil renderer
// gracefully (falls back to raw title).
func TestDetailPane_MarkdownNilRenderer(t *testing.T) {
	p := newDetailPaneWithRenderer(nil, nil)
	p.visible = true

	pr := makePR("pr1", "Fallback Title")
	p = sendMsg(p, makeFocusMsg(pr, nil))

	view := p.View()
	if !strings.Contains(view, "Fallback Title") {
		t.Errorf("View() should contain raw title with nil renderer; got:\n%s", view)
	}
}

// TestDetailPane_MarkdownEmptyRendered falls back to raw title when rendered output is empty.
func TestDetailPane_MarkdownEmptyRendered(t *testing.T) {
	md := &stubMarkdownRenderer{output: "   \n  "}
	p := newDetailPaneWithRenderer(nil, md)
	p.visible = true

	pr := makePR("pr1", "Raw Title Fallback")
	p = sendMsg(p, makeFocusMsg(pr, nil))

	view := p.View()
	if !strings.Contains(view, "Raw Title Fallback") {
		t.Errorf("View() should contain raw title when rendered is whitespace-only; got:\n%s", view)
	}
}

// TestDetailPane_GlamourRendererRender exercises the real glamourRenderer.Render wrapper.
func TestDetailPane_GlamourRendererRender(t *testing.T) {
	// Create a real glamour TermRenderer and wrap it.
	p := NewDetailPane(nil)
	p.visible = true

	pr := makePR("pr1", "# Heading")
	p = sendMsg(p, makeFocusMsg(pr, nil))

	view := p.View()
	// Glamour renders markdown so output should be non-empty.
	if strings.TrimSpace(view) == "" {
		t.Error("View() should be non-empty after rendering with real glamour renderer")
	}
}

// TestDefaultMarkdownRenderer_Error exercises the error path by replacing the
// constructor with one that always fails.
func TestDefaultMarkdownRenderer_Error(t *testing.T) {
	// Save and restore.
	orig := glamourNewTermRenderer
	defer func() { glamourNewTermRenderer = orig }()

	glamourNewTermRenderer = func(_ ...glamour.TermRendererOption) (*glamour.TermRenderer, error) {
		return nil, errors.New("glamour init error")
	}

	r, err := defaultMarkdownRenderer()
	if err == nil {
		t.Error("expected error from defaultMarkdownRenderer")
	}
	if r != nil {
		t.Error("expected nil renderer on error")
	}
}

// TestDetailPane_UpdateForwardsOtherKeysToViewport verifies that key events
// other than n/N/r are forwarded to the viewport.
func TestDetailPane_UpdateForwardsOtherKeysToViewport(t *testing.T) {
	p := makeDetailPane(nil, "md")
	p.visible = true

	pr := makePR("pr1", "PR")
	p = sendMsg(p, makeFocusMsg(pr, nil))

	// PageDown and down arrow are viewport keys — pane should not panic and should return itself.
	updated, _ := p.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	if updated == nil {
		t.Error("Update with PageDown should return non-nil SubModel")
	}

	// Also exercise with a key that maps to a viewport scroll (down arrow).
	updated, _ = p.Update(tea.KeyMsg{Type: tea.KeyDown})
	if updated == nil {
		t.Error("Update with Down key should return non-nil SubModel")
	}
}

// TestDetailPane_UpdateUnknownMessage verifies that an unrecognised message
// type returns the pane unchanged with a nil Cmd.
func TestDetailPane_UpdateUnknownMessage(t *testing.T) {
	p := makeDetailPane(nil, "md")

	type unknownMsg struct{}
	updated, cmd := p.Update(unknownMsg{})
	if updated == nil {
		t.Error("Update with unknown message should return non-nil SubModel")
	}
	if cmd != nil {
		t.Error("Update with unknown message should return nil Cmd")
	}
}
