package ui

import (
	"context"
	"fmt"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// SetupDeps groups the injectable boundaries for RunSetup.
type SetupDeps struct {
	Verify     func(ctx context.Context, token string) (string, error)
	RunProgram func(m tea.Model) (tea.Model, error)
}

// RunSetup runs the PAT setup modal as a standalone Bubble Tea program.
// Returns the entered token on success, or quit=true if the user cancelled.
func RunSetup(ctx context.Context, deps SetupDeps) (token string, quit bool, err error) {
	m := newSetupModel(ctx, deps.Verify)
	final, err := deps.RunProgram(m)
	if err != nil {
		return "", false, fmt.Errorf("running setup program: %w", err)
	}
	result := final.(SetupModel)
	if result.quit {
		return "", true, nil
	}
	return result.token, false, nil
}

// verifyResultMsg carries the result of a background token verification.
type verifyResultMsg struct {
	login string
	err   error
}

// SetupModel is the Bubble Tea model for the PAT setup screen.
type SetupModel struct {
	input     textinput.Model
	verifying bool
	errMsg    string
	token     string
	done      bool
	quit      bool
	width     int
	height    int
	theme     Theme
	verify    func(ctx context.Context, token string) (string, error)
	ctx       context.Context
}

func newSetupModel(ctx context.Context, verify func(ctx context.Context, token string) (string, error)) SetupModel {
	ti := textinput.New()
	ti.Placeholder = "ghp_..."
	ti.EchoMode = textinput.EchoPassword
	ti.EchoCharacter = '•'
	ti.Focus()
	ti.CharLimit = 256

	return SetupModel{
		input:  ti,
		theme:  newTheme(lipgloss.HasDarkBackground()),
		verify: verify,
		ctx:    ctx,
	}
}

// Init starts the text input cursor blink.
func (m SetupModel) Init() tea.Cmd {
	return textinput.Blink
}

// Update handles messages for the setup model.
func (m SetupModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case verifyResultMsg:
		m.verifying = false
		if msg.err != nil {
			m.errMsg = msg.err.Error()
			m.input.Focus()
			return m, nil
		}
		m.token = m.input.Value()
		m.done = true
		return m, tea.Quit
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m SetupModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "esc":
		m.quit = true
		return m, tea.Quit

	case "q":
		if m.verifying {
			return m, nil
		}
		if m.input.Value() == "" {
			m.quit = true
			return m, tea.Quit
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd

	case "s":
		if m.verifying {
			return m, nil
		}
		if m.input.Value() != "" {
			m.verifying = true
			m.errMsg = ""
			m.input.Blur()
			token := m.input.Value()
			verify := m.verify
			ctx := m.ctx
			return m, func() tea.Msg {
				login, err := verify(ctx, token)
				return verifyResultMsg{login: login, err: err}
			}
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd

	default:
		if m.verifying {
			return m, nil
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
}

// View renders the setup screen.
func (m SetupModel) View() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#C0C0FF"))
	faintStyle := lipgloss.NewStyle().Faint(true)
	errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#FF6B6B"))

	title := titleStyle.Render("argh — GitHub PAT Setup")
	desc := "Enter a GitHub Personal Access Token with repo scope."
	inputLine := m.input.View()

	saveLabel := "[s]ave"
	if m.input.Value() == "" {
		saveLabel = faintStyle.Render("[s]ave")
	}
	controls := "[q]uit  " + saveLabel

	status := ""
	if m.verifying {
		status = "verifying..."
	}
	if m.errMsg != "" {
		status = errorStyle.Render("error: " + m.errMsg)
	}

	content := title + "\n\n" + desc + "\n\n" + inputLine + "\n\n" + controls
	if status != "" {
		content += "\n\n" + status
	}

	if m.width > 0 && m.height > 0 {
		return lipgloss.Place(m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			content)
	}
	return content
}
