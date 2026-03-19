package ui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"

	"github.com/evanisnor/argh/internal/persistence"
)

// PRFocusedMsg is sent when a PR gains focus (e.g. the user moves the cursor
// in one of the PR panels). The detail pane reacts by loading that PR's data.
type PRFocusedMsg struct {
	PR            persistence.PullRequest
	CheckRuns     []persistence.CheckRun
	Threads       []persistence.ReviewThread
	Watches       []persistence.Watch
	TimelineEvents []persistence.TimelineEvent
}

// ThreadResolvedMsg is sent (as a Cmd result) when a thread has been
// successfully resolved so that the pane can update its local state.
type ThreadResolvedMsg struct {
	ThreadID string
}

// ThreadResolver calls the resolveReviewThread GraphQL mutation.
type ThreadResolver interface {
	ResolveReviewThread(ctx context.Context, threadID string) error
}

// MarkdownRenderer abstracts glamour so it can be swapped in tests.
type MarkdownRenderer interface {
	Render(in string) (string, error)
}

// glamourRenderer is the real implementation backed by a glamour.TermRenderer.
type glamourRenderer struct {
	r *glamour.TermRenderer
}

func (g *glamourRenderer) Render(in string) (string, error) {
	return g.r.Render(in)
}

// glamourNewTermRenderer is the glamour constructor; replaced in tests to simulate errors.
var glamourNewTermRenderer = func(opts ...glamour.TermRendererOption) (*glamour.TermRenderer, error) {
	return glamour.NewTermRenderer(opts...)
}

// defaultMarkdownRenderer creates a glamour renderer with the auto style.
func defaultMarkdownRenderer() (MarkdownRenderer, error) {
	r, err := glamourNewTermRenderer(glamour.WithAutoStyle(), glamour.WithWordWrap(80))
	if err != nil {
		return nil, err
	}
	return &glamourRenderer{r: r}, nil
}

// DetailPane is the collapsible side/bottom pane that shows the focused PR's
// full detail: description, check runs, review threads, watches, and timeline.
type DetailPane struct {
	visible       bool
	pr            persistence.PullRequest
	checkRuns     []persistence.CheckRun
	threads       []persistence.ReviewThread // all threads (resolved + open)
	watches       []persistence.Watch
	timeline      []persistence.TimelineEvent
	openThreads   []persistence.ReviewThread // unresolved threads only
	currentThread int                        // index into openThreads
	viewport      viewport.Model
	resolver      ThreadResolver
	mdRenderer    MarkdownRenderer
}

// NewDetailPane creates a new DetailPane. resolver may be nil when no GitHub
// client is available (the pane will still render; mark-resolved will be a no-op).
func NewDetailPane(resolver ThreadResolver) *DetailPane {
	r, _ := defaultMarkdownRenderer()
	return newDetailPaneWithRenderer(resolver, r)
}

// newDetailPaneWithRenderer creates a DetailPane with an injected renderer
// (used in tests to replace glamour with a stub).
func newDetailPaneWithRenderer(resolver ThreadResolver, r MarkdownRenderer) *DetailPane {
	vp := viewport.New(80, 20)
	return &DetailPane{
		visible:    false,
		viewport:   vp,
		resolver:   resolver,
		mdRenderer: r,
	}
}

// Toggle flips the visible state of the pane.
func (p *DetailPane) Toggle() {
	p.visible = !p.visible
}

// Init satisfies tea.Model (no initial commands needed).
func (p *DetailPane) Init() tea.Cmd {
	return nil
}

// Update handles Bubble Tea messages.
func (p *DetailPane) Update(msg tea.Msg) (SubModel, tea.Cmd) {
	switch m := msg.(type) {
	case PRFocusedMsg:
		p.pr = m.PR
		p.checkRuns = m.CheckRuns
		p.threads = m.Threads
		p.watches = m.Watches
		p.timeline = m.TimelineEvents
		p.rebuildOpenThreads()
		p.currentThread = 0
		p.refreshViewport()
		return p, nil

	case ThreadResolvedMsg:
		// Mark the thread as resolved in our local thread list.
		for i := range p.threads {
			if p.threads[i].ID == m.ThreadID {
				p.threads[i].Resolved = true
			}
		}
		p.rebuildOpenThreads()
		// Clamp index.
		if p.currentThread >= len(p.openThreads) && len(p.openThreads) > 0 {
			p.currentThread = len(p.openThreads) - 1
		} else if len(p.openThreads) == 0 {
			p.currentThread = 0
		}
		p.refreshViewport()
		return p, nil

	case tea.KeyMsg:
		switch m.String() {
		case "n":
			return p, p.nextThread()
		case "N":
			return p, p.prevThread()
		case "r":
			return p, p.resolveCurrentThread()
		}
		// Forward other key events to the viewport for scrolling.
		var cmd tea.Cmd
		p.viewport, cmd = p.viewport.Update(msg)
		return p, cmd
	}

	return p, nil
}

// View renders the pane. Returns an empty string when the pane is not visible.
func (p *DetailPane) View() string {
	if !p.visible {
		return ""
	}
	return p.viewport.View()
}

