package ghcli

import "github.com/evanisnor/argh/internal/api"

// FixedRateLimitReader implements api.RateLimitReader with a static full-quota
// response. The gh CLI manages its own rate limits, so we always report full
// quota to keep the poller at its base interval.
type FixedRateLimitReader struct{}

func (f *FixedRateLimitReader) CurrentState() api.RateLimitState {
	return api.RateLimitState{Remaining: 5000, Limit: 5000}
}
