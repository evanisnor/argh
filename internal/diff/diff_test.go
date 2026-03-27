package diff

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ── Viewer.ShowDiff ────────────────────────────────────────────────────────────

func TestViewer_ShowDiff_InvalidRepo(t *testing.T) {
	tests := []struct {
		name string
		repo string
	}{
		{name: "no slash", repo: "invalid"},
		{name: "empty owner", repo: "/repo"},
		{name: "empty name", repo: "owner/"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := New("token", NewStubDiffFetcher(), NewStubSubprocessRunner(), NewStubBinaryLookup("/usr/bin/delta"))
			_, err := v.ShowDiff(tt.repo, 1)
			if err == nil || !strings.Contains(err.Error(), "invalid repo") {
				t.Errorf("expected invalid repo error, got: %v", err)
			}
		})
	}
}

func TestViewer_ShowDiff_FetchError(t *testing.T) {
	fetcher := NewStubDiffFetcher()
	fetcher.FetchFunc = func(url, token string) ([]byte, error) {
		return nil, errors.New("network error")
	}
	v := New("token", fetcher, NewStubSubprocessRunner(), NewStubBinaryLookup("/usr/bin/delta"))
	_, err := v.ShowDiff("owner/repo", 42)
	if err == nil || !strings.Contains(err.Error(), "network error") {
		t.Errorf("expected fetch error, got: %v", err)
	}
}

func TestViewer_ShowDiff_DeltaPresent(t *testing.T) {
	fetcher := NewStubDiffFetcher()
	runner := NewStubSubprocessRunner()
	lookup := NewStubBinaryLookup("/usr/bin/delta")

	v := New("mytoken", fetcher, runner, lookup)
	content, err := v.ShowDiff("owner/repo", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantURL := "https://api.github.com/repos/owner/repo/pulls/42"
	if fetcher.FetchURL != wantURL {
		t.Errorf("fetch URL = %q, want %q", fetcher.FetchURL, wantURL)
	}
	if fetcher.FetchToken != "mytoken" {
		t.Errorf("fetch token = %q, want %q", fetcher.FetchToken, "mytoken")
	}

	if len(runner.Calls) != 1 {
		t.Fatalf("expected 1 subprocess call, got %d", len(runner.Calls))
	}
	if runner.Calls[0].Name != "/usr/bin/delta" {
		t.Errorf("subprocess name = %q, want %q", runner.Calls[0].Name, "/usr/bin/delta")
	}
	if len(runner.Calls[0].Stdin) == 0 {
		t.Error("expected non-empty stdin passed to delta")
	}
	if content == "" {
		t.Error("expected non-empty content from delta")
	}
}

func TestViewer_ShowDiff_DeltaRunError(t *testing.T) {
	runner := NewStubSubprocessRunner()
	runner.RunFunc = func(name string, args []string, stdin []byte) ([]byte, error) {
		return nil, errors.New("delta exited with status 1")
	}
	v := New("token", NewStubDiffFetcher(), runner, NewStubBinaryLookup("/usr/bin/delta"))
	_, err := v.ShowDiff("owner/repo", 1)
	if err == nil || !strings.Contains(err.Error(), "delta exited with status 1") {
		t.Errorf("expected delta error, got: %v", err)
	}
}

func TestViewer_ShowDiff_DeltaNotFound(t *testing.T) {
	v := New("token", NewStubDiffFetcher(), NewStubSubprocessRunner(), NewStubBinaryLookup(""))

	content, err := v.ShowDiff("owner/repo", 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(content, "delta not found") {
		t.Errorf("expected install hint in content, got: %q", content)
	}
	if !strings.Contains(content, "brew install git-delta") {
		t.Errorf("expected brew install hint in content, got: %q", content)
	}
	if !strings.Contains(content, "--- a/file") {
		t.Errorf("expected raw diff in content, got: %q", content)
	}
}

// ── New ────────────────────────────────────────────────────────────────────────

func TestNew_WithNilRunnerAndLookup(t *testing.T) {
	v := New("token", NewStubDiffFetcher(), nil, nil)
	if v.run == nil {
		t.Error("expected non-nil runner after nil injection")
	}
	if v.lookup == nil {
		t.Error("expected non-nil lookup after nil injection")
	}
}

func TestNew_WithExplicitDependencies(t *testing.T) {
	runner := NewStubSubprocessRunner()
	lookup := NewStubBinaryLookup("/delta")
	v := New("tok", NewStubDiffFetcher(), runner, lookup)
	if v.run != runner {
		t.Error("expected runner to be preserved when explicitly provided")
	}
	if v.lookup != lookup {
		t.Error("expected lookup to be preserved when explicitly provided")
	}
}

// ── HTTPFetcher ────────────────────────────────────────────────────────────────

func TestHTTPFetcher_Fetch_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("Accept"), "application/vnd.github.diff"; got != want {
			t.Errorf("Accept = %q, want %q", got, want)
		}
		if got, want := r.Header.Get("Authorization"), "Bearer mytoken"; got != want {
			t.Errorf("Authorization = %q, want %q", got, want)
		}
		fmt.Fprint(w, "--- diff content ---")
	}))
	defer server.Close()

	f := NewHTTPFetcher(server.Client())
	body, err := f.Fetch(server.URL, "mytoken")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(body) != "--- diff content ---" {
		t.Errorf("body = %q, want %q", string(body), "--- diff content ---")
	}
}

