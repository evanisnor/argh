package ui

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/sahilm/fuzzy"
)

// cbMode identifies the current autocomplete mode of the command bar.
type cbMode int

const (
	cbModeCommand      cbMode = iota // completing a command name
	cbModePR                         // completing a PR reference
	cbModeCollaborator               // completing a @user for :request
)

const maxSuggestions = 8

// commandDef describes a command and its autocomplete behavior.
type commandDef struct {
	name              string
	signature         string
	needsPR           bool
	needsCollaborator bool
}

// commandList is the full set of argh commands shown in the command bar.
var commandList = []commandDef{
	{name: ":open", signature: ":open [#pr]", needsPR: true},
	{name: ":diff", signature: ":diff [#pr]", needsPR: true},
	{name: ":approve", signature: ":approve [#pr]", needsPR: true},
	{name: ":review", signature: ":review [#pr]", needsPR: true},
	{name: ":request", signature: ":request [#pr] @user...", needsPR: true, needsCollaborator: true},
	{name: ":ready", signature: ":ready [#pr]", needsPR: true},
	{name: ":draft", signature: ":draft [#pr]", needsPR: true},
	{name: ":merge", signature: ":merge [#pr]", needsPR: true},
	{name: ":watch", signature: ":watch [#pr] <trigger> <action> [@user...]", needsPR: true},
	{name: ":close", signature: ":close [#pr]", needsPR: true},
	{name: ":reopen", signature: ":reopen [#pr]", needsPR: true},
	{name: ":label", signature: ":label [#pr] [label]", needsPR: true},
	{name: ":comment", signature: ":comment [#pr]", needsPR: true},
	{name: ":dnd", signature: ":dnd [duration]", needsPR: false},
	{name: ":wake", signature: ":wake", needsPR: false},
	{name: ":reload", signature: ":reload", needsPR: false},
	{name: ":help", signature: ":help", needsPR: false},
	{name: ":quit", signature: ":quit", needsPR: false},
}

// PRRef is a reference to a pull request usable for command bar completion.
type PRRef struct {
	SessionID string
	Number    int
	Title     string
	Repo      string
	URL       string
}

// CommandDispatcher parses and executes command-bar input, returning a
// Bubble Tea command that carries the result back to the model.
type CommandDispatcher interface {
	Execute(cmd string, args []string) tea.Cmd
}

// CommandBar is the persistent input bar pinned to the bottom of the TUI.
// It supports fuzzy command autocomplete, PR reference completion, and
// collaborator completion for :request.
type CommandBar struct {
	input         textinput.Model
	focused       bool
	suggestions   []string
	suggCursor    int
	suggOffset    int // index of first visible suggestion
	history       []string
	histCursor    int    // -1 means not in history navigation
	savedInput    string // input saved when entering history navigation
	prRefs        []PRRef
	collaborators []string
	mode          cbMode
	hint          string
	executor      CommandDispatcher // nil = commands not yet wired
}

// SetExecutor wires a CommandDispatcher into the bar so that pressing Enter
// while the bar is focused dispatches the typed command.
func (c *CommandBar) SetExecutor(e CommandDispatcher) {
	c.executor = e
}

// NewCommandBar creates a new CommandBar.
func NewCommandBar() *CommandBar {
	ti := textinput.New()
	ti.Prompt = ""
	ti.Placeholder = "/ or : for commands · ? for help"
	ti.CharLimit = 256
	return &CommandBar{
		histCursor: -1,
		input:      ti,
	}
}

// SetPRRefs updates the list of PR references available for completion.
func (c *CommandBar) SetPRRefs(refs []PRRef) {
	c.prRefs = refs
}

// SetCollaborators updates the list of collaborator logins available for
// @-completion in :request commands.
func (c *CommandBar) SetCollaborators(collabs []string) {
	c.collaborators = collabs
}

// Value returns the current text in the input field.
func (c *CommandBar) Value() string {
	return c.input.Value()
}

