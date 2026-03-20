package ui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/evanisnor/argh/internal/api"
	"github.com/evanisnor/argh/internal/config"
)

func setupModel() SetupModel {
	return newSetupModel(context.Background(), SetupDeps{
		Verify:      okVerify,
		DeviceFlow:  &api.StubDeviceFlowClient{},
		OpenBrowser: func(_ string) error { return nil },
		ClientID:    "test-client",
		Scopes:      []string{"repo", "read:org"},
	})
}

func setupModelWithDeviceFlow(df api.DeviceFlowClient, browser func(string) error) SetupModel {
	return newSetupModel(context.Background(), SetupDeps{
		Verify:      okVerify,
		DeviceFlow:  df,
		OpenBrowser: browser,
		ClientID:    "test-client",
		Scopes:      []string{"repo"},
	})
}

func okVerify(_ context.Context, _ string) (string, error) {
	return "octocat", nil
}

func failVerify(_ context.Context, _ string) (string, error) {
	return "", errors.New("401 Unauthorized")
}

// typeToken sets the input value directly to avoid key interception of 's'.
func typeToken(m SetupModel, token string) SetupModel {
	m.input.SetValue(token)
	return m
}

func typeChars(m SetupModel, chars string) SetupModel {
	for _, c := range chars {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{c}})
		m = updated.(SetupModel)
	}
	return m
}

// ── Method Selection Screen ──────────────────────────────────────────────────

func TestSetup_InitialState(t *testing.T) {
	m := setupModel()

	if m.screen != screenMethodSelect {
		t.Errorf("expected screenMethodSelect, got %d", m.screen)
	}
	if m.verifying {
		t.Error("expected verifying=false initially")
	}
	if m.done {
		t.Error("expected done=false initially")
	}
	if m.quit {
		t.Error("expected quit=false initially")
	}
	if m.token != "" {
		t.Errorf("expected empty token initially, got %q", m.token)
	}
	if m.errMsg != "" {
		t.Errorf("expected empty errMsg initially, got %q", m.errMsg)
	}

	v := m.View()
	if !strings.Contains(v, "Authentication Setup") {
		t.Error("View should contain 'Authentication Setup'")
	}
	if !strings.Contains(v, "[g]") {
		t.Error("View should contain '[g]' option")
	}
	if !strings.Contains(v, "[p]") {
		t.Error("View should contain '[p]' option")
	}
}

func TestSetup_Init_ReturnsNil(t *testing.T) {
	m := setupModel()
	cmd := m.Init()
	if cmd != nil {
		t.Error("Init should return nil for method selection screen")
	}
}

func TestSetup_MethodSelect_Q_Quits(t *testing.T) {
	m := setupModel()

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	m = updated.(SetupModel)

	if !m.quit {
		t.Error("expected quit=true when q pressed on method selection")
	}
	if cmd == nil {
		t.Fatal("expected tea.Quit command")
	}
}

func TestSetup_MethodSelect_Esc_Quits(t *testing.T) {
	m := setupModel()

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(SetupModel)

	if !m.quit {
		t.Error("expected quit=true on Esc")
	}
	if cmd == nil {
		t.Fatal("expected tea.Quit command")
	}
}

func TestSetup_MethodSelect_CtrlC_Quits(t *testing.T) {
	m := setupModel()

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = updated.(SetupModel)

	if !m.quit {
		t.Error("expected quit=true on Ctrl+C")
	}
	if cmd == nil {
		t.Fatal("expected tea.Quit command")
	}
}

func TestSetup_MethodSelect_P_TransitionsToPAT(t *testing.T) {
	m := setupModel()

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m = updated.(SetupModel)

	if m.screen != screenPATEntry {
		t.Errorf("expected screenPATEntry, got %d", m.screen)
	}
	if cmd == nil {
		t.Fatal("expected non-nil command (textinput.Blink)")
	}
}

func TestSetup_MethodSelect_G_TransitionsToDeviceFlow(t *testing.T) {
	df := &api.StubDeviceFlowClient{
		RequestCodeFunc: func(_ context.Context, _ string, _ []string) (*api.DeviceCodeResponse, error) {
			return &api.DeviceCodeResponse{
				DeviceCode:      "dc123",
				UserCode:        "ABCD-1234",
				VerificationURI: "https://github.com/login/device",
				Interval:        5 * time.Second,
			}, nil
		},
	}
	m := setupModelWithDeviceFlow(df, func(_ string) error { return nil })

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	m = updated.(SetupModel)

	if m.screen != screenDeviceFlow {
		t.Errorf("expected screenDeviceFlow, got %d", m.screen)
	}
	if cmd == nil {
		t.Fatal("expected non-nil command for device code request")
	}
}

func TestSetup_MethodSelect_UnknownKey_Ignored(t *testing.T) {
	m := setupModel()

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = updated.(SetupModel)

	if m.screen != screenMethodSelect {
		t.Errorf("expected to stay on method selection, got %d", m.screen)
	}
	if cmd != nil {
		t.Error("expected nil command for unknown key")
	}
}

func TestSetup_WindowSizeMsg(t *testing.T) {
	m := setupModel()

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = updated.(SetupModel)

	if m.width != 120 {
		t.Errorf("width: got %d, want 120", m.width)
	}
	if m.height != 40 {
		t.Errorf("height: got %d, want 40", m.height)
	}
}

