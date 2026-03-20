package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// stubSleeper records sleep calls without actually sleeping.
type stubSleeper struct {
	calls []time.Duration
}

func (s *stubSleeper) Sleep(d time.Duration) {
	s.calls = append(s.calls, d)
}

func TestDeviceFlowError_Error(t *testing.T) {
	tests := []struct {
		name string
		err  DeviceFlowError
		want string
	}{
		{"with description", DeviceFlowError{Code: "access_denied", Description: "user denied"}, "access_denied: user denied"},
		{"without description", DeviceFlowError{Code: "expired_token"}, "expired_token"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRequestCode_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/login/device/code" {
			t.Errorf("path = %s, want /login/device/code", r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
			t.Errorf("Content-Type = %s, want application/x-www-form-urlencoded", ct)
		}
		if accept := r.Header.Get("Accept"); accept != "application/json" {
			t.Errorf("Accept = %s, want application/json", accept)
		}
		_ = r.ParseForm()
		if cid := r.PostFormValue("client_id"); cid != "test-client" {
			t.Errorf("client_id = %q, want %q", cid, "test-client")
		}
		if scope := r.PostFormValue("scope"); scope != "repo read:org" {
			t.Errorf("scope = %q, want %q", scope, "repo read:org")
		}
		json.NewEncoder(w).Encode(map[string]any{
			"device_code":      "dc123",
			"user_code":        "ABCD-1234",
			"verification_uri": "https://github.com/login/device",
			"expires_in":       900,
			"interval":         5,
		})
	}))
	defer srv.Close()

	client := &GitHubDeviceFlowClient{HTTP: srv.Client(), BaseURL: srv.URL}
	resp, err := client.RequestCode(context.Background(), "test-client", []string{"repo", "read:org"})
	if err != nil {
		t.Fatalf("RequestCode: %v", err)
	}
	if resp.DeviceCode != "dc123" {
		t.Errorf("DeviceCode = %q, want %q", resp.DeviceCode, "dc123")
	}
	if resp.UserCode != "ABCD-1234" {
		t.Errorf("UserCode = %q, want %q", resp.UserCode, "ABCD-1234")
	}
	if resp.VerificationURI != "https://github.com/login/device" {
		t.Errorf("VerificationURI = %q, want %q", resp.VerificationURI, "https://github.com/login/device")
	}
	if resp.ExpiresIn != 900 {
		t.Errorf("ExpiresIn = %d, want 900", resp.ExpiresIn)
	}
	if resp.Interval != 5*time.Second {
		t.Errorf("Interval = %v, want 5s", resp.Interval)
	}
}

func TestRequestCode_DefaultInterval(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"device_code":      "dc",
			"user_code":        "UC",
			"verification_uri": "https://example.com",
			"expires_in":       900,
			// no interval field
		})
	}))
	defer srv.Close()

	client := &GitHubDeviceFlowClient{HTTP: srv.Client(), BaseURL: srv.URL}
	resp, err := client.RequestCode(context.Background(), "cid", nil)
	if err != nil {
		t.Fatalf("RequestCode: %v", err)
	}
	if resp.Interval != 5*time.Second {
		t.Errorf("Interval = %v, want 5s default", resp.Interval)
	}
}

func TestRequestCode_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("server error"))
	}))
	defer srv.Close()

	client := &GitHubDeviceFlowClient{HTTP: srv.Client(), BaseURL: srv.URL}
	_, err := client.RequestCode(context.Background(), "cid", nil)
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error = %q, want to contain '500'", err.Error())
	}
}

