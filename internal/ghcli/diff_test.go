package ghcli

import (
	"context"
	"errors"
	"testing"
)

func TestGHCLIDiffFetcher_Fetch_Success(t *testing.T) {
	runner := NewStubCommandRunner()
	runner.RunFunc = func(_ context.Context, _ []string) ([]byte, error) {
		return []byte("diff --git a/file.go b/file.go\n"), nil
	}

	f := &GHCLIDiffFetcher{Runner: runner}
	out, err := f.Fetch("https://github.com/owner/repo/pull/42", "")
	if err != nil {
		t.Fatalf("Fetch error = %v", err)
	}
	if string(out) != "diff --git a/file.go b/file.go\n" {
		t.Errorf("output = %q", string(out))
	}

	call := runner.FindCall("pr", "diff", "42")
	if call == nil {
		t.Error("expected pr diff 42 call")
	}
	if runner.FindCall("-R", "owner/repo") == nil {
		t.Error("expected -R owner/repo")
	}
}

func TestGHCLIDiffFetcher_Fetch_CommandError(t *testing.T) {
	cmdErr := errors.New("gh failed")
	runner := NewStubCommandRunner()
	runner.RunFunc = func(_ context.Context, _ []string) ([]byte, error) {
		return nil, cmdErr
	}

	f := &GHCLIDiffFetcher{Runner: runner}
	_, err := f.Fetch("https://github.com/owner/repo/pull/42", "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, cmdErr) {
		t.Errorf("error = %v, want to wrap %v", err, cmdErr)
	}
}

func TestGHCLIDiffFetcher_Fetch_InvalidURL(t *testing.T) {
	runner := NewStubCommandRunner()
	f := &GHCLIDiffFetcher{Runner: runner}

	_, err := f.Fetch("not a url with enough segments", "")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestParsePRURL(t *testing.T) {
	tests := []struct {
		name       string
		url        string
		wantRepo   string
		wantNumber int
		wantErr    bool
	}{
		{
			name:       "standard github PR URL",
			url:        "https://github.com/owner/repo/pull/42",
			wantRepo:   "owner/repo",
			wantNumber: 42,
		},
		{
			name:       "API URL format",
			url:        "https://api.github.com/repos/owner/repo/pulls/42",
			wantRepo:   "owner/repo",
			wantNumber: 42,
		},
		{
			name:    "missing number",
			url:     "https://github.com/owner/repo/pull/",
			wantErr: true,
		},
		{
			name:    "wrong path structure",
			url:     "https://github.com/owner/repo",
			wantErr: true,
		},
		{
			name:    "unparseable URL",
			url:     "://invalid",
			wantErr: true,
		},
		{
			name:    "non-numeric PR number",
			url:     "https://github.com/owner/repo/pull/abc",
			wantErr: true,
		},
		{
			name:       "pulls plural in standard URL",
			url:        "https://github.com/owner/repo/pulls/99",
			wantRepo:   "owner/repo",
			wantNumber: 99,
		},
		{
			name:       "API URL with pull singular",
			url:        "https://api.github.com/repos/owner/repo/pull/7",
			wantRepo:   "owner/repo",
			wantNumber: 7,
		},
		{
			name:    "API URL non-numeric PR number",
			url:     "https://api.github.com/repos/owner/repo/pulls/abc",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo, number, err := parsePRURL(tt.url)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if repo != tt.wantRepo {
				t.Errorf("repo = %q, want %q", repo, tt.wantRepo)
			}
			if number != tt.wantNumber {
				t.Errorf("number = %d, want %d", number, tt.wantNumber)
			}
		})
	}
}
