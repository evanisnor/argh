package api

import (
	"net/http"
	"strings"

	"github.com/evanisnor/argh/internal/eventbus"
)

// SSOInfo contains the parsed details from a GitHub SSO 403 response.
type SSOInfo struct {
	OrgName          string
	AuthorizationURL string
}

// SSOObserver is notified when a GitHub API response indicates SSO authorization
// is required.
type SSOObserver interface {
	OnSSORequired(info SSOInfo)
}

// BusSSOObserver publishes SSORequired events to the event bus when an SSO 403
// response is detected.
type BusSSOObserver struct {
	Bus Publisher
}

// OnSSORequired publishes an SSORequired event with the SSOInfo as payload.
func (b *BusSSOObserver) OnSSORequired(info SSOInfo) {
	b.Bus.Publish(eventbus.Event{
		Type:  eventbus.SSORequired,
		After: info,
	})
}

// ssoTransport wraps a base http.RoundTripper and inspects every response for
// GitHub SSO 403 responses (status 403 with X-GitHub-SSO header).
type ssoTransport struct {
	base     http.RoundTripper
	observer SSOObserver
}

// NewSSOTransport returns an http.RoundTripper that wraps base and calls
// observer.OnSSORequired when a 403 response includes an X-GitHub-SSO header.
// The original response is always returned unchanged.
func NewSSOTransport(base http.RoundTripper, observer SSOObserver) http.RoundTripper {
	return &ssoTransport{base: base, observer: observer}
}

func (t *ssoTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode == http.StatusForbidden {
		if header := resp.Header.Get("X-GitHub-SSO"); header != "" {
			if info, ok := parseSSOHeader(header); ok {
				t.observer.OnSSORequired(info)
			}
		}
	}

	return resp, nil
}

// parseSSOHeader parses the X-GitHub-SSO header value.
// Format: "required; url=https://github.com/orgs/ORGNAME/sso?authorization_id=..."
func parseSSOHeader(header string) (SSOInfo, bool) {
	header = strings.TrimSpace(header)
	if !strings.HasPrefix(header, "required;") {
		return SSOInfo{}, false
	}

	rest := strings.TrimPrefix(header, "required;")
	rest = strings.TrimSpace(rest)

	if !strings.HasPrefix(rest, "url=") {
		return SSOInfo{}, false
	}

	authURL := strings.TrimPrefix(rest, "url=")
	authURL = strings.TrimSpace(authURL)
	if authURL == "" {
		return SSOInfo{}, false
	}

	orgName := extractOrgFromSSOURL(authURL)

	return SSOInfo{
		OrgName:          orgName,
		AuthorizationURL: authURL,
	}, true
}

// extractOrgFromSSOURL extracts the org name from a URL like
// https://github.com/orgs/ORGNAME/sso?authorization_id=...
func extractOrgFromSSOURL(u string) string {
	const orgsPrefix = "/orgs/"
	idx := strings.Index(u, orgsPrefix)
	if idx < 0 {
		return ""
	}
	rest := u[idx+len(orgsPrefix):]
	if slashIdx := strings.IndexByte(rest, '/'); slashIdx >= 0 {
		return rest[:slashIdx]
	}
	return rest
}
