package ghcli

import "testing"

func TestFixedRateLimitReader_CurrentState(t *testing.T) {
	r := &FixedRateLimitReader{}
	state := r.CurrentState()

	if state.Remaining != 5000 {
		t.Errorf("Remaining = %d, want 5000", state.Remaining)
	}
	if state.Limit != 5000 {
		t.Errorf("Limit = %d, want 5000", state.Limit)
	}
	if !state.Reset.IsZero() {
		t.Errorf("Reset = %v, want zero time", state.Reset)
	}
}