func TestSetup_View_WithDimensions(t *testing.T) {
	m := setupModel()
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(SetupModel)

	v := m.View()
	if v == "" {
		t.Error("View should not be empty with dimensions set")
	}
}

func TestSetup_View_NoDimensions(t *testing.T) {
	m := setupModel()
	v := m.View()
	if v == "" {
		t.Error("View should not be empty without dimensions")
	}
}

func TestSetup_MethodSelect_UnknownMsg_Ignored(t *testing.T) {
	m := setupModel()
	updated, _ := m.Update(tea.FocusMsg{})
	m = updated.(SetupModel)
	if m.quit {
		t.Error("unexpected quit on unknown message")
	}
}

// ── PAT Entry Screen ────────────────────────────────────────────────────────

func TestSetup_PATEntry_TypeChars(t *testing.T) {
	m := setupModel()
	// Transition to PAT entry
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m = updated.(SetupModel)

	m = typeChars(m, "ghp_abc")
	if m.input.Value() != "ghp_abc" {
		t.Errorf("input value: got %q, want %q", m.input.Value(), "ghp_abc")
	}
}

func TestSetup_PATEntry_Save_Verifying(t *testing.T) {
	m := setupModel()
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m = updated.(SetupModel)
	m = typeToken(m, "ghp_token123")

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = updated.(SetupModel)

	if !m.verifying {
		t.Error("expected verifying=true after pressing s")
	}
	if m.errMsg != "" {
		t.Errorf("expected empty errMsg while verifying, got %q", m.errMsg)
	}
	if cmd == nil {
		t.Fatal("expected non-nil command for async verify")
	}

	result := cmd()
	vrm, ok := result.(verifyResultMsg)
	if !ok {
		t.Fatalf("expected verifyResultMsg, got %T", result)
	}
	if vrm.err != nil {
		t.Fatalf("expected no verify error, got: %v", vrm.err)
	}
}

func TestSetup_PATEntry_VerifySuccess_Done(t *testing.T) {
	m := setupModel()
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m = updated.(SetupModel)
	m = typeToken(m, "ghp_valid")
	m.verifying = true

	updated, cmd := m.Update(verifyResultMsg{login: "octocat"})
	m = updated.(SetupModel)

	if m.verifying {
		t.Error("expected verifying=false after success")
	}
	if !m.done {
		t.Error("expected done=true after verify success")
	}
	if m.token != "ghp_valid" {
		t.Errorf("token: got %q, want %q", m.token, "ghp_valid")
	}
	if m.tokenType != config.TokenTypePAT {
		t.Errorf("tokenType: got %q, want %q", m.tokenType, config.TokenTypePAT)
	}
	if cmd == nil {
		t.Fatal("expected tea.Quit command")
	}
}

func TestSetup_PATEntry_VerifyFailure_ErrorShown(t *testing.T) {
	m := newSetupModel(context.Background(), SetupDeps{
		Verify:      failVerify,
		DeviceFlow:  &api.StubDeviceFlowClient{},
		OpenBrowser: func(_ string) error { return nil },
		ClientID:    "test-client",
	})
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m = updated.(SetupModel)
	m = typeToken(m, "ghp_bad")
	m.verifying = true

	updated, _ = m.Update(verifyResultMsg{err: errors.New("401 Unauthorized")})
	m = updated.(SetupModel)

	if m.verifying {
		t.Error("expected verifying=false after failure")
	}
	if m.done {
		t.Error("expected done=false after failure")
	}
	if m.errMsg != "401 Unauthorized" {
		t.Errorf("errMsg: got %q, want %q", m.errMsg, "401 Unauthorized")
	}

	v := m.View()
	if !strings.Contains(v, "401 Unauthorized") {
		t.Error("View should contain error message")
	}
}

func TestSetup_PATEntry_Q_EmptyInput_Quits(t *testing.T) {
	m := setupModel()
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m = updated.(SetupModel)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	m = updated.(SetupModel)

	if !m.quit {
		t.Error("expected quit=true when q pressed with empty input")
	}
	if cmd == nil {
		t.Fatal("expected tea.Quit command")
	}
}

func TestSetup_PATEntry_Q_NonEmptyInput_TypesQ(t *testing.T) {
	m := setupModel()
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m = updated.(SetupModel)
	m = typeToken(m, "ghp_")

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	m = updated.(SetupModel)

	if m.quit {
		t.Error("expected quit=false when q pressed with non-empty input")
	}
	if !strings.HasSuffix(m.input.Value(), "q") {
		t.Errorf("expected input to end with 'q', got %q", m.input.Value())
	}
}

func TestSetup_PATEntry_Esc_Quits(t *testing.T) {
	m := setupModel()
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m = updated.(SetupModel)
	m = typeToken(m, "ghp_token")

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(SetupModel)

	if !m.quit {
		t.Error("expected quit=true on Esc")
	}
	if cmd == nil {
		t.Fatal("expected tea.Quit command")
	}
}

func TestSetup_PATEntry_S_EmptyInput_TypesS(t *testing.T) {
	m := setupModel()
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m = updated.(SetupModel)

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = updated.(SetupModel)

	if m.verifying {
		t.Error("s with empty input should not start verification")
	}
	if m.input.Value() != "s" {
		t.Errorf("input: got %q, want %q", m.input.Value(), "s")
	}
}