// HasContent always returns true: the command bar is always present.
func (c *CommandBar) HasContent() bool {
	return true
}

// Update handles incoming Bubble Tea messages.
func (c *CommandBar) Update(msg tea.Msg) (SubModel, tea.Cmd) {
	switch m := msg.(type) {
	case FocusCommandBarMsg:
		c.focused = true
		focusCmd := c.input.Focus()
		c.histCursor = -1
		c.savedInput = ""
		c.refreshSuggestions()
		return c, focusCmd

	case BlurCommandBarMsg:
		c.focused = false
		c.input.Blur()
		c.input.SetValue("")
		c.suggestions = nil
		c.suggCursor = 0
		c.suggOffset = 0
		c.histCursor = -1
		c.savedInput = ""
		c.mode = cbModeCommand
		c.hint = ""
		return c, nil

	case ReviewSuggestionsMsg:
		// Pre-fill the input with the prefix and update suggestions so the user
		// can select reviewers from the ranked list via @-completion.
		c.collaborators = m.Suggestions
		c.input.SetValue(m.InputPrefix)
		c.input.CursorEnd()
		c.focused = true
		focusCmd := c.input.Focus()
		c.histCursor = -1
		c.savedInput = ""
		c.refreshSuggestions()
		return c, focusCmd

	case CollaboratorsUpdatedMsg:
		c.collaborators = m.Logins
		if c.focused {
			c.refreshSuggestions()
		}
		return c, nil

	case tea.KeyMsg:
		if !c.focused {
			return c, nil
		}
		return c.handleKey(m)
	}
	return c, nil
}

// handleKey processes key events when the command bar is focused.
func (c *CommandBar) handleKey(msg tea.KeyMsg) (SubModel, tea.Cmd) {
	slog.Debug("commandBar.handleKey", "key", msg.String(), "inputBefore", c.input.Value())
	switch msg.String() {
	case "tab":
		c.acceptTopSuggestion()
		return c, nil

	case "up":
		if len(c.suggestions) > 0 {
			if c.suggCursor > 0 {
				c.suggCursor--
				if c.suggCursor < c.suggOffset {
					c.suggOffset = c.suggCursor
				}
			}
		} else {
			c.historyBack()
		}
		return c, nil

	case "down":
		if len(c.suggestions) > 0 {
			if c.suggCursor < len(c.suggestions)-1 {
				c.suggCursor++
				if c.suggCursor >= c.suggOffset+maxSuggestions {
					c.suggOffset = c.suggCursor - maxSuggestions + 1
				}
			}
		} else {
			c.historyForward()
		}
		return c, nil

	case "esc":
		// Root model handles blur via BlurCommandBarMsg; no local action needed.
		return c, nil

	case "enter":
		if len(c.suggestions) > 0 {
			c.acceptTopSuggestion()
			return c, nil
		}
		val := c.input.Value()
		c.commitToHistory()
		c.input.SetValue("")
		c.refreshSuggestions()
		if c.executor != nil && val != "" {
			cmd, args := ParseCommand(val)
			return c, c.executor.Execute(cmd, args)
		}
		return c, nil
	}

	// Forward all other keys to the textinput and refresh suggestions.
	var cmd tea.Cmd
	c.input, cmd = c.input.Update(msg)
	slog.Debug("commandBar.handleKey: textinput updated", "inputAfter", c.input.Value())
	c.histCursor = -1
	c.refreshSuggestions()
	return c, cmd
}

// acceptTopSuggestion replaces the current input with the top suggestion.
func (c *CommandBar) acceptTopSuggestion() {
	if len(c.suggestions) == 0 {
		return
	}
	top := c.suggestions[c.suggCursor]
	switch c.mode {
	case cbModeCommand:
		c.input.SetValue(top + " ")
	case cbModePR:
		parts := strings.SplitN(c.input.Value(), " ", 2)
		c.input.SetValue(parts[0] + " " + top + " ")
	case cbModeCollaborator:
		val := c.input.Value()
		atIdx := strings.LastIndex(val, "@")
		if atIdx >= 0 {
			c.input.SetValue(val[:atIdx] + "@" + top + " ")
		}
	}
	c.input.CursorEnd()
	c.refreshSuggestions()
}