func TestRequestCode_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"Not Found"}`))
	}))
	defer srv.Close()

	client := &GitHubDeviceFlowClient{HTTP: srv.Client(), BaseURL: srv.URL}
	_, err := client.RequestCode(context.Background(), "bad-client-id", nil)
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
	want := "GitHub OAuth app not found"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q, want to contain %q", err.Error(), want)
	}
}

func TestRequestCode_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer srv.Close()

	client := &GitHubDeviceFlowClient{HTTP: srv.Client(), BaseURL: srv.URL}
	_, err := client.RequestCode(context.Background(), "cid", nil)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestRequestCode_TransportError(t *testing.T) {
	client := &GitHubDeviceFlowClient{
		HTTP:    &http.Client{Transport: &failTransport{err: errors.New("network down")}},
		BaseURL: "http://invalid",
	}
	_, err := client.RequestCode(context.Background(), "cid", nil)
	if err == nil {
		t.Fatal("expected error for transport failure")
	}
}

func TestRequestCode_DefaultBaseURL(t *testing.T) {
	// Just verify the default base URL is set correctly without actually calling github.com.
	client := &GitHubDeviceFlowClient{HTTP: &http.Client{}}
	if got := client.baseURL(); got != "https://github.com" {
		t.Errorf("baseURL() = %q, want %q", got, "https://github.com")
	}
}

func TestPollToken_Success(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		_ = r.ParseForm()
		if gt := r.PostFormValue("grant_type"); gt != "urn:ietf:params:oauth:grant-type:device_code" {
			t.Errorf("grant_type = %q", gt)
		}
		if n <= 2 {
			json.NewEncoder(w).Encode(map[string]string{
				"error":             "authorization_pending",
				"error_description": "waiting",
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]string{
			"access_token": "gho_abc123",
			"token_type":   "bearer",
			"scope":        "repo",
		})
	}))
	defer srv.Close()

	sleeper := &stubSleeper{}
	client := &GitHubDeviceFlowClient{HTTP: srv.Client(), BaseURL: srv.URL, Sleeper: sleeper}
	resp, err := client.PollToken(context.Background(), "cid", "dc123", 5*time.Second)
	if err != nil {
		t.Fatalf("PollToken: %v", err)
	}
	if resp.AccessToken != "gho_abc123" {
		t.Errorf("AccessToken = %q, want %q", resp.AccessToken, "gho_abc123")
	}
	if resp.TokenType != "bearer" {
		t.Errorf("TokenType = %q, want %q", resp.TokenType, "bearer")
	}
	if resp.Scope != "repo" {
		t.Errorf("Scope = %q, want %q", resp.Scope, "repo")
	}
	if int(calls.Load()) != 3 {
		t.Errorf("expected 3 poll calls, got %d", calls.Load())
	}
	// All sleep calls should be at 5s (no slow_down).
	for i, d := range sleeper.calls {
		if d != 5*time.Second {
			t.Errorf("sleep[%d] = %v, want 5s", i, d)
		}
	}
}

func TestPollToken_SlowDown(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			json.NewEncoder(w).Encode(map[string]string{
				"error": "slow_down",
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]string{
			"access_token": "token",
			"token_type":   "bearer",
		})
	}))
	defer srv.Close()

	sleeper := &stubSleeper{}
	client := &GitHubDeviceFlowClient{HTTP: srv.Client(), BaseURL: srv.URL, Sleeper: sleeper}
	_, err := client.PollToken(context.Background(), "cid", "dc", 5*time.Second)
	if err != nil {
		t.Fatalf("PollToken: %v", err)
	}
	if len(sleeper.calls) != 2 {
		t.Fatalf("expected 2 sleep calls, got %d", len(sleeper.calls))
	}
	if sleeper.calls[0] != 5*time.Second {
		t.Errorf("first sleep = %v, want 5s", sleeper.calls[0])
	}
	if sleeper.calls[1] != 10*time.Second {
		t.Errorf("second sleep = %v, want 10s (5s + 5s slow_down)", sleeper.calls[1])
	}
}

func TestPollToken_ExpiredToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{
			"error":             "expired_token",
			"error_description": "device code expired",
		})
	}))
	defer srv.Close()

	sleeper := &stubSleeper{}
	client := &GitHubDeviceFlowClient{HTTP: srv.Client(), BaseURL: srv.URL, Sleeper: sleeper}
	_, err := client.PollToken(context.Background(), "cid", "dc", 5*time.Second)
	if err == nil {
		t.Fatal("expected error for expired_token")
	}
	var dfe *DeviceFlowError
	if !errors.As(err, &dfe) {
		t.Fatalf("error type = %T, want *DeviceFlowError", err)
	}
	if dfe.Code != "expired_token" {
		t.Errorf("error code = %q, want expired_token", dfe.Code)
	}
}

func TestPollToken_AccessDenied(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{
			"error":             "access_denied",
			"error_description": "user denied",
		})
	}))
	defer srv.Close()

	sleeper := &stubSleeper{}
	client := &GitHubDeviceFlowClient{HTTP: srv.Client(), BaseURL: srv.URL, Sleeper: sleeper}
	_, err := client.PollToken(context.Background(), "cid", "dc", 5*time.Second)
	if err == nil {
		t.Fatal("expected error for access_denied")
	}
	var dfe *DeviceFlowError
	if !errors.As(err, &dfe) {
		t.Fatalf("error type = %T, want *DeviceFlowError", err)
	}
	if dfe.Code != "access_denied" {
		t.Errorf("error code = %q, want access_denied", dfe.Code)
	}
}

func TestPollToken_UnknownError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{
			"error": "server_error",
		})
	}))
	defer srv.Close()

	sleeper := &stubSleeper{}
	client := &GitHubDeviceFlowClient{HTTP: srv.Client(), BaseURL: srv.URL, Sleeper: sleeper}
	_, err := client.PollToken(context.Background(), "cid", "dc", 5*time.Second)
	if err == nil {
		t.Fatal("expected error for unknown error code")
	}
}

func TestPollToken_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("server error"))
	}))
	defer srv.Close()

	sleeper := &stubSleeper{}
	client := &GitHubDeviceFlowClient{HTTP: srv.Client(), BaseURL: srv.URL, Sleeper: sleeper}
	_, err := client.PollToken(context.Background(), "cid", "dc", 5*time.Second)
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestPollToken_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer srv.Close()

	sleeper := &stubSleeper{}
	client := &GitHubDeviceFlowClient{HTTP: srv.Client(), BaseURL: srv.URL, Sleeper: sleeper}
	_, err := client.PollToken(context.Background(), "cid", "dc", 5*time.Second)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestPollToken_CancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	sleeper := &stubSleeper{}
	client := &GitHubDeviceFlowClient{
		HTTP:    &http.Client{},
		BaseURL: "http://invalid",
		Sleeper: sleeper,
	}
	_, err := client.PollToken(ctx, "cid", "dc", 5*time.Second)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestPollToken_DefaultInterval(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{
			"access_token": "token",
			"token_type":   "bearer",
		})
	}))
	defer srv.Close()

	sleeper := &stubSleeper{}
	client := &GitHubDeviceFlowClient{HTTP: srv.Client(), BaseURL: srv.URL, Sleeper: sleeper}
	_, err := client.PollToken(context.Background(), "cid", "dc", 0) // 0 → default 5s
	if err != nil {
		t.Fatalf("PollToken: %v", err)
	}
	if len(sleeper.calls) != 1 || sleeper.calls[0] != 5*time.Second {
		t.Errorf("sleep calls = %v, want [5s]", sleeper.calls)
	}
}

func TestPollToken_TransportError(t *testing.T) {
	sleeper := &stubSleeper{}
	client := &GitHubDeviceFlowClient{
		HTTP:    &http.Client{Transport: &failTransport{err: errors.New("network down")}},
		BaseURL: "http://invalid",
		Sleeper: sleeper,
	}
	_, err := client.PollToken(context.Background(), "cid", "dc", 5*time.Second)
	if err == nil {
		t.Fatal("expected error for transport failure")
	}
}

func TestRealSleeper(t *testing.T) {
	s := RealSleeper()
	// Just verify it doesn't panic with a tiny duration.
	s.Sleep(0)
}

// failTransport is a test helper that always returns an error.
type failTransport struct {
	err error
}

func (f *failTransport) RoundTrip(_ *http.Request) (*http.Response, error) {
	return nil, f.err
}

// brokenBodyTransport returns a response with a body whose Read always errors.
type brokenBodyTransport struct{ statusCode int }

func (b *brokenBodyTransport) RoundTrip(_ *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: b.statusCode,
		Body:       io.NopCloser(&brokenReader{}),
	}, nil
}

type brokenReader struct{}

func (brokenReader) Read(_ []byte) (int, error) { return 0, errors.New("read failed") }

// cancelSleeper cancels the context during Sleep.
type cancelSleeper struct {
	cancel context.CancelFunc
}

func (s *cancelSleeper) Sleep(_ time.Duration) { s.cancel() }

// ── Stub coverage tests ───────────────────────────────────────────────────────

func TestFakeTicker_Stop(t *testing.T) {
	ft := NewFakeTicker(time.Second)
	ft.Stop() // no-op; just needs to be called
}

func TestStubDeviceFlowClient_RequestCode(t *testing.T) {
	stub := &StubDeviceFlowClient{
		RequestCodeFunc: func(_ context.Context, clientID string, scopes []string) (*DeviceCodeResponse, error) {
			return &DeviceCodeResponse{UserCode: "STUB"}, nil
		},
	}
	resp, err := stub.RequestCode(context.Background(), "cid", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.UserCode != "STUB" {
		t.Errorf("UserCode = %q, want STUB", resp.UserCode)
	}
}

func TestStubDeviceFlowClient_PollToken(t *testing.T) {
	stub := &StubDeviceFlowClient{
		PollTokenFunc: func(_ context.Context, _ string, _ string, _ time.Duration) (*TokenResponse, error) {
			return &TokenResponse{AccessToken: "tok"}, nil
		},
	}
	resp, err := stub.PollToken(context.Background(), "cid", "dc", time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.AccessToken != "tok" {
		t.Errorf("AccessToken = %q, want tok", resp.AccessToken)
	}
}

// ── Nil sleeper fallback ──────────────────────────────────────────────────────

func TestPollToken_NilSleeper_FallsBackToReal(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancelled so the first ctx.Done() check fires immediately

	client := &GitHubDeviceFlowClient{
		HTTP:    &http.Client{},
		BaseURL: "http://localhost",
		// Sleeper intentionally nil to exercise the fallback path.
	}
	_, err := client.PollToken(ctx, "cid", "dc", time.Second)
	if err == nil {
		t.Fatal("expected context cancelled error")
	}
}

// ── RequestCode error paths ───────────────────────────────────────────────────

func TestRequestCode_BadURL_CreatingRequestFails(t *testing.T) {
	client := &GitHubDeviceFlowClient{
		HTTP:    &http.Client{},
		BaseURL: "://\x7f",
	}
	_, err := client.RequestCode(context.Background(), "cid", nil)
	if err == nil {
		t.Fatal("expected error for unparseable URL")
	}
	if !strings.Contains(err.Error(), "creating request") {
		t.Errorf("error = %q, want to contain 'creating request'", err.Error())
	}
}

func TestRequestCode_ReadBodyError(t *testing.T) {
	client := &GitHubDeviceFlowClient{
		HTTP:    &http.Client{Transport: &brokenBodyTransport{statusCode: http.StatusOK}},
		BaseURL: "http://localhost",
	}
	_, err := client.RequestCode(context.Background(), "cid", nil)
	if err == nil {
		t.Fatal("expected error for broken body")
	}
	if !strings.Contains(err.Error(), "reading response") {
		t.Errorf("error = %q, want to contain 'reading response'", err.Error())
	}
}

// ── PollToken error paths ─────────────────────────────────────────────────────

func TestPollToken_ContextCancelledDuringSleep(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	client := &GitHubDeviceFlowClient{
		HTTP:    &http.Client{},
		BaseURL: "http://localhost",
		Sleeper: &cancelSleeper{cancel: cancel},
	}
	_, err := client.PollToken(ctx, "cid", "dc", time.Second)
	if err == nil {
		t.Fatal("expected context cancelled error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}
}

func TestPollToken_BadURL_CreatingRequestFails(t *testing.T) {
	client := &GitHubDeviceFlowClient{
		HTTP:    &http.Client{},
		BaseURL: "://\x7f",
		Sleeper: &stubSleeper{},
	}
	_, err := client.PollToken(context.Background(), "cid", "dc", time.Second)
	if err == nil {
		t.Fatal("expected error for unparseable URL")
	}
	if !strings.Contains(err.Error(), "creating request") {
		t.Errorf("error = %q, want to contain 'creating request'", err.Error())
	}
}

func TestPollToken_ReadBodyError(t *testing.T) {
	client := &GitHubDeviceFlowClient{
		HTTP:    &http.Client{Transport: &brokenBodyTransport{statusCode: http.StatusOK}},
		BaseURL: "http://localhost",
		Sleeper: &stubSleeper{},
	}
	_, err := client.PollToken(context.Background(), "cid", "dc", time.Second)
	if err == nil {
		t.Fatal("expected error for broken body")
	}
	if !strings.Contains(err.Error(), "reading response") {
		t.Errorf("error = %q, want to contain 'reading response'", err.Error())
	}
}

func TestPollToken_TokenDecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// No "error" field, so errResp.Error == ""; but access_token is a number,
		// causing the TokenResponse unmarshal to fail.
		w.Write([]byte(`{"access_token": 123}`))
	}))
	defer srv.Close()

	client := &GitHubDeviceFlowClient{HTTP: srv.Client(), BaseURL: srv.URL, Sleeper: &stubSleeper{}}
	_, err := client.PollToken(context.Background(), "cid", "dc", time.Second)
	if err == nil {
		t.Fatal("expected error for token decode failure")
	}
	if !strings.Contains(err.Error(), "decoding token response") {
		t.Errorf("error = %q, want to contain 'decoding token response'", err.Error())
	}
}