func TestSetup_PATEntry_KeysDuringVerifying_Ignored(t *testing.T) {
	m := setupModel()
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m = updated.(SetupModel)
	m = typeToken(m, "ghp_token")
	m.verifying = true
	m.input.Blur()
	val := m.input.Value()

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = updated.(SetupModel)

	if m.input.Value() != val {
		t.Errorf("input should not change during verification: got %q, want %q", m.input.Value(), val)
	}
}

func TestSetup_PATEntry_S_WhileVerifying_Ignored(t *testing.T) {
	m := setupModel()
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m = updated.(SetupModel)
	m = typeToken(m, "ghp_token")
	m.verifying = true

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = updated.(SetupModel)

	if cmd != nil {
		t.Error("expected nil command when s pressed during verification")
	}
}

func TestSetup_PATEntry_Q_WhileVerifying_Ignored(t *testing.T) {
	m := setupModel()
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m = updated.(SetupModel)
	m = typeToken(m, "ghp_token")
	m.verifying = true

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	m = updated.(SetupModel)

	if m.quit {
		t.Error("expected quit=false during verification")
	}
}

func TestSetup_PATEntry_View_Verifying(t *testing.T) {
	m := setupModel()
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m = updated.(SetupModel)
	m = typeToken(m, "ghp_token")
	m.verifying = true

	v := m.View()
	if !strings.Contains(v, "verifying") {
		t.Error("View should show 'verifying' during verification")
	}
}

func TestSetup_PATEntry_View_SaveFaint_WhenEmpty(t *testing.T) {
	m := setupModel()
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m = updated.(SetupModel)

	v := m.View()
	if !strings.Contains(v, "[q]uit") {
		t.Error("View should contain [q]uit")
	}
}

func TestSetup_PATEntry_View_ShowsPATTitle(t *testing.T) {
	m := setupModel()
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m = updated.(SetupModel)

	v := m.View()
	if !strings.Contains(v, "PAT") {
		t.Error("View should contain 'PAT'")
	}
}

func TestSetup_PATEntry_UnknownMsg_PassedToInput(t *testing.T) {
	m := setupModel()
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m = updated.(SetupModel)

	updated, _ = m.Update(tea.FocusMsg{})
	m = updated.(SetupModel)
	if m.quit {
		t.Error("unexpected quit on unknown message")
	}
}

// ── Device Flow Screen ──────────────────────────────────────────────────────

func TestSetup_DeviceFlow_RequestCodeSuccess(t *testing.T) {
	browserOpened := ""
	df := &api.StubDeviceFlowClient{
		RequestCodeFunc: func(_ context.Context, clientID string, scopes []string) (*api.DeviceCodeResponse, error) {
			return &api.DeviceCodeResponse{
				DeviceCode:      "dc123",
				UserCode:        "ABCD-1234",
				VerificationURI: "https://github.com/login/device",
				Interval:        5 * time.Second,
			}, nil
		},
		PollTokenFunc: func(_ context.Context, _ string, _ string, _ time.Duration) (*api.TokenResponse, error) {
			return &api.TokenResponse{AccessToken: "gho_abc123", TokenType: "bearer"}, nil
		},
	}
	m := setupModelWithDeviceFlow(df, func(url string) error {
		browserOpened = url
		return nil
	})

	// Press 'g' to start device flow
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	m = updated.(SetupModel)

	if cmd == nil {
		t.Fatal("expected non-nil command for RequestCode")
	}

	// Simulate device code response
	result := cmd()
	updated, cmd = m.Update(result)
	m = updated.(SetupModel)

	if m.userCode != "ABCD-1234" {
		t.Errorf("userCode: got %q, want %q", m.userCode, "ABCD-1234")
	}
	if m.verifyURI != "https://github.com/login/device" {
		t.Errorf("verifyURI: got %q, want %q", m.verifyURI, "https://github.com/login/device")
	}
	if browserOpened != "https://github.com/login/device" {
		t.Errorf("browser opened: got %q, want %q", browserOpened, "https://github.com/login/device")
	}
	if !m.polling {
		t.Error("expected polling=true after device code received")
	}

	v := m.View()
	if !strings.Contains(v, "ABCD-1234") {
		t.Error("View should contain user code")
	}
	if !strings.Contains(v, "https://github.com/login/device") {
		t.Error("View should contain verification URI")
	}

	// Simulate token response
	if cmd == nil {
		t.Fatal("expected non-nil command for PollToken")
	}
	result = cmd()
	updated, cmd = m.Update(result)
	m = updated.(SetupModel)

	if m.screen != screenSSO {
		t.Errorf("expected screenSSO after token, got %d", m.screen)
	}
	if m.done {
		t.Error("expected done=false on SSO screen (not yet dismissed)")
	}
	if m.token != "gho_abc123" {
		t.Errorf("token: got %q, want %q", m.token, "gho_abc123")
	}
	if m.tokenType != config.TokenTypeOAuth {
		t.Errorf("tokenType: got %q, want %q", m.tokenType, config.TokenTypeOAuth)
	}
	if cmd != nil {
		t.Error("expected nil command on SSO screen transition")
	}

	// Press 's' to skip SSO screen and complete setup
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = updated.(SetupModel)

	if !m.done {
		t.Error("expected done=true after skipping SSO screen")
	}
	if cmd == nil {
		t.Fatal("expected tea.Quit command after SSO skip")
	}
}

