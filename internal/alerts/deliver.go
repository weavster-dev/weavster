package alerts

import (
	"context"
	"fmt"

	"github.com/weavster-dev/weavster/internal/notify"
)

// Handle evaluates an event and delivers matching alerts to their recipients
// via the Notifier (spec §2.7.25).
func (m *Manager) Handle(ctx context.Context, e ProcessingError) error {
	for _, a := range m.Evaluate(e) {
		if err := m.deliver(ctx, a, e); err != nil {
			return fmt.Errorf("alerts: deliver %s: %w", a.ID, err)
		}
	}
	return nil
}

func (m *Manager) deliver(ctx context.Context, a Alert, e ProcessingError) error {
	if m.notifier == nil {
		return nil
	}
	return m.notifier.Notify(ctx, notify.Notification{
		Recipients: a.Recipients,
		Subject:    fmt.Sprintf("Weavster alert: %s", a.ID),
		Body:       fmt.Sprintf("Trigger %q fired for flow %q: %s", a.Trigger, e.Flow, e.Err),
	})
}
