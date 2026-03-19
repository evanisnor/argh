package notify

import "github.com/gen2brain/beeep"

// BeeepSender dispatches macOS Notification Center alerts via gen2brain/beeep.
// This is the production Sender implementation; tests inject a stub instead.
// The notifyFn field can be replaced in tests to avoid OS calls.
type BeeepSender struct {
	notifyFn func(title, message string, icon any) error
}

// NewBeeepSender returns a BeeepSender backed by the real beeep.Notify call.
func NewBeeepSender() BeeepSender {
	return BeeepSender{notifyFn: beeep.Notify}
}

// Notify sends a macOS system notification with the given title and body.
func (s BeeepSender) Notify(title, body string) error {
	return s.notifyFn(title, body, "")
}
