// Package notify implements the Notifier port with SMTP and webhook adapters.
package notify

import "context"

// Notification is the payload delivered by a Notifier.
type Notification struct {
	Recipients []string
	Subject    string
	Body       string
}

// Notifier is the port for alert delivery (arch §3.1).
type Notifier interface {
	Notify(ctx context.Context, n Notification) error
}
