package ui

import (
	"context"
	"fmt"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/evanisnor/argh/internal/api"
	"github.com/evanisnor/argh/internal/config"
)

// setupScreen identifies which screen the setup flow is on.
type setupScreen int

const (
	screenMethodSelect setupScreen = iota
	screenDeviceFlow
	screenPATEntry
	screenSSO
	screenGHCLI
)

// SetupDeps groups the injectable boundaries for RunSetup.
type SetupDeps struct {
	Verify      func(ctx context.Context, token string) (string, error)
	RunProgram  func(m tea.Model) (tea.Model, error)
	DeviceFlow  api.DeviceFlowClient
	OpenBrowser func(url string) error
	ClientID    string
	Scopes      []string
	GHCLIVerify func(ctx context.Context) (string, error)
}

// SetupResult holds the outcome of a completed setup flow.
type SetupResult struct {
	Token     string
	TokenType config.TokenType
	Quit      bool
}

// RunSetup runs the setup modal as a standalone Bubble Tea program.
func RunSetup(ctx context.Context, deps SetupDeps) (SetupResult, error) {
	m := newSetupModel(ctx, deps)
	final, err := deps.RunProgram(m)
	if err != nil {
		return SetupResult{}, fmt.Errorf("running setup program: %w", err)
	}
	result := final.(SetupModel)
	if result.quit {
		return SetupResult{Quit: true}, nil
	}
	return SetupResult{
		Token:     result.token,
		TokenType: result.tokenType,
	}, nil
}

// verifyResultMsg carries the result of a background token verification.
type verifyResultMsg struct {
	login string
	err   error
}

// deviceCodeMsg carries the result of requesting a device code.
type deviceCodeMsg struct {
	resp *api.DeviceCodeResponse
	err  error
}

// deviceTokenMsg carries the result of polling for a device flow token.
type deviceTokenMsg struct {
	resp *api.TokenResponse
	err  error
}

// ghcliVerifyMsg carries the result of verifying gh CLI authentication.
type ghcliVerifyMsg struct {
	login string
	err   error
}

// SetupModel is the Bubble Tea model for the multi-screen setup flow.
type SetupModel struct {
	screen    setupScreen
	input     textinput.Model
	verifying bool
	errMsg    string
	token     string
	tokenType config.TokenType
	done      bool
	quit      bool
	width     int
	height    int
	theme     Theme

	// PAT verification
	verify func(ctx context.Context, token string) (string, error)
	ctx    context.Context

	// Device flow
	deviceFlow  api.DeviceFlowClient
	openBrowser func(url string) error
	clientID    string
	scopes      []string
	userCode    string
	verifyURI   string
	polling     bool
	ssoURL      string

	// gh CLI verification
	ghcliVerify func(ctx context.Context) (string, error)
}

func newSetupModel(ctx context.Context, deps SetupDeps) SetupModel {
	ti := textinput.New()
	ti.Placeholder = "ghp_..."
	ti.EchoMode = textinput.EchoPassword
	ti.EchoCharacter = '•'
	ti.CharLimit = 256

	return SetupModel{
		screen:      screenMethodSelect,
		input:       ti,
		theme:       newTheme(lipgloss.HasDarkBackground()),
		verify:      deps.Verify,
		ctx:         ctx,
		deviceFlow:  deps.DeviceFlow,
		openBrowser: deps.OpenBrowser,
		clientID:    deps.ClientID,
		scopes:      deps.Scopes,
		ghcliVerify: deps.GHCLIVerify,
	}
}