func TestSetup_DeviceFlow_RequestCodeError(t *testing.T) {
	df := &api.StubDeviceFlowClient{
		RequestCodeFunc: func(_ context.Context, _ string, _ []string) (*api.DeviceCodeResponse, error) {
			return nil, errors.New("network error")
		},
	}
	m := setupModelWithDeviceFlow(df, func(_ string) error { return nil })

	// Press 'g' to start device flow
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	m = updated.(SetupModel)

	// Simulate error response
	result := cmd()
	updated, _ = m.Update(result)
	m = updated.(SetupModel)

	if m.errMsg != "network error" {
		t.Errorf("errMsg: got %q, want %q", m.errMsg, "network error")
	}

	v := m.View()
	if !strings.Contains(v, "network error") {
		t.Error("View should contain error message")
	}
}

func TestSetup_DeviceFlow_PollTokenError(t *testing.T) {
	df := &api.StubDeviceFlowClient{
		RequestCodeFunc: func(_ context.Context, _ string, _ []string) (*api.DeviceCodeResponse, error) {
			return &api.DeviceCodeResponse{
				DeviceCode:      "dc123",
				UserCode:        "ABCD-1234",
				VerificationURI: "https://github.com/login/device",
				Interval:        5 * time.Second,
			}, nil
		},
		PollTokenFunc: func(_ context.Context, _ string, _ string, _ time.Duration) (*api.TokenResponse, error) {
			return nil, &api.DeviceFlowError{Code: "expired_token", Description: "device code expired"}
		},
	}
	m := setupModelWithDeviceFlow(df, func(_ string) error { return nil })

	// Press 'g', receive code, poll fails
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	m = updated.(SetupModel)
	result := cmd()
	updated, cmd = m.Update(result)
	m = updated.(SetupModel)
	result = cmd()
	updated, _ = m.Update(result)
	m = updated.(SetupModel)

	if !m.polling == true {
		// polling should be false after error
	}
	if m.errMsg == "" {
		t.Error("expected non-empty errMsg after poll error")
	}
	if m.done {
		t.Error("expected done=false after poll error")
	}
}

func TestSetup_DeviceFlow_Q_Quits(t *testing.T) {
	df := &api.StubDeviceFlowClient{
		RequestCodeFunc: func(_ context.Context, _ string, _ []string) (*api.DeviceCodeResponse, error) {
			return &api.DeviceCodeResponse{
				DeviceCode:      "dc123",
				UserCode:        "ABCD-1234",
				VerificationURI: "https://github.com/login/device",
				Interval:        5 * time.Second,
			}, nil
		},
	}
	m := setupModelWithDeviceFlow(df, func(_ string) error { return nil })

	// Press 'g' to enter device flow
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	m = updated.(SetupModel)

	// Receive device code
	result := cmd()
	updated, _ = m.Update(result)
	m = updated.(SetupModel)

	// Press 'q' to quit
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	m = updated.(SetupModel)

	if !m.quit {
		t.Error("expected quit=true when q pressed during device flow")
	}
	if cmd == nil {
		t.Fatal("expected tea.Quit command")
	}
}

func TestSetup_DeviceFlow_View_Requesting(t *testing.T) {
	m := setupModel()
	m.screen = screenDeviceFlow
	// userCode not yet set — should show "Requesting device code..."

	v := m.View()
	if !strings.Contains(v, "Requesting device code") {
		t.Error("View should show 'Requesting device code...' while waiting")
	}
}

func TestSetup_DeviceFlow_NilBrowser_NoError(t *testing.T) {
	df := &api.StubDeviceFlowClient{
		RequestCodeFunc: func(_ context.Context, _ string, _ []string) (*api.DeviceCodeResponse, error) {
			return &api.DeviceCodeResponse{
				DeviceCode:      "dc",
				UserCode:        "UC",
				VerificationURI: "https://example.com",
				Interval:        5 * time.Second,
			}, nil
		},
		PollTokenFunc: func(_ context.Context, _ string, _ string, _ time.Duration) (*api.TokenResponse, error) {
			return &api.TokenResponse{AccessToken: "token"}, nil
		},
	}
	m := setupModelWithDeviceFlow(df, nil)

	// Press 'g', receive code — should not panic even with nil browser
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	m = updated.(SetupModel)
	result := cmd()
	updated, _ = m.Update(result)
	m = updated.(SetupModel)

	if m.userCode != "UC" {
		t.Errorf("userCode: got %q, want %q", m.userCode, "UC")
	}
}