// HasContent always returns false; the pane does not control its own
// show/hide in the panel grid (the root model does via detailOpen).
func (p *DetailPane) HasContent() bool {
	return false
}

// rebuildOpenThreads filters threads to only the unresolved ones.
func (p *DetailPane) rebuildOpenThreads() {
	p.openThreads = p.openThreads[:0]
	for _, t := range p.threads {
		if !t.Resolved {
			p.openThreads = append(p.openThreads, t)
		}
	}
}

// refreshViewport rebuilds the viewport content from current state.
func (p *DetailPane) refreshViewport() {
	p.viewport.SetContent(p.buildContent())
}

// buildContent composes the full pane text.
func (p *DetailPane) buildContent() string {
	var sb strings.Builder

	// ── PR description (Markdown) ─────────────────────────────────────────────
	sb.WriteString("── Description ─────────────────────────────────────────────\n")
	desc := p.pr.Title
	if p.mdRenderer != nil {
		rendered, err := p.mdRenderer.Render(desc)
		if err == nil && strings.TrimSpace(rendered) != "" {
			sb.WriteString(rendered)
		} else {
			sb.WriteString(desc)
			sb.WriteString("\n")
		}
	} else {
		sb.WriteString(desc)
		sb.WriteString("\n")
	}

	// ── Check runs ────────────────────────────────────────────────────────────
	sb.WriteString("\n── Check Runs ───────────────────────────────────────────────\n")
	if len(p.checkRuns) == 0 {
		sb.WriteString("  (no check runs)\n")
	} else {
		for _, cr := range p.checkRuns {
			sb.WriteString(fmt.Sprintf("  %s  %s  %s\n", checkRunStateSymbol(cr.State, cr.Conclusion), cr.Name, cr.Conclusion))
		}
	}

	// ── Review threads ────────────────────────────────────────────────────────
	open, resolved := 0, 0
	for _, t := range p.threads {
		if t.Resolved {
			resolved++
		} else {
			open++
		}
	}
	sb.WriteString(fmt.Sprintf("\n── Review Threads (%d open, %d resolved) ────────────────────\n", open, resolved))
	if len(p.openThreads) == 0 {
		sb.WriteString("  (no open threads)\n")
	} else {
		for i, t := range p.openThreads {
			prefix := "  "
			if i == p.currentThread {
				prefix = "> "
			}
			sb.WriteString(fmt.Sprintf("%s[thread %d] %s:%d  %s\n", prefix, i+1, t.Path, t.Line, truncate(t.Body, 60)))
		}
		sb.WriteString("  [n/N to navigate, r to resolve]\n")
	}

	// ── Active watches ────────────────────────────────────────────────────────
	sb.WriteString("\n── Watches ──────────────────────────────────────────────────\n")
	activeWatches := 0
	for _, w := range p.watches {
		if w.Status == "waiting" || w.Status == "scheduled" {
			activeWatches++
			sb.WriteString(fmt.Sprintf("  %s  %s → %s\n", watchStatusDisplay(w.Status), w.TriggerExpr, w.ActionExpr))
		}
	}
	if activeWatches == 0 {
		sb.WriteString("  (no active watches)\n")
	}

	// ── Timeline events ───────────────────────────────────────────────────────
	sb.WriteString("\n── Recent Timeline ──────────────────────────────────────────\n")
	if len(p.timeline) == 0 {
		sb.WriteString("  (no timeline events)\n")
	} else {
		for _, e := range p.timeline {
			sb.WriteString(fmt.Sprintf("  %s  @%s\n", e.EventType, e.Actor))
		}
	}

	return sb.String()
}

// nextThread advances the focused thread index (wraps around).
func (p *DetailPane) nextThread() tea.Cmd {
	if len(p.openThreads) == 0 {
		return nil
	}
	p.currentThread = (p.currentThread + 1) % len(p.openThreads)
	p.refreshViewport()
	return nil
}

// prevThread retreats the focused thread index (wraps around).
func (p *DetailPane) prevThread() tea.Cmd {
	if len(p.openThreads) == 0 {
		return nil
	}
	p.currentThread = (p.currentThread - 1 + len(p.openThreads)) % len(p.openThreads)
	p.refreshViewport()
	return nil
}

// resolveCurrentThread sends the resolveReviewThread mutation for the focused thread.
func (p *DetailPane) resolveCurrentThread() tea.Cmd {
	if len(p.openThreads) == 0 || p.resolver == nil {
		return nil
	}
	threadID := p.openThreads[p.currentThread].ID
	return func() tea.Msg {
		if err := p.resolver.ResolveReviewThread(context.Background(), threadID); err != nil {
			return nil
		}
		return ThreadResolvedMsg{ThreadID: threadID}
	}
}

// checkRunStateSymbol converts a check-run state+conclusion to a display symbol.
func checkRunStateSymbol(state, conclusion string) string {
	switch {
	case conclusion == "success":
		return "✓"
	case conclusion == "failure" || conclusion == "timed_out":
		return "✗"
	case state == "in_progress" || state == "queued":
		return "⟳"
	default:
		return "—"
	}
}

// truncate shortens s to max runes and appends "…" if needed.
func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "…"
}