// Init returns nil — no initial command needed for the method selection screen.
func (m SetupModel) Init() tea.Cmd {
	return nil
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
		return m.handleVerifyResult(msg)

	case deviceCodeMsg:
		return m.handleDeviceCode(msg)

	case deviceTokenMsg:
		return m.handleDeviceToken(msg)

	case ghcliVerifyMsg:
		return m.handleGHCLIVerify(msg)
	}

	if m.screen == screenPATEntry {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m SetupModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "esc":
		if m.screen == screenSSO {
			m.done = true
			return m, tea.Quit
		}
		m.quit = true
		return m, tea.Quit

	case "q":
		return m.handleQ(msg)

	case "o":
		if m.screen == screenSSO {
			if m.openBrowser != nil {
				_ = m.openBrowser(m.ssoURL)
			}
			m.done = true
			return m, tea.Quit
		}
		return m.handleDefault(msg)

	case "enter":
		if m.screen == screenSSO {
			if m.openBrowser != nil {
				_ = m.openBrowser(m.ssoURL)
			}
			m.done = true
			return m, tea.Quit
		}
		return m.handleDefault(msg)

	case "g":
		if m.screen == screenMethodSelect {
			return m.startDeviceFlow()
		}
		return m.handleDefault(msg)

	case "c":
		if m.screen == screenMethodSelect && m.ghcliVerify != nil {
			return m.startGHCLIVerify()
		}
		if m.screen == screenGHCLI && !m.verifying {
			return m.startGHCLIVerify()
		}
		return m.handleDefault(msg)

	case "p":
		if m.screen == screenMethodSelect {
			m.screen = screenPATEntry
			m.input.Focus()
			return m, textinput.Blink
		}
		return m.handleDefault(msg)

	case "s":
		return m.handleS(msg)

	default:
		return m.handleDefault(msg)
	}
}

func (m SetupModel) handleQ(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.screen == screenMethodSelect {
		m.quit = true
		return m, tea.Quit
	}
	if m.screen == screenDeviceFlow {
		m.quit = true
		return m, tea.Quit
	}
	if m.screen == screenSSO {
		m.done = true
		return m, tea.Quit
	}
	if m.screen == screenGHCLI {
		m.quit = true
		return m, tea.Quit
	}
	// PAT entry screen
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
}

func (m SetupModel) handleS(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.screen == screenSSO {
		m.done = true
		return m, tea.Quit
	}
	if m.screen != screenPATEntry {
		return m.handleDefault(msg)
	}
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
}

func (m SetupModel) handleDefault(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.screen == screenPATEntry && !m.verifying {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m SetupModel) handleVerifyResult(msg verifyResultMsg) (tea.Model, tea.Cmd) {
	m.verifying = false
	if msg.err != nil {
		m.errMsg = msg.err.Error()
		m.input.Focus()
		return m, nil
	}
	m.token = m.input.Value()
	m.tokenType = config.TokenTypePAT
	m.done = true
	return m, tea.Quit
}

func (m SetupModel) startDeviceFlow() (tea.Model, tea.Cmd) {
	m.screen = screenDeviceFlow
	m.errMsg = ""
	df := m.deviceFlow
	clientID := m.clientID
	scopes := m.scopes
	ctx := m.ctx
	return m, func() tea.Msg {
		resp, err := df.RequestCode(ctx, clientID, scopes)
		return deviceCodeMsg{resp: resp, err: err}
	}
}

func (m SetupModel) handleDeviceCode(msg deviceCodeMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		m.errMsg = msg.err.Error()
		return m, nil
	}
	m.userCode = msg.resp.UserCode
	m.verifyURI = msg.resp.VerificationURI
	m.polling = true

	// Try to open the browser (ignore errors — user can open manually).
	if m.openBrowser != nil {
		_ = m.openBrowser(msg.resp.VerificationURI)
	}

	df := m.deviceFlow
	clientID := m.clientID
	deviceCode := msg.resp.DeviceCode
	interval := msg.resp.Interval
	ctx := m.ctx
	return m, func() tea.Msg {
		resp, err := df.PollToken(ctx, clientID, deviceCode, interval)
		return deviceTokenMsg{resp: resp, err: err}
	}
}