// commitToHistory adds the current input to history (if non-empty) and
// resets the history cursor.
func (c *CommandBar) commitToHistory() {
	val := c.input.Value()
	if val != "" {
		c.history = append(c.history, val)
	}
	c.histCursor = -1
	c.savedInput = ""
}

// historyBack navigates one step back through command history.
func (c *CommandBar) historyBack() {
	if len(c.history) == 0 {
		return
	}
	if c.histCursor == -1 {
		c.savedInput = c.input.Value()
		c.histCursor = len(c.history) - 1
	} else if c.histCursor > 0 {
		c.histCursor--
	}
	c.input.SetValue(c.history[c.histCursor])
}

// historyForward navigates one step forward through command history.
func (c *CommandBar) historyForward() {
	if c.histCursor == -1 {
		return
	}
	if c.histCursor < len(c.history)-1 {
		c.histCursor++
		c.input.SetValue(c.history[c.histCursor])
	} else {
		c.histCursor = -1
		c.input.SetValue(c.savedInput)
	}
}

// refreshSuggestions recomputes suggestions and hint from the current input.
func (c *CommandBar) refreshSuggestions() {
	c.mode, c.hint, c.suggestions = computeSuggestions(c.input.Value(), c.prRefs, c.collaborators)
	if c.suggCursor >= len(c.suggestions) {
		c.suggCursor = 0
		c.suggOffset = 0
	}
	if maxOff := len(c.suggestions) - maxSuggestions; c.suggOffset > 0 && maxOff >= 0 && c.suggOffset > maxOff {
		c.suggOffset = maxOff
	}
}

// computeSuggestions is a pure function that returns the autocomplete mode,
// help hint, and suggestion list for the given input value.
func computeSuggestions(val string, prRefs []PRRef, collaborators []string) (cbMode, string, []string) {
	spaceIdx := strings.Index(val, " ")
	if spaceIdx == -1 {
		// Still typing the command — fuzzy match against the command name list.
		names := allCommandNames()
		hint := commandSignatureHint(val)
		return cbModeCommand, hint, fuzzyFilterStrings(val, names)
	}

	cmdPart := val[:spaceIdx]
	argPart := strings.TrimSpace(val[spaceIdx+1:])

	def := findCommandDef(cmdPart)
	if def == nil {
		return cbModeCommand, "", nil
	}

	// Check for collaborator mode: :request after a PR arg with "@" present.
	if def.needsCollaborator {
		atIdx := strings.LastIndex(argPart, "@")
		if atIdx >= 0 {
			prefix := argPart[atIdx+1:]
			return cbModeCollaborator, def.signature, fuzzyFilterStrings(prefix, collaborators)
		}
	}

	// Check for collaborator mode in :watch when the action token is "review".
	if cmdPart == ":watch" {
		atIdx := strings.LastIndex(argPart, "@")
		if atIdx >= 0 {
			before := strings.TrimSpace(argPart[:atIdx])
			for _, f := range strings.Fields(before) {
				if f == "review" {
					prefix := argPart[atIdx+1:]
					return cbModeCollaborator, def.signature, fuzzyFilterStrings(prefix, collaborators)
				}
			}
		}
	}

	// PR completion mode.
	if def.needsPR {
		return cbModePR, def.signature, prMatches(argPart, prRefs)
	}

	return cbModeCommand, def.signature, nil
}

// allCommandNames returns just the name strings from commandList.
func allCommandNames() []string {
	names := make([]string, len(commandList))
	for i, c := range commandList {
		names[i] = c.name
	}
	return names
}

// commandSignatureHint returns the signature of the best fuzzy-matching
// command, or an empty string if there is no match or partial is empty.
func commandSignatureHint(partial string) string {
	if partial == "" {
		return ""
	}
	matches := fuzzy.Find(partial, allCommandNames())
	if len(matches) == 0 {
		return ""
	}
	return commandList[matches[0].Index].signature
}

