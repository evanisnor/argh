package ghcli

import (
	"context"
	"errors"
	"testing"
)

func TestGHCLIAuthVerifier_Verify(t *testing.T) {
	tests := []struct {
		name      string
		stdout    string
		runErr    error
		wantLogin string
		wantErr   bool
	}{
		{
			name:      "successful auth — modern format",
			stdout:    "github.com\n  ✓ Logged in to github.com account alice (keyring)\n  - Active account: true\n",
			wantLogin: "alice",
		},
		{
			name:      "successful auth — account at end of line",
			stdout:    "github.com\n  ✓ Logged in to github.com account bob\n",
			wantLogin: "bob",
		},
		{
			name:    "not authenticated — command error",
			runErr:  errors.New("gh auth status failed: not logged in"),
			wantErr: true,
		},
		{
			name:    "malformed output — no account line",
			stdout:  "github.com\n  some other output\n",
			wantErr: true,
		},
		{
			name:    "empty output",
			stdout:  "",
			wantErr: true,
		},
		{
			name:      "account with parenthetical token type",
			stdout:    "  Logged in to github.com account carol (oauth_token)\n",
			wantLogin: "carol",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := NewStubCommandRunner()
			runner.RunFunc = func(_ context.Context, args []string) ([]byte, error) {
				return []byte(tt.stdout), tt.runErr
			}

			verifier := &GHCLIAuthVerifier{Runner: runner}
			login, err := verifier.Verify(context.Background(), "ignored-token")

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if login != tt.wantLogin {
				t.Errorf("login = %q, want %q", login, tt.wantLogin)
			}

			// Verify correct args were passed.
			call := runner.LastCall()
			if len(call) < 4 || call[0] != "auth" || call[1] != "status" || call[2] != "--hostname" || call[3] != "github.com" {
				t.Errorf("expected [auth status --hostname github.com], got %v", call)
			}
		})
	}
}

func TestParseGHAuthLogin(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   string
	}{
		{
			name:   "standard format with keyring",
			output: "  ✓ Logged in to github.com account alice (keyring)\n",
			want:   "alice",
		},
		{
			name:   "account at end of line",
			output: "  ✓ Logged in to github.com account bob\n",
			want:   "bob",
		},
		{
			name:   "no account line",
			output: "  You are not logged in\n",
			want:   "",
		},
		{
			name:   "empty string",
			output: "",
			want:   "",
		},
		{
			name:   "account with extra spaces",
			output: "  account dave (token)\n",
			want:   "dave",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseGHAuthLogin(tt.output)
			if got != tt.want {
				t.Errorf("parseGHAuthLogin = %q, want %q", got, tt.want)
			}
		})
	}
}