func (m SetupModel) handleDeviceToken(msg deviceTokenMsg) (tea.Model, tea.Cmd) {
	m.polling = false
	if msg.err != nil {
		m.errMsg = msg.err.Error()
		return m, nil
	}
	m.token = msg.resp.AccessToken
	m.tokenType = config.TokenTypeOAuth
	m.screen = screenSSO
	m.ssoURL = fmt.Sprintf("https://github.com/settings/connections/applications/%s", m.clientID)
	return m, nil
}

func (m SetupModel) startGHCLIVerify() (tea.Model, tea.Cmd) {
	m.screen = screenGHCLI
	m.verifying = true
	m.errMsg = ""
	verify := m.ghcliVerify
	ctx := m.ctx
	return m, func() tea.Msg {
		login, err := verify(ctx)
		return ghcliVerifyMsg{login: login, err: err}
	}
}

func (m SetupModel) handleGHCLIVerify(msg ghcliVerifyMsg) (tea.Model, tea.Cmd) {
	m.verifying = false
	if msg.err != nil {
		m.errMsg = msg.err.Error()
		return m, nil
	}
	m.token = "ghcli"
	m.tokenType = config.TokenTypeGHCLI
	m.done = true
	return m, tea.Quit
}

// View renders the current setup screen.
func (m SetupModel) View() string {
	var content string
	switch m.screen {
	case screenMethodSelect:
		content = m.viewMethodSelect()
	case screenDeviceFlow:
		content = m.viewDeviceFlow()
	case screenPATEntry:
		content = m.viewPATEntry()
	case screenSSO:
		content = m.viewSSO()
	case screenGHCLI:
		content = m.viewGHCLI()
	}

	if m.width > 0 && m.height > 0 {
		return lipgloss.Place(m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			content)
	}
	return content
}

func (m SetupModel) viewMethodSelect() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#C0C0FF"))
	title := titleStyle.Render("argh - Authentication Setup")
	options := "  [g] Login with GitHub (recommended)\n  [p] Enter Personal Access Token"
	if m.ghcliVerify != nil {
		options += "\n  [c] Use gh CLI (for SSO organizations)"
	}
	options += "\n  [q] Quit"
	return title + "\n\n" + options
}

func (m SetupModel) viewDeviceFlow() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#C0C0FF"))
	errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#FF6B6B"))

	title := titleStyle.Render("argh - Login with GitHub")

	if m.errMsg != "" {
		return title + "\n\n" + errorStyle.Render("error: "+m.errMsg) + "\n\n[q]uit"
	}

	if m.userCode == "" {
		return title + "\n\nRequesting device code..."
	}

	code := fmt.Sprintf("Open this URL in your browser:\n  %s\n\nEnter this code:  %s", m.verifyURI, m.userCode)
	status := "Waiting for authorization..."
	return title + "\n\n" + code + "\n\n" + status + "  [q]uit"
}

func (m SetupModel) viewSSO() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#C0C0FF"))
	title := titleStyle.Render("argh - SSO Authorization")

	desc := "If your GitHub organization uses SAML SSO, you need to\nauthorize this app for each SSO-protected org."
	url := fmt.Sprintf("  %s", m.ssoURL)
	controls := "[o]pen in browser  [s]kip"

	return title + "\n\n" + desc + "\n\n" + url + "\n\n" + controls
}

func (m SetupModel) viewPATEntry() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#C0C0FF"))
	faintStyle := lipgloss.NewStyle().Faint(true)
	errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#FF6B6B"))

	title := titleStyle.Render("argh - GitHub PAT Setup")
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
	return content
}

func (m SetupModel) viewGHCLI() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#C0C0FF"))
	errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#FF6B6B"))

	title := titleStyle.Render("argh - gh CLI Setup")

	if m.verifying {
		return title + "\n\nVerifying gh CLI authentication..."
	}

	if m.errMsg != "" {
		hint := "Run `gh auth login` to authenticate, then try again."
		return title + "\n\n" + errorStyle.Render("error: "+m.errMsg) + "\n\n" + hint + "\n\n[c] retry  [q]uit"
	}

	return title + "\n\nVerifying gh CLI authentication..."
}
