package api

import (
	"net/http"

	"github.com/evanisnor/argh/internal/persistence"
)

// ETagStore is the persistence interface required by ETagTransport.
type ETagStore interface {
	GetETag(url string) (persistence.ETag, error)
	UpsertETag(e persistence.ETag) error
}

// ETagTransport is an http.RoundTripper that adds ETag-based conditional
// request headers and stores response ETags in the database.
//
// On each request:
//   - If a stored ETag or Last-Modified exists for the URL, the corresponding
//     If-None-Match / If-Modified-Since headers are added.
//   - On a 200 response that carries an ETag or Last-Modified header, those
//     values are persisted for use in the next request.
//   - On a 304 Not Modified response the response is returned as-is and the
//     store is not updated.  The caller is responsible for skipping DB writes
//     when it receives a 304.
type ETagTransport struct {
	inner http.RoundTripper
	store ETagStore
}

// NewETagTransport returns a new ETagTransport wrapping inner.
// If inner is nil, http.DefaultTransport is used.
func NewETagTransport(inner http.RoundTripper, store ETagStore) *ETagTransport {
	if inner == nil {
		inner = http.DefaultTransport
	}
	return &ETagTransport{inner: inner, store: store}
}

// RoundTrip adds conditional request headers and persists response ETags.
func (t *ETagTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Clone the request so we do not mutate the caller's headers.
	req = req.Clone(req.Context())

	// Look up any stored ETag for this URL and add conditional headers.
	// Any error (including sql.ErrNoRows) simply means no cached entry —
	// the request proceeds unconditionally.
	if stored, err := t.store.GetETag(req.URL.String()); err == nil {
		if stored.ETag != "" {
			req.Header.Set("If-None-Match", stored.ETag)
		}
		if stored.LastModified != "" {
			req.Header.Set("If-Modified-Since", stored.LastModified)
		}
	}

	resp, err := t.inner.RoundTrip(req)
	if err != nil {
		return nil, err
	}

	// On a full (200) response, persist ETag / Last-Modified for next time.
	if resp.StatusCode == http.StatusOK {
		etag := resp.Header.Get("ETag")
		lastModified := resp.Header.Get("Last-Modified")
		if etag != "" || lastModified != "" {
			// Best-effort: a caching failure is not fatal.
			_ = t.store.UpsertETag(persistence.ETag{
				URL:          req.URL.String(),
				ETag:         etag,
				LastModified: lastModified,
			})
		}
	}

	return resp, nil
}
