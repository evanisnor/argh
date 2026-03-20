package ghcli

import (
	"context"
	"errors"
	"testing"
)

func TestStubCommandRunner_RecordsCalls(t *testing.T) {
	runner := NewStubCommandRunner()
	runner.RunFunc = func(_ context.Context, args []string) ([]byte, error) {
		return []byte("ok"), nil
	}

	out, err := runner.Run(context.Background(), []string{"auth", "status"})
	if err != nil {
		t.Fatalf("Run error = %v", err)
	}
	if string(out) != "ok" {
		t.Errorf("output = %q, want %q", string(out), "ok")
	}
	if runner.CallCount() != 1 {
		t.Errorf("CallCount = %d, want 1", runner.CallCount())
	}
	last := runner.LastCall()
	if len(last) != 2 || last[0] != "auth" || last[1] != "status" {
		t.Errorf("LastCall = %v, want [auth status]", last)
	}
}

func TestStubCommandRunner_FindCall(t *testing.T) {
	runner := NewStubCommandRunner()
	runner.Run(context.Background(), []string{"pr", "diff", "42"})
	runner.Run(context.Background(), []string{"auth", "status"})

	found := runner.FindCall("auth", "status")
	if found == nil {
		t.Fatal("FindCall returned nil, expected a match")
	}
	if found[0] != "auth" {
		t.Errorf("found[0] = %q, want %q", found[0], "auth")
	}

	notFound := runner.FindCall("nonexistent")
	if notFound != nil {
		t.Errorf("FindCall should return nil for no match, got %v", notFound)
	}
}

func TestStubCommandRunner_LastCall_Empty(t *testing.T) {
	runner := NewStubCommandRunner()
	if last := runner.LastCall(); last != nil {
		t.Errorf("LastCall on empty runner = %v, want nil", last)
	}
}

func TestCheckInstalled_Found(t *testing.T) {
	lookup := StubLookup("/usr/local/bin/gh", nil)
	if err := CheckInstalled(lookup); err != nil {
		t.Errorf("CheckInstalled error = %v, want nil", err)
	}
}

func TestStubLookup_WithError(t *testing.T) {
	testErr := errors.New("lookup failed")
	lookup := StubLookup("", testErr)
	_, err := lookup("gh")
	if !errors.Is(err, testErr) {
		t.Errorf("error = %v, want %v", err, testErr)
	}
}

func TestCheckInstalled_NotFound(t *testing.T) {
	lookup := StubLookupNotFound()
	err := CheckInstalled(lookup)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestStubCommandRunner_RunFunc_Error(t *testing.T) {
	runner := NewStubCommandRunner()
	testErr := errors.New("command failed")
	runner.RunFunc = func(_ context.Context, _ []string) ([]byte, error) {
		return nil, testErr
	}

	_, err := runner.Run(context.Background(), []string{"test"})
	if !errors.Is(err, testErr) {
		t.Errorf("error = %v, want %v", err, testErr)
	}
	if runner.CallCount() != 1 {
		t.Errorf("CallCount = %d, want 1", runner.CallCount())
	}
}
