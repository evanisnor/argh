package api

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/evanisnor/argh/internal/persistence"
)

// stubRoundTripper is a test double for http.RoundTripper.
type stubRoundTripper struct {
	roundTripFunc func(req *http.Request) (*http.Response, error)
}

func (s *stubRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return s.roundTripFunc(req)
}

func TestETagTransport_FirstRequest_NoETagSent_ResponseETagStored(t *testing.T) {
	store := NewStubETagStore() // returns sql.ErrNoRows by default

	var sentIfNoneMatch string
	inner := &stubRoundTripper{
		roundTripFunc: func(req *http.Request) (*http.Response, error) {
			sentIfNoneMatch = req.Header.Get("If-None-Match")
			h := make(http.Header)
			h.Set("ETag", `"abc123"`)
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     h,
				Body:       io.NopCloser(strings.NewReader("")),
			}, nil
		},
	}

	transport := NewETagTransport(inner, store)
	req, err := http.NewRequest(http.MethodGet, "https://api.github.com/repos/owner/repo/pulls/1", nil)
	if err != nil {
		t.Fatalf("creating request: %v", err)
	}

	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	// No If-None-Match on first request (no cached ETag).
	if sentIfNoneMatch != "" {
		t.Errorf("expected no If-None-Match header, got %q", sentIfNoneMatch)
	}

	// ETag stored after 200 response.
	if len(store.UpsertedETags) != 1 {
		t.Fatalf("expected 1 upserted ETag, got %d", len(store.UpsertedETags))
	}
	if store.UpsertedETags[0].ETag != `"abc123"` {
		t.Errorf("stored ETag = %q, want %q", store.UpsertedETags[0].ETag, `"abc123"`)
	}
}

func TestETagTransport_SecondRequest_ETagSent_304_StoreNotUpdated(t *testing.T) {
	store := NewStubETagStore()
	store.GetETagFunc = func(url string) (persistence.ETag, error) {
		return persistence.ETag{URL: url, ETag: `"abc123"`}, nil
	}

	var sentIfNoneMatch string
	inner := &stubRoundTripper{
		roundTripFunc: func(req *http.Request) (*http.Response, error) {
			sentIfNoneMatch = req.Header.Get("If-None-Match")
			return &http.Response{
				StatusCode: http.StatusNotModified,
				Header:     http.Header{},
				Body:       io.NopCloser(strings.NewReader("")),
			}, nil
		},
	}

	transport := NewETagTransport(inner, store)
	req, err := http.NewRequest(http.MethodGet, "https://api.github.com/repos/owner/repo/pulls/1", nil)
	if err != nil {
		t.Fatalf("creating request: %v", err)
	}

	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	if resp.StatusCode != http.StatusNotModified {
		t.Errorf("expected 304, got %d", resp.StatusCode)
	}

	// If-None-Match sent on second request.
	if sentIfNoneMatch != `"abc123"` {
		t.Errorf("If-None-Match = %q, want %q", sentIfNoneMatch, `"abc123"`)
	}

	// Store not updated on 304.
	if len(store.UpsertedETags) != 0 {
		t.Errorf("expected no ETag upserts on 304, got %d", len(store.UpsertedETags))
	}
}

func TestETagTransport_200NewETag_StoreUpdated(t *testing.T) {
	store := NewStubETagStore()
	store.GetETagFunc = func(url string) (persistence.ETag, error) {
		return persistence.ETag{URL: url, ETag: `"abc123"`}, nil
	}

	inner := &stubRoundTripper{
		roundTripFunc: func(req *http.Request) (*http.Response, error) {
			h := make(http.Header)
			h.Set("ETag", `"def456"`)
			h.Set("Last-Modified", "Tue, 01 Jan 2026 00:00:00 GMT")
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     h,
				Body:       io.NopCloser(strings.NewReader("")),
			}, nil
		},
	}

	transport := NewETagTransport(inner, store)
	req, err := http.NewRequest(http.MethodGet, "https://api.github.com/repos/owner/repo/pulls/1", nil)
	if err != nil {
		t.Fatalf("creating request: %v", err)
	}

	_, err = transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}

	// Store updated with new ETag and Last-Modified.
	if len(store.UpsertedETags) != 1 {
		t.Fatalf("expected 1 upserted ETag, got %d", len(store.UpsertedETags))
	}
	got := store.UpsertedETags[0]
	if got.ETag != `"def456"` {
		t.Errorf("ETag = %q, want %q", got.ETag, `"def456"`)
	}
	if got.LastModified != "Tue, 01 Jan 2026 00:00:00 GMT" {
		t.Errorf("LastModified = %q, want %q", got.LastModified, "Tue, 01 Jan 2026 00:00:00 GMT")
	}
}

