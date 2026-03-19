package api_test

import (
	"context"
	"strings"
	"testing"

	"github.com/evanisnor/argh/internal/api"
)

func TestOSCommandExecutor_Output(t *testing.T) {
	exec := &api.OSCommandExecutor{}
	ctx := context.Background()

	t.Run("successful command returns output", func(t *testing.T) {
		out, err := exec.Output(ctx, "echo", "hello")
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if !strings.Contains(string(out), "hello") {
			t.Errorf("expected output to contain 'hello', got: %q", string(out))
		}
	})

	t.Run("missing command returns error", func(t *testing.T) {
		_, err := exec.Output(ctx, "argh-nonexistent-binary-xyz")
		if err == nil {
			t.Fatal("expected error for missing binary, got nil")
		}
	})
}

func TestStubCommandExecutor_unconfigured(t *testing.T) {
	exec := api.NewStubCommandExecutor()
	ctx := context.Background()

	_, err := exec.Output(ctx, "unconfigured", "command")
	if err == nil {
		t.Fatal("expected error for unconfigured command, got nil")
	}
	if !strings.Contains(err.Error(), "no response configured") {
		t.Errorf("expected 'no response configured' in error, got: %v", err)
	}
}
