package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/evanisnor/argh/internal/eventbus"
)

// spySSOObserver records all OnSSORequired calls.
type spySSOObserver struct {
	calls []SSOInfo
}

func (s *spySSOObserver) OnSSORequired(info SSOInfo) {
	s.calls = append(s.calls, info)
}

func TestSSOTransport_200_NoCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	spy := &spySSOObserver{}
	transport := NewSSOTransport(srv.Client().Transport, spy)
	client := &http.Client{Transport: transport}

	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp.Body.Close()

	if len(spy.calls) != 0 {
		t.Errorf("expected 0 observer calls, got %d", len(spy.calls))
	}
}

func TestSSOTransport_403_NoHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	spy := &spySSOObserver{}
	transport := NewSSOTransport(srv.Client().Transport, spy)
	client := &http.Client{Transport: transport}

	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp.Body.Close()

	if len(spy.calls) != 0 {
		t.Errorf("expected 0 observer calls for 403 without X-GitHub-SSO, got %d", len(spy.calls))
	}
}

func TestSSOTransport_403_WithValidHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-GitHub-SSO", "required; url=https://github.com/orgs/acme-corp/sso?authorization_id=abc123")
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	spy := &spySSOObserver{}
	transport := NewSSOTransport(srv.Client().Transport, spy)
	client := &http.Client{Transport: transport}

	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp.Body.Close()

	if len(spy.calls) != 1 {
		t.Fatalf("expected 1 observer call, got %d", len(spy.calls))
	}
	if spy.calls[0].OrgName != "acme-corp" {
		t.Errorf("OrgName = %q, want %q", spy.calls[0].OrgName, "acme-corp")
	}
	if spy.calls[0].AuthorizationURL != "https://github.com/orgs/acme-corp/sso?authorization_id=abc123" {
		t.Errorf("AuthorizationURL = %q, want %q", spy.calls[0].AuthorizationURL, "https://github.com/orgs/acme-corp/sso?authorization_id=abc123")
	}
}

func TestSSOTransport_403_MalformedHeader(t *testing.T) {
	tests := []struct {
		name   string
		header string
	}{
		{"empty header", ""},
		{"no required prefix", "optional; url=https://example.com"},
		{"required but no url", "required; something=else"},
		{"required with empty url", "required; url="},
		{"just required semicolon", "required;"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if tt.header != "" {
					w.Header().Set("X-GitHub-SSO", tt.header)
				}
				w.WriteHeader(http.StatusForbidden)
			}))
			defer srv.Close()

			spy := &spySSOObserver{}
			transport := NewSSOTransport(srv.Client().Transport, spy)
			client := &http.Client{Transport: transport}

			resp, err := client.Get(srv.URL)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			resp.Body.Close()

			if len(spy.calls) != 0 {
				t.Errorf("expected 0 observer calls for malformed header %q, got %d", tt.header, len(spy.calls))
			}
		})
	}
}

func TestSSOTransport_ReturnsOriginalResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-GitHub-SSO", "required; url=https://github.com/orgs/myorg/sso?authorization_id=xyz")
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("forbidden"))
	}))
	defer srv.Close()

	spy := &spySSOObserver{}
	transport := NewSSOTransport(srv.Client().Transport, spy)
	client := &http.Client{Transport: transport}

	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("StatusCode = %d, want %d", resp.StatusCode, http.StatusForbidden)
	}
}

func TestSSOTransport_TransportError(t *testing.T) {
	spy := &spySSOObserver{}
	transport := NewSSOTransport(&failTransport{err: errors.New("network down")}, spy)
	client := &http.Client{Transport: transport}

	_, err := client.Get("http://invalid")
	if err == nil {
		t.Fatal("expected error for transport failure")
	}
	if len(spy.calls) != 0 {
		t.Errorf("expected 0 observer calls on transport error, got %d", len(spy.calls))
	}
}

func TestBusSSOObserver_PublishesCorrectEvent(t *testing.T) {
	pub := &StubPublisher{}
	observer := &BusSSOObserver{Bus: pub}

	info := SSOInfo{OrgName: "acme", AuthorizationURL: "https://github.com/orgs/acme/sso?authorization_id=123"}
	observer.OnSSORequired(info)

	if len(pub.Events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(pub.Events))
	}
	if pub.Events[0].Type != eventbus.SSORequired {
		t.Errorf("event type = %q, want %q", pub.Events[0].Type, eventbus.SSORequired)
	}
	got, ok := pub.Events[0].After.(SSOInfo)
	if !ok {
		t.Fatalf("event After type = %T, want SSOInfo", pub.Events[0].After)
	}
	if got.OrgName != "acme" {
		t.Errorf("OrgName = %q, want %q", got.OrgName, "acme")
	}
	if got.AuthorizationURL != "https://github.com/orgs/acme/sso?authorization_id=123" {
		t.Errorf("AuthorizationURL = %q, want %q", got.AuthorizationURL, "https://github.com/orgs/acme/sso?authorization_id=123")
	}
}

func TestParseSSOHeader(t *testing.T) {
	tests := []struct {
		name    string
		header  string
		wantOK  bool
		wantOrg string
		wantURL string
	}{
		{
			name:    "valid header",
			header:  "required; url=https://github.com/orgs/acme-corp/sso?authorization_id=abc",
			wantOK:  true,
			wantOrg: "acme-corp",
			wantURL: "https://github.com/orgs/acme-corp/sso?authorization_id=abc",
		},
		{
			name:    "valid with whitespace",
			header:  "  required;  url=https://github.com/orgs/myorg/sso  ",
			wantOK:  true,
			wantOrg: "myorg",
			wantURL: "https://github.com/orgs/myorg/sso",
		},
		{
			name:   "missing required prefix",
			header: "optional; url=https://example.com",
			wantOK: false,
		},
		{
			name:   "missing url key",
			header: "required; href=https://example.com",
			wantOK: false,
		},
		{
			name:   "empty url value",
			header: "required; url=",
			wantOK: false,
		},
		{
			name:   "empty string",
			header: "",
			wantOK: false,
		},
		{
			name:   "required only",
			header: "required;",
			wantOK: false,
		},
		{
			name:    "url without orgs path",
			header:  "required; url=https://example.com/auth",
			wantOK:  true,
			wantOrg: "",
			wantURL: "https://example.com/auth",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, ok := parseSSOHeader(tt.header)
			if ok != tt.wantOK {
				t.Fatalf("parseSSOHeader(%q) ok = %v, want %v", tt.header, ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if info.OrgName != tt.wantOrg {
				t.Errorf("OrgName = %q, want %q", info.OrgName, tt.wantOrg)
			}
			if info.AuthorizationURL != tt.wantURL {
				t.Errorf("AuthorizationURL = %q, want %q", info.AuthorizationURL, tt.wantURL)
			}
		})
	}
}

func TestExtractOrgFromSSOURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{"standard url", "https://github.com/orgs/acme/sso?authorization_id=abc", "acme"},
		{"no orgs path", "https://example.com/auth", ""},
		{"orgs at end", "https://github.com/orgs/myorg", "myorg"},
		{"hyphenated org", "https://github.com/orgs/my-org/sso", "my-org"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractOrgFromSSOURL(tt.url); got != tt.want {
				t.Errorf("extractOrgFromSSOURL(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}