func TestETagTransport_LastModifiedSent(t *testing.T) {
	store := NewStubETagStore()
	store.GetETagFunc = func(url string) (persistence.ETag, error) {
		return persistence.ETag{
			URL:          url,
			LastModified: "Mon, 01 Dec 2025 00:00:00 GMT",
		}, nil
	}

	var sentIfModifiedSince string
	inner := &stubRoundTripper{
		roundTripFunc: func(req *http.Request) (*http.Response, error) {
			sentIfModifiedSince = req.Header.Get("If-Modified-Since")
			return &http.Response{
				StatusCode: http.StatusNotModified,
				Header:     http.Header{},
				Body:       io.NopCloser(strings.NewReader("")),
			}, nil
		},
	}

	transport := NewETagTransport(inner, store)
	req, err := http.NewRequest(http.MethodGet, "https://api.github.com/repos/owner/repo/pulls/2", nil)
	if err != nil {
		t.Fatalf("creating request: %v", err)
	}

	_, err = transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}

	if sentIfModifiedSince != "Mon, 01 Dec 2025 00:00:00 GMT" {
		t.Errorf("If-Modified-Since = %q, want %q", sentIfModifiedSince, "Mon, 01 Dec 2025 00:00:00 GMT")
	}
}

func TestETagTransport_InnerTransportError_ReturnedToCallerStoreNotUpdated(t *testing.T) {
	store := NewStubETagStore()
	inner := &stubRoundTripper{
		roundTripFunc: func(req *http.Request) (*http.Response, error) {
			return nil, io.ErrUnexpectedEOF
		},
	}

	transport := NewETagTransport(inner, store)
	req, err := http.NewRequest(http.MethodGet, "https://api.github.com/repos/owner/repo/pulls/1", nil)
	if err != nil {
		t.Fatalf("creating request: %v", err)
	}

	resp, err := transport.RoundTrip(req)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if resp != nil {
		t.Errorf("expected nil response on error, got %v", resp)
	}
	if len(store.UpsertedETags) != 0 {
		t.Errorf("expected no ETag upserts on transport error, got %d", len(store.UpsertedETags))
	}
}

func TestNewETagTransport_NilInner_UsesDefaultTransport(t *testing.T) {
	store := NewStubETagStore()
	transport := NewETagTransport(nil, store)
	if transport.inner == nil {
		t.Error("expected inner transport to be set to http.DefaultTransport, got nil")
	}
}

func TestETagTransport_200NoETagHeader_StoreNotUpdated(t *testing.T) {
	store := NewStubETagStore()

	inner := &stubRoundTripper{
		roundTripFunc: func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{},
				Body:       io.NopCloser(strings.NewReader("")),
			}, nil
		},
	}

	transport := NewETagTransport(inner, store)
	req, err := http.NewRequest(http.MethodGet, "https://api.github.com/repos/owner/repo/pulls/3", nil)
	if err != nil {
		t.Fatalf("creating request: %v", err)
	}

	_, err = transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}

	// No ETag/Last-Modified in response — store not touched.
	if len(store.UpsertedETags) != 0 {
		t.Errorf("expected no ETag upserts when response has no ETag header, got %d", len(store.UpsertedETags))
	}
}
