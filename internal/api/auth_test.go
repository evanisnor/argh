package api_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/evanisnor/argh/internal/api"
	"github.com/evanisnor/argh/internal/config"
)

// tokenFS is a minimal config.Filesystem for auth tests.
type tokenFS struct {
	files     map[string][]byte
	configDir string
	configErr error
}

func newTokenFS(configDir string) *tokenFS {
	return &tokenFS{
		files:     make(map[string][]byte),
		configDir: configDir,
	}
}

func (f *tokenFS) ReadFile(path string) ([]byte, error) {
	data, ok := f.files[path]
	if !ok {
		return nil, os.ErrNotExist
	}
	return data, nil
}

func (f *tokenFS) WriteFile(path string, data []byte, _ os.FileMode) error {
	f.files[path] = data
	return nil
}

func (f *tokenFS) MkdirAll(_ string, _ os.FileMode) error { return nil }

func (f *tokenFS) Remove(path string) error {
	delete(f.files, path)
	return nil
}

func (f *tokenFS) UserConfigDir() (string, error) {
	if f.configErr != nil {
		return "", f.configErr
	}
	return f.configDir, nil
}

func (f *tokenFS) setToken(token string) {
	f.files[filepath.Join(f.configDir, "argh", "token")] = []byte(token)
}

func TestGitHubTokenVerifier_Verify(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"login": "octocat", "id": 1}`))
		}))
		defer srv.Close()

		v := &api.GitHubTokenVerifier{BaseURL: srv.URL + "/"}
		login, err := v.Verify(context.Background(), "ghp_test")
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if login != "octocat" {
			t.Errorf("login: got %q, want %q", login, "octocat")
		}
	})

	t.Run("empty login", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"login": "", "id": 1}`))
		}))
		defer srv.Close()

		v := &api.GitHubTokenVerifier{BaseURL: srv.URL + "/"}
		_, err := v.Verify(context.Background(), "ghp_test")
		if err == nil {
			t.Fatal("expected error for empty login, got nil")
		}
	})

	t.Run("cancelled context", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		v := &api.GitHubTokenVerifier{}
		_, err := v.Verify(ctx, "ghp_invalid")
		if err == nil {
			t.Fatal("expected error with cancelled context, got nil")
		}
	})

	t.Run("invalid base URL", func(t *testing.T) {
		v := &api.GitHubTokenVerifier{BaseURL: "://bad"}
		_, err := v.Verify(context.Background(), "ghp_test")
		if err == nil {
			t.Fatal("expected error for invalid base URL, got nil")
		}
	})
}

func TestAuthenticate(t *testing.T) {
	ctx := context.Background()

	t.Run("token found and verification succeeds", func(t *testing.T) {
		fs := newTokenFS("/fake/config")
		fs.setToken("ghp_valid123")
		verifier := &api.StubTokenVerifier{Login: "octocat"}

		creds, err := api.Authenticate(ctx, fs, verifier)
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if creds.Token != "ghp_valid123" {
			t.Errorf("token: got %q, want %q", creds.Token, "ghp_valid123")
		}
		if creds.Login != "octocat" {
			t.Errorf("login: got %q, want %q", creds.Login, "octocat")
		}
	})

	t.Run("token not found returns ErrTokenNotFound", func(t *testing.T) {
		fs := newTokenFS("/fake/config")
		verifier := &api.StubTokenVerifier{Login: "octocat"}

		_, err := api.Authenticate(ctx, fs, verifier)
		if !errors.Is(err, config.ErrTokenNotFound) {
			t.Errorf("expected ErrTokenNotFound, got: %v", err)
		}
	})

	t.Run("empty token returns error", func(t *testing.T) {
		fs := newTokenFS("/fake/config")
		fs.setToken("  \n")
		verifier := &api.StubTokenVerifier{Login: "octocat"}

		_, err := api.Authenticate(ctx, fs, verifier)
		if err == nil {
			t.Fatal("expected error for empty token, got nil")
		}
	})

	t.Run("verification fails returns error", func(t *testing.T) {
		fs := newTokenFS("/fake/config")
		fs.setToken("ghp_bad")
		verifier := &api.StubTokenVerifier{Err: errors.New("401 Unauthorized")}

		_, err := api.Authenticate(ctx, fs, verifier)
		if err == nil {
			t.Fatal("expected error when verification fails, got nil")
		}
	})

	t.Run("verification returns empty login", func(t *testing.T) {
		fs := newTokenFS("/fake/config")
		fs.setToken("ghp_nouser")
		verifier := &api.StubTokenVerifier{Login: ""}

		creds, err := api.Authenticate(ctx, fs, verifier)
		// StubTokenVerifier returns empty login without error;
		// the real GitHubTokenVerifier would return an error, but
		// Authenticate itself doesn't validate login emptiness —
		// it trusts the verifier. So creds are returned.
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if creds.Login != "" {
			t.Errorf("login: got %q, want empty", creds.Login)
		}
	})
}