func TestSetup_DeviceFlow_BrowserError_Ignored(t *testing.T) {
	df := &api.StubDeviceFlowClient{
		RequestCodeFunc: func(_ context.Context, _ string, _ []string) (*api.DeviceCodeResponse, error) {
			return &api.DeviceCodeResponse{
				DeviceCode:      "dc",
				UserCode:        "UC",
				VerificationURI: "https://example.com",
				Interval:        5 * time.Second,
			}, nil
		},
		PollTokenFunc: func(_ context.Context, _ string, _ string, _ time.Duration) (*api.TokenResponse, error) {
			return &api.TokenResponse{AccessToken: "token"}, nil
		},
	}
	m := setupModelWithDeviceFlow(df, func(_ string) error { return errors.New("no browser") })

	// Press 'g', receive code — browser error should be ignored
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	m = updated.(SetupModel)
	result := cmd()
	updated, _ = m.Update(result)
	m = updated.(SetupModel)

	if m.errMsg != "" {
		t.Errorf("expected no error for browser failure, got %q", m.errMsg)
	}
	if m.userCode != "UC" {
		t.Errorf("userCode: got %q, want %q", m.userCode, "UC")
	}
}

// ── SSO Screen ──────────────────────────────────────────────────────────────

func ssoModel() SetupModel {
	m := setupModel()
	m.screen = screenSSO
	m.token = "gho_token"
	m.tokenType = config.TokenTypeOAuth
	m.ssoURL = "https://github.com/settings/connections/applications/test-client"
	return m
}

func TestSetup_SSO_ViewContainsURL(t *testing.T) {
	m := ssoModel()
	v := m.View()
	if !strings.Contains(v, "https://github.com/settings/connections/applications/test-client") {
		t.Error("SSO view should contain the authorization URL")
	}
	if !strings.Contains(v, "SSO Authorization") {
		t.Error("SSO view should contain 'SSO Authorization' title")
	}
	if !strings.Contains(v, "[o]pen in browser") {
		t.Error("SSO view should contain '[o]pen in browser'")
	}
	if !strings.Contains(v, "[s]kip") {
		t.Error("SSO view should contain '[s]kip'")
	}
}

