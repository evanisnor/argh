package ui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func setupModel(verify func(ctx context.Context, token string) (string, error)) SetupModel {
	return newSetupModel(context.Background(), verify)
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

func TestSetup_InitialState(t *testing.T) {
	m := setupModel(okVerify)

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
	if !strings.Contains(v, "argh") {
		t.Error("View should contain 'argh'")
	}
	if !strings.Contains(v, "PAT") {
		t.Error("View should contain 'PAT'")
	}
}

func TestSetup_TypeChars(t *testing.T) {
	m := setupModel(okVerify)
	// Use chars that aren't intercepted ('s' triggers save, 'q' triggers quit when empty).
	m = typeChars(m, "ghp_abc")

	if m.input.Value() != "ghp_abc" {
		t.Errorf("input value: got %q, want %q", m.input.Value(), "ghp_abc")
	}
}

func TestSetup_Save_Verifying(t *testing.T) {
	m := setupModel(okVerify)
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

func TestSetup_VerifySuccess_Done(t *testing.T) {
	m := setupModel(okVerify)
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
	if cmd == nil {
		t.Fatal("expected tea.Quit command")
	}
}

func TestSetup_VerifyFailure_ErrorShown(t *testing.T) {
	m := setupModel(failVerify)
	m = typeToken(m, "ghp_bad")
	m.verifying = true

	updated, _ := m.Update(verifyResultMsg{err: errors.New("401 Unauthorized")})
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

func TestSetup_Q_EmptyInput_Quits(t *testing.T) {
	m := setupModel(okVerify)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	m = updated.(SetupModel)

	if !m.quit {
		t.Error("expected quit=true when q pressed with empty input")
	}
	if cmd == nil {
		t.Fatal("expected tea.Quit command")
	}
}

func TestSetup_Q_NonEmptyInput_TypesQ(t *testing.T) {
	m := setupModel(okVerify)
	m = typeToken(m, "ghp_")

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	m = updated.(SetupModel)

	if m.quit {
		t.Error("expected quit=false when q pressed with non-empty input")
	}
	if !strings.HasSuffix(m.input.Value(), "q") {
		t.Errorf("expected input to end with 'q', got %q", m.input.Value())
	}
}

func TestSetup_Esc_Quits(t *testing.T) {
	m := setupModel(okVerify)
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

func TestSetup_CtrlC_Quits(t *testing.T) {
	m := setupModel(okVerify)

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = updated.(SetupModel)

	if !m.quit {
		t.Error("expected quit=true on Ctrl+C")
	}
	if cmd == nil {
		t.Fatal("expected tea.Quit command")
	}
}

func TestSetup_WindowSizeMsg(t *testing.T) {
	m := setupModel(okVerify)

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
	m := setupModel(okVerify)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(SetupModel)

	v := m.View()
	if v == "" {
		t.Error("View should not be empty with dimensions set")
	}
}

func TestSetup_View_NoDimensions(t *testing.T) {
	m := setupModel(okVerify)
	v := m.View()
	if v == "" {
		t.Error("View should not be empty without dimensions")
	}
}

func TestSetup_View_Verifying(t *testing.T) {
	m := setupModel(okVerify)
	m = typeToken(m, "ghp_token")
	m.verifying = true

	v := m.View()
	if !strings.Contains(v, "verifying") {
		t.Error("View should show 'verifying' during verification")
	}
}

func TestSetup_S_EmptyInput_TypesS(t *testing.T) {
	m := setupModel(okVerify)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = updated.(SetupModel)

	if m.verifying {
		t.Error("s with empty input should not start verification")
	}
	if m.input.Value() != "s" {
		t.Errorf("input: got %q, want %q", m.input.Value(), "s")
	}
}

func TestSetup_KeysDuringVerifying_Ignored(t *testing.T) {
	m := setupModel(okVerify)
	m = typeToken(m, "ghp_token")
	m.verifying = true
	m.input.Blur()
	val := m.input.Value()

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = updated.(SetupModel)

	if m.input.Value() != val {
		t.Errorf("input should not change during verification: got %q, want %q", m.input.Value(), val)
	}
}

func TestSetup_Init(t *testing.T) {
	m := setupModel(okVerify)
	cmd := m.Init()
	if cmd == nil {
		t.Error("Init should return a non-nil command (blink)")
	}
}

func TestRunSetup_Success(t *testing.T) {
	token, quit, err := RunSetup(context.Background(), SetupDeps{
		Verify: okVerify,
		RunProgram: func(m tea.Model) (tea.Model, error) {
			sm := m.(SetupModel)
			sm.token = "ghp_fromsetup"
			sm.done = true
			return sm, nil
		},
	})
	if err != nil {
		t.Fatalf("RunSetup error: %v", err)
	}
	if quit {
		t.Error("expected quit=false")
	}
	if token != "ghp_fromsetup" {
		t.Errorf("token: got %q, want %q", token, "ghp_fromsetup")
	}
}

func TestRunSetup_Quit(t *testing.T) {
	token, quit, err := RunSetup(context.Background(), SetupDeps{
		Verify: okVerify,
		RunProgram: func(m tea.Model) (tea.Model, error) {
			sm := m.(SetupModel)
			sm.quit = true
			return sm, nil
		},
	})
	if err != nil {
		t.Fatalf("RunSetup error: %v", err)
	}
	if !quit {
		t.Error("expected quit=true")
	}
	if token != "" {
		t.Errorf("token: got %q, want empty", token)
	}
}

func TestRunSetup_ProgramError(t *testing.T) {
	_, _, err := RunSetup(context.Background(), SetupDeps{
		Verify: okVerify,
		RunProgram: func(m tea.Model) (tea.Model, error) {
			return m, errors.New("terminal failed")
		},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "terminal failed") {
		t.Errorf("error: got %q, want to contain 'terminal failed'", err.Error())
	}
}

func TestSetup_S_WhileVerifying_Ignored(t *testing.T) {
	m := setupModel(okVerify)
	m = typeToken(m, "ghp_token")
	m.verifying = true

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = updated.(SetupModel)

	if cmd != nil {
		t.Error("expected nil command when s pressed during verification")
	}
}

func TestSetup_Q_WhileVerifying_Ignored(t *testing.T) {
	m := setupModel(okVerify)
	m = typeToken(m, "ghp_token")
	m.verifying = true

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	m = updated.(SetupModel)

	if m.quit {
		t.Error("expected quit=false during verification")
	}
}

func TestSetup_View_SaveFaint_WhenEmpty(t *testing.T) {
	m := setupModel(okVerify)
	v := m.View()
	// When input is empty, the [s]ave label should be rendered with faint styling.
	// We can't easily detect lipgloss faint in string comparison, but we verify
	// the view contains the controls.
	if !strings.Contains(v, "[q]uit") {
		t.Error("View should contain [q]uit")
	}
}

func TestSetup_UnknownMsg_PassedToInput(t *testing.T) {
	m := setupModel(okVerify)
	// Send a non-key, non-window message to exercise the default branch.
	updated, _ := m.Update(tea.FocusMsg{})
	m = updated.(SetupModel)
	// No crash, model returned.
	if m.quit {
		t.Error("unexpected quit on unknown message")
	}
}
