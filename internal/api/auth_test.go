package api_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/evanisnor/argh/internal/api"
)

func TestAuthenticate(t *testing.T) {
	ctx := context.Background()

	t.Run("gh returns token and login fetched", func(t *testing.T) {
		exec := api.NewStubCommandExecutor()
		exec.Responses["gh auth token"] = []byte("ghs_testtoken123\n")
		exec.Responses["gh api user --jq .login"] = []byte("octocat\n")

		creds, err := api.Authenticate(ctx, exec)
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if creds.Token != "ghs_testtoken123" {
			t.Errorf("expected token %q, got %q", "ghs_testtoken123", creds.Token)
		}
		if creds.Login != "octocat" {
			t.Errorf("expected login %q, got %q", "octocat", creds.Login)
		}
	})

	t.Run("gh not found returns clear error", func(t *testing.T) {
		exec := api.NewStubCommandExecutor()
		exec.Errors["gh auth token"] = errors.New("exec: \"gh\": executable file not found in $PATH")

		_, err := api.Authenticate(ctx, exec)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "gh auth token") {
			t.Errorf("expected error to mention 'gh auth token', got: %v", err)
		}
	})

	t.Run("gh not authenticated returns clear error", func(t *testing.T) {
		exec := api.NewStubCommandExecutor()
		exec.Errors["gh auth token"] = errors.New("exit status 1")

		_, err := api.Authenticate(ctx, exec)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "gh auth token") {
			t.Errorf("expected error to mention 'gh auth token', got: %v", err)
		}
	})

	t.Run("gh returns empty token returns clear error", func(t *testing.T) {
		exec := api.NewStubCommandExecutor()
		exec.Responses["gh auth token"] = []byte("\n")
		exec.Responses["gh api user --jq .login"] = []byte("octocat\n")

		_, err := api.Authenticate(ctx, exec)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "empty token") {
			t.Errorf("expected error to mention 'empty token', got: %v", err)
		}
	})

	t.Run("gh api user fails returns error", func(t *testing.T) {
		exec := api.NewStubCommandExecutor()
		exec.Responses["gh auth token"] = []byte("ghs_testtoken123\n")
		exec.Errors["gh api user --jq .login"] = errors.New("exit status 1")

		_, err := api.Authenticate(ctx, exec)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "gh api user") {
			t.Errorf("expected error to mention 'gh api user', got: %v", err)
		}
	})
}