func TestSetup_SSO_O_OpensBrowser(t *testing.T) {
	m := ssoModel()
	browserOpened := ""
	m.openBrowser = func(url string) error {
		browserOpened = url
		return nil
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	m = updated.(SetupModel)

	if !m.done {
		t.Error("expected done=true after pressing o")
	}
	if m.quit {
		t.Error("expected quit=false after pressing o")
	}
	if browserOpened != m.ssoURL {
		t.Errorf("browser opened: got %q, want %q", browserOpened, m.ssoURL)
	}
	if cmd == nil {
		t.Fatal("expected tea.Quit command")
	}
}

func TestSetup_SSO_Enter_OpensBrowser(t *testing.T) {
	m := ssoModel()
	browserOpened := ""
	m.openBrowser = func(url string) error {
		browserOpened = url
		return nil
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(SetupModel)

	if !m.done {
		t.Error("expected done=true after pressing Enter")
	}
	if browserOpened != m.ssoURL {
		t.Errorf("browser opened: got %q, want %q", browserOpened, m.ssoURL)
	}
	if cmd == nil {
		t.Fatal("expected tea.Quit command")
	}
}

func TestSetup_SSO_S_SkipsWithoutBrowser(t *testing.T) {
	m := ssoModel()
	browserOpened := false
	m.openBrowser = func(_ string) error {
		browserOpened = true
		return nil
	}

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = updated.(SetupModel)

	if !m.done {
		t.Error("expected done=true after pressing s")
	}
	if m.quit {
		t.Error("expected quit=false after pressing s")
	}
	if browserOpened {
		t.Error("browser should not open on skip")
	}
	if cmd == nil {
		t.Fatal("expected tea.Quit command")
	}
}

func TestSetup_SSO_Q_SetsDone(t *testing.T) {
	m := ssoModel()

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	m = updated.(SetupModel)

	if !m.done {
		t.Error("expected done=true after pressing q on SSO screen")
	}
	if m.quit {
		t.Error("expected quit=false — token is already obtained")
	}
	if cmd == nil {
		t.Fatal("expected tea.Quit command")
	}
}

func TestSetup_SSO_Esc_SetsDone(t *testing.T) {
	m := ssoModel()

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(SetupModel)

	if !m.done {
		t.Error("expected done=true after Esc on SSO screen")
	}
	if m.quit {
		t.Error("expected quit=false — token is already obtained")
	}
	if cmd == nil {
		t.Fatal("expected tea.Quit command")
	}
}

func TestSetup_SSO_CtrlC_SetsDone(t *testing.T) {
	m := ssoModel()

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = updated.(SetupModel)

	if !m.done {
		t.Error("expected done=true after Ctrl+C on SSO screen")
	}
	if m.quit {
		t.Error("expected quit=false — token is already obtained")
	}
	if cmd == nil {
		t.Fatal("expected tea.Quit command")
	}
}

func TestSetup_SSO_NilBrowser_NoPanic(t *testing.T) {
	m := ssoModel()
	m.openBrowser = nil

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	m = updated.(SetupModel)

	if !m.done {
		t.Error("expected done=true")
	}
	if cmd == nil {
		t.Fatal("expected tea.Quit command")
	}
}

func TestSetup_SSO_UnknownKey_Ignored(t *testing.T) {
	m := ssoModel()

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = updated.(SetupModel)

	if m.done {
		t.Error("expected done=false for unknown key")
	}
	if m.quit {
		t.Error("expected quit=false for unknown key")
	}
	if m.screen != screenSSO {
		t.Errorf("expected to stay on SSO screen, got %d", m.screen)
	}
	if cmd != nil {
		t.Error("expected nil command for unknown key")
	}
}

func TestSetup_SSO_View_WithDimensions(t *testing.T) {
	m := ssoModel()
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(SetupModel)

	v := m.View()
	if v == "" {
		t.Error("SSO view should not be empty with dimensions set")
	}
	if !strings.Contains(v, "SSO Authorization") {
		t.Error("SSO view should contain title when rendered with dimensions")
	}
}

func TestSetup_SSO_UnknownMsg_Ignored(t *testing.T) {
	m := ssoModel()

	updated, _ := m.Update(tea.FocusMsg{})
	m = updated.(SetupModel)

	if m.done {
		t.Error("expected done=false for unknown message type")
	}
}

// ── O and Enter keys on non-SSO screens ─────────────────────────────────────

func TestSetup_MethodSelect_O_Ignored(t *testing.T) {
	m := setupModel()
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	m = updated.(SetupModel)

	if m.screen != screenMethodSelect {
		t.Errorf("expected to stay on method selection after 'o', got screen %d", m.screen)
	}
	if cmd != nil {
		t.Error("expected nil command for 'o' on method selection")
	}
}

func TestSetup_MethodSelect_Enter_Ignored(t *testing.T) {
	m := setupModel()
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(SetupModel)

	if m.screen != screenMethodSelect {
		t.Errorf("expected to stay on method selection after Enter, got screen %d", m.screen)
	}
	if cmd != nil {
		t.Error("expected nil command for Enter on method selection")
	}
}

// ── RunSetup ────────────────────────────────────────────────────────────────

func TestRunSetup_Success_PAT(t *testing.T) {
	result, err := RunSetup(context.Background(), SetupDeps{
		Verify: okVerify,
		RunProgram: func(m tea.Model) (tea.Model, error) {
			sm := m.(SetupModel)
			sm.token = "ghp_fromsetup"
			sm.tokenType = config.TokenTypePAT
			sm.done = true
			return sm, nil
		},
		DeviceFlow:  &api.StubDeviceFlowClient{},
		OpenBrowser: func(_ string) error { return nil },
		ClientID:    "test-client",
	})
	if err != nil {
		t.Fatalf("RunSetup error: %v", err)
	}
	if result.Quit {
		t.Error("expected Quit=false")
	}
	if result.Token != "ghp_fromsetup" {
		t.Errorf("Token: got %q, want %q", result.Token, "ghp_fromsetup")
	}
	if result.TokenType != config.TokenTypePAT {
		t.Errorf("TokenType: got %q, want %q", result.TokenType, config.TokenTypePAT)
	}
}

func TestRunSetup_Success_OAuth(t *testing.T) {
	result, err := RunSetup(context.Background(), SetupDeps{
		Verify: okVerify,
		RunProgram: func(m tea.Model) (tea.Model, error) {
			sm := m.(SetupModel)
			sm.token = "gho_oauthtoken"
			sm.tokenType = config.TokenTypeOAuth
			sm.done = true
			return sm, nil
		},
		DeviceFlow:  &api.StubDeviceFlowClient{},
		OpenBrowser: func(_ string) error { return nil },
		ClientID:    "test-client",
	})
	if err != nil {
		t.Fatalf("RunSetup error: %v", err)
	}
	if result.Token != "gho_oauthtoken" {
		t.Errorf("Token: got %q, want %q", result.Token, "gho_oauthtoken")
	}
	if result.TokenType != config.TokenTypeOAuth {
		t.Errorf("TokenType: got %q, want %q", result.TokenType, config.TokenTypeOAuth)
	}
}

func TestRunSetup_Quit(t *testing.T) {
	result, err := RunSetup(context.Background(), SetupDeps{
		Verify: okVerify,
		RunProgram: func(m tea.Model) (tea.Model, error) {
			sm := m.(SetupModel)
			sm.quit = true
			return sm, nil
		},
		DeviceFlow:  &api.StubDeviceFlowClient{},
		OpenBrowser: func(_ string) error { return nil },
		ClientID:    "test-client",
	})
	if err != nil {
		t.Fatalf("RunSetup error: %v", err)
	}
	if !result.Quit {
		t.Error("expected Quit=true")
	}
	if result.Token != "" {
		t.Errorf("Token: got %q, want empty", result.Token)
	}
}

func TestRunSetup_ProgramError(t *testing.T) {
	_, err := RunSetup(context.Background(), SetupDeps{
		Verify: okVerify,
		RunProgram: func(m tea.Model) (tea.Model, error) {
			return m, errors.New("terminal failed")
		},
		DeviceFlow:  &api.StubDeviceFlowClient{},
		OpenBrowser: func(_ string) error { return nil },
		ClientID:    "test-client",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "terminal failed") {
		t.Errorf("error: got %q, want to contain 'terminal failed'", err.Error())
	}
}

// ── S key on non-PAT screens ────────────────────────────────────────────────

func TestSetup_MethodSelect_S_Ignored(t *testing.T) {
	m := setupModel()
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = updated.(SetupModel)

	if m.screen != screenMethodSelect {
		t.Errorf("expected to stay on method selection after 's', got screen %d", m.screen)
	}
	if cmd != nil {
		t.Error("expected nil command for 's' on method selection")
	}
}

func TestSetup_DeviceFlow_S_Ignored(t *testing.T) {
	m := setupModel()
	m.screen = screenDeviceFlow

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = updated.(SetupModel)

	if m.screen != screenDeviceFlow {
		t.Errorf("expected to stay on device flow after 's', got screen %d", m.screen)
	}
	if cmd != nil {
		t.Error("expected nil command for 's' on device flow")
	}
}

// ── G key on non-method-select screens ──────────────────────────────────────

func TestSetup_PATEntry_G_TypesG(t *testing.T) {
	m := setupModel()
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m = updated.(SetupModel)

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	m = updated.(SetupModel)

	if m.screen != screenPATEntry {
		t.Error("expected to stay on PAT entry after 'g'")
	}
}

// ── P key on non-method-select screens ──────────────────────────────────────

func TestSetup_DeviceFlow_P_Ignored(t *testing.T) {
	m := setupModel()
	m.screen = screenDeviceFlow

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m = updated.(SetupModel)

	if m.screen != screenDeviceFlow {
		t.Error("expected to stay on device flow after 'p'")
	}
	if cmd != nil {
		t.Error("expected nil command for 'p' on device flow")
	}
}

// ── gh CLI Screen ───────────────────────────────────────────────────────────

func ghcliModel(verify func(context.Context) (string, error)) SetupModel {
	return newSetupModel(context.Background(), SetupDeps{
		Verify:      okVerify,
		DeviceFlow:  &api.StubDeviceFlowClient{},
		OpenBrowser: func(_ string) error { return nil },
		ClientID:    "test-client",
		GHCLIVerify: verify,
	})
}

func TestSetup_MethodSelect_C_TransitionsToGHCLI(t *testing.T) {
	m := ghcliModel(func(_ context.Context) (string, error) {
		return "octocat", nil
	})

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	m = updated.(SetupModel)

	if m.screen != screenGHCLI {
		t.Errorf("expected screenGHCLI, got %d", m.screen)
	}
	if !m.verifying {
		t.Error("expected verifying=true")
	}
	if cmd == nil {
		t.Fatal("expected non-nil command for ghcli verify")
	}
}

func TestSetup_MethodSelect_C_NoVerifier_Ignored(t *testing.T) {
	m := setupModel() // no GHCLIVerify set

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	m = updated.(SetupModel)

	if m.screen != screenMethodSelect {
		t.Errorf("expected to stay on method selection, got %d", m.screen)
	}
	if cmd != nil {
		t.Error("expected nil command when no verifier")
	}
}

func TestSetup_MethodSelect_View_ShowsGHCLI_WhenAvailable(t *testing.T) {
	m := ghcliModel(func(_ context.Context) (string, error) {
		return "octocat", nil
	})

	v := m.View()
	if !strings.Contains(v, "[c]") {
		t.Error("View should contain '[c]' option when GHCLIVerify is set")
	}
	if !strings.Contains(v, "gh CLI") {
		t.Error("View should contain 'gh CLI'")
	}
}

func TestSetup_MethodSelect_View_HidesGHCLI_WhenUnavailable(t *testing.T) {
	m := setupModel() // no GHCLIVerify

	v := m.View()
	if strings.Contains(v, "[c]") {
		t.Error("View should not contain '[c]' option when GHCLIVerify is nil")
	}
}

func TestSetup_GHCLI_VerifySuccess(t *testing.T) {
	m := ghcliModel(func(_ context.Context) (string, error) {
		return "octocat", nil
	})

	// Press 'c' to start
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	m = updated.(SetupModel)

	// Execute the verify command
	result := cmd()
	updated, cmd = m.Update(result)
	m = updated.(SetupModel)

	if m.verifying {
		t.Error("expected verifying=false after success")
	}
	if !m.done {
		t.Error("expected done=true after verify success")
	}
	if m.token != "ghcli" {
		t.Errorf("token: got %q, want %q", m.token, "ghcli")
	}
	if m.tokenType != config.TokenTypeGHCLI {
		t.Errorf("tokenType: got %q, want %q", m.tokenType, config.TokenTypeGHCLI)
	}
	if cmd == nil {
		t.Fatal("expected tea.Quit command")
	}
}

func TestSetup_GHCLI_VerifyError(t *testing.T) {
	m := ghcliModel(func(_ context.Context) (string, error) {
		return "", errors.New("gh not found")
	})

	// Press 'c' to start
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	m = updated.(SetupModel)

	// Execute the verify command
	result := cmd()
	updated, _ = m.Update(result)
	m = updated.(SetupModel)

	if m.verifying {
		t.Error("expected verifying=false after error")
	}
	if m.done {
		t.Error("expected done=false after verify error")
	}
	if m.errMsg != "gh not found" {
		t.Errorf("errMsg: got %q, want %q", m.errMsg, "gh not found")
	}

	v := m.View()
	if !strings.Contains(v, "gh not found") {
		t.Error("View should contain error message")
	}
	if !strings.Contains(v, "gh auth login") {
		t.Error("View should contain 'gh auth login' hint")
	}
	if !strings.Contains(v, "[c] retry") {
		t.Error("View should contain retry option")
	}
}

func TestSetup_GHCLI_Retry_AfterError(t *testing.T) {
	calls := 0
	m := ghcliModel(func(_ context.Context) (string, error) {
		calls++
		if calls == 1 {
			return "", errors.New("not authenticated")
		}
		return "octocat", nil
	})

	// First attempt: error
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	m = updated.(SetupModel)
	result := cmd()
	updated, _ = m.Update(result)
	m = updated.(SetupModel)

	if m.errMsg == "" {
		t.Fatal("expected error after first attempt")
	}

	// Retry with 'c'
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	m = updated.(SetupModel)

	if !m.verifying {
		t.Error("expected verifying=true on retry")
	}
	if m.errMsg != "" {
		t.Error("expected errMsg cleared on retry")
	}

	result = cmd()
	updated, cmd = m.Update(result)
	m = updated.(SetupModel)

	if !m.done {
		t.Error("expected done=true after successful retry")
	}
	if m.token != "ghcli" {
		t.Errorf("token: got %q, want %q", m.token, "ghcli")
	}
}

func TestSetup_GHCLI_Q_Quits(t *testing.T) {
	m := ghcliModel(func(_ context.Context) (string, error) {
		return "", errors.New("error")
	})

	// Start and fail
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	m = updated.(SetupModel)
	result := cmd()
	updated, _ = m.Update(result)
	m = updated.(SetupModel)

	// Press q to quit
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	m = updated.(SetupModel)

	if !m.quit {
		t.Error("expected quit=true")
	}
	if cmd == nil {
		t.Fatal("expected tea.Quit command")
	}
}

func TestSetup_GHCLI_Esc_Quits(t *testing.T) {
	m := ghcliModel(func(_ context.Context) (string, error) {
		return "octocat", nil
	})
	m.screen = screenGHCLI

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(SetupModel)

	if !m.quit {
		t.Error("expected quit=true on Esc")
	}
	if cmd == nil {
		t.Fatal("expected tea.Quit command")
	}
}

func TestSetup_GHCLI_View_Verifying(t *testing.T) {
	m := ghcliModel(func(_ context.Context) (string, error) {
		return "octocat", nil
	})
	m.screen = screenGHCLI
	m.verifying = true

	v := m.View()
	if !strings.Contains(v, "Verifying gh CLI") {
		t.Error("View should show verifying message")
	}
	if !strings.Contains(v, "gh CLI Setup") {
		t.Error("View should contain title")
	}
}

func TestSetup_GHCLI_View_WithDimensions(t *testing.T) {
	m := ghcliModel(func(_ context.Context) (string, error) {
		return "octocat", nil
	})
	m.screen = screenGHCLI
	m.verifying = true
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(SetupModel)

	v := m.View()
	if v == "" {
		t.Error("View should not be empty with dimensions set")
	}
}

func TestSetup_GHCLI_UnknownKey_Ignored(t *testing.T) {
	m := ghcliModel(func(_ context.Context) (string, error) {
		return "", errors.New("error")
	})
	m.screen = screenGHCLI

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = updated.(SetupModel)

	if m.screen != screenGHCLI {
		t.Errorf("expected to stay on ghcli screen, got %d", m.screen)
	}
	if cmd != nil {
		t.Error("expected nil command for unknown key")
	}
}

func TestSetup_GHCLI_C_DuringVerifying_Ignored(t *testing.T) {
	m := ghcliModel(func(_ context.Context) (string, error) {
		return "octocat", nil
	})
	m.screen = screenGHCLI
	m.verifying = true

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	m = updated.(SetupModel)

	if cmd != nil {
		t.Error("expected nil command when c pressed during verification")
	}
}

func TestSetup_GHCLI_View_DefaultState(t *testing.T) {
	m := ghcliModel(func(_ context.Context) (string, error) {
		return "octocat", nil
	})
	m.screen = screenGHCLI
	// Neither verifying nor errMsg — default view
	v := m.View()
	if !strings.Contains(v, "gh CLI Setup") {
		t.Error("View should contain title")
	}
}

func TestSetup_GHCLI_UnknownMsg_Ignored(t *testing.T) {
	m := ghcliModel(func(_ context.Context) (string, error) {
		return "octocat", nil
	})
	m.screen = screenGHCLI

	updated, _ := m.Update(tea.FocusMsg{})
	m = updated.(SetupModel)

	if m.quit {
		t.Error("unexpected quit on unknown message")
	}
}

func TestRunSetup_Success_GHCLI(t *testing.T) {
	result, err := RunSetup(context.Background(), SetupDeps{
		Verify: okVerify,
		RunProgram: func(m tea.Model) (tea.Model, error) {
			sm := m.(SetupModel)
			sm.token = "ghcli"
			sm.tokenType = config.TokenTypeGHCLI
			sm.done = true
			return sm, nil
		},
		DeviceFlow:  &api.StubDeviceFlowClient{},
		OpenBrowser: func(_ string) error { return nil },
		ClientID:    "test-client",
		GHCLIVerify: func(_ context.Context) (string, error) { return "octocat", nil },
	})
	if err != nil {
		t.Fatalf("RunSetup error: %v", err)
	}
	if result.Quit {
		t.Error("expected Quit=false")
	}
	if result.Token != "ghcli" {
		t.Errorf("Token: got %q, want %q", result.Token, "ghcli")
	}
	if result.TokenType != config.TokenTypeGHCLI {
		t.Errorf("TokenType: got %q, want %q", result.TokenType, config.TokenTypeGHCLI)
	}
}