// findCommandDef returns a pointer to the commandDef whose name matches
// cmdPart exactly, or nil if not found.
func findCommandDef(cmdPart string) *commandDef {
	for i := range commandList {
		if commandList[i].name == cmdPart {
			return &commandList[i]
		}
	}
	return nil
}

// prMatches returns PR reference strings that match fragment.
// It recognises: #number, exact session ID, and fuzzy title fragment.
// When fragment is empty, all refs are returned.
func prMatches(fragment string, refs []PRRef) []string {
	if len(refs) == 0 {
		return nil
	}
	if fragment == "" {
		out := make([]string, len(refs))
		for i, ref := range refs {
			if ref.SessionID != "" {
				out[i] = ref.SessionID
			} else {
				out[i] = fmt.Sprintf("#%d", ref.Number)
			}
		}
		return out
	}

	// #number — match by PR number.
	if strings.HasPrefix(fragment, "#") {
		numStr := fragment[1:]
		num, err := strconv.Atoi(numStr)
		if err == nil {
			for _, ref := range refs {
				if ref.Number == num {
					return []string{fmt.Sprintf("#%d", ref.Number)}
				}
			}
		}
		// Partial #-prefix or no exact match — list all as #number suggestions.
		out := make([]string, len(refs))
		for i, ref := range refs {
			out[i] = fmt.Sprintf("#%d", ref.Number)
		}
		return out
	}

	// Exact session ID match.
	for _, ref := range refs {
		if ref.SessionID == fragment {
			return []string{ref.SessionID}
		}
	}

	// Fuzzy title match.
	titles := make([]string, len(refs))
	for i, ref := range refs {
		titles[i] = ref.Title
	}
	results := fuzzy.Find(fragment, titles)
	if len(results) == 0 {
		return nil
	}
	out := make([]string, len(results))
	for i, r := range results {
		ref := refs[r.Index]
		if ref.SessionID != "" {
			out[i] = ref.SessionID
		} else {
			out[i] = fmt.Sprintf("#%d", ref.Number)
		}
	}
	return out
}

// fuzzyFilterStrings runs fuzzy matching and returns the matched strings.
// When pattern is empty, all items are returned.
func fuzzyFilterStrings(pattern string, items []string) []string {
	if pattern == "" {
		out := make([]string, len(items))
		copy(out, items)
		return out
	}
	matches := fuzzy.Find(pattern, items)
	out := make([]string, len(matches))
	for i, m := range matches {
		out[i] = items[m.Index]
	}
	return out
}

// HasSuggestions reports whether the command bar currently has autocomplete
// suggestions to display.
func (c *CommandBar) HasSuggestions() bool {
	return len(c.suggestions) > 0
}

// SuggestionsView renders the autocomplete suggestion list as a standalone
// string. Returns an empty string when there are no suggestions. The caller
// is responsible for applying any width or background styling.
func (c *CommandBar) SuggestionsView() string {
	if len(c.suggestions) == 0 {
		return ""
	}
	end := c.suggOffset + maxSuggestions
	if end > len(c.suggestions) {
		end = len(c.suggestions)
	}
	var sb strings.Builder
	for i := c.suggOffset; i < end; i++ {
		if i > c.suggOffset {
			sb.WriteString("\n")
		}
		if i == c.suggCursor {
			sb.WriteString("> ")
		} else {
			sb.WriteString("  ")
		}
		sb.WriteString(c.suggestions[i])
	}
	return sb.String()
}

// View renders the command bar input line and optional hint. Suggestion lines
// are rendered separately via SuggestionsView and overlaid by the root model.
func (c *CommandBar) View() string {
	if !c.focused {
		return "/ or : for commands · ? for help"
	}

	var sb strings.Builder
	sb.WriteString(c.input.View())
	if c.hint != "" {
		sb.WriteString("  ")
		sb.WriteString(c.hint)
	}

	return sb.String()
}