func TestHTTPFetcher_Fetch_InvalidURL(t *testing.T) {
	f := NewHTTPFetcher(&http.Client{})
	_, err := f.Fetch("://invalid-url", "token")
	if err == nil || !strings.Contains(err.Error(), "creating request") {
		t.Errorf("expected request creation error, got: %v", err)
	}
}

func TestHTTPFetcher_Fetch_RequestError(t *testing.T) {
	// Use a client that always fails transport.
	f := NewHTTPFetcher(&http.Client{Transport: &alwaysFailTransport{}})
	_, err := f.Fetch("http://example.com/", "token")
	if err == nil || !strings.Contains(err.Error(), "HTTP request failed") {
		t.Errorf("expected HTTP request failure, got: %v", err)
	}
}

func TestHTTPFetcher_Fetch_Non200Status(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	f := NewHTTPFetcher(server.Client())
	_, err := f.Fetch(server.URL, "token")
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("expected 404 error, got: %v", err)
	}
}

func TestHTTPFetcher_Fetch_BodyReadError(t *testing.T) {
	f := NewHTTPFetcher(&http.Client{Transport: &failBodyTransport{}})
	_, err := f.Fetch("http://example.com/", "token")
	if err == nil || !strings.Contains(err.Error(), "reading response body") {
		t.Errorf("expected body read error, got: %v", err)
	}
}

// alwaysFailTransport rejects every request with a network error.
type alwaysFailTransport struct{}

func (t *alwaysFailTransport) RoundTrip(_ *http.Request) (*http.Response, error) {
	return nil, errors.New("connection refused")
}

// failBodyTransport returns a 200 response whose body always fails on Read.
type failBodyTransport struct{}

func (t *failBodyTransport) RoundTrip(_ *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(&failBodyReader{}),
		Header:     make(http.Header),
	}, nil
}

type failBodyReader struct{}

func (r *failBodyReader) Read(p []byte) (int, error) {
	return 0, errors.New("read failed")
}

// ── OS implementations ─────────────────────────────────────────────────────────

func TestOSRunner_Run_Success(t *testing.T) {
	r := &osRunner{}
	out, err := r.Run("/bin/echo", []string{"hello"}, []byte("stdin data"))
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !strings.Contains(string(out), "hello") {
		t.Errorf("expected captured output to contain 'hello', got: %q", string(out))
	}
}

func TestOSRunner_Run_Error(t *testing.T) {
	r := &osRunner{}
	_, err := r.Run("/bin/false", nil, nil)
	if err == nil {
		t.Error("expected error from /bin/false")
	}
}

func TestOSLookup_LookPath_Found(t *testing.T) {
	l := &osLookup{}
	path, err := l.LookPath("ls")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path == "" {
		t.Error("expected non-empty path for ls")
	}
}

func TestOSLookup_LookPath_NotFound(t *testing.T) {
	l := &osLookup{}
	_, err := l.LookPath("this-binary-definitely-does-not-exist-argh-xyz123")
	if err == nil {
		t.Error("expected error for non-existent binary")
	}
}
