// Package alerts implements alert definitions, trigger evaluation, and
// delivery via the Notifier port.
package alerts

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/weavster-dev/weavster/internal/notify"
)

// Alert is an alert definition (spec §2.7.24).
type Alert struct {
	ID         string   `json:"id"`
	Trigger    string   `json:"trigger"`
	Recipients []string `json:"recipients"`
	Scope      string   `json:"scope"` // "" (all), "flow:<id>", or "source:<id>"
	Enabled    bool     `json:"enabled"`
}

// ProcessingError is the event that may fire an alert (spec §2.7.24).
type ProcessingError struct {
	Flow   string
	Source string
	Err    string
}

// Manager holds alert definitions and evaluates/delivers them.
type Manager struct {
	mu       sync.Mutex
	alerts   map[string]Alert
	notifier notify.Notifier
}

// NewManager returns an alert manager using the given notifier.
func NewManager(n notify.Notifier) *Manager {
	return &Manager{alerts: make(map[string]Alert), notifier: n}
}

// Add creates or replaces an alert.
func (m *Manager) Add(a Alert) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.alerts[a.ID] = a
}

// Remove deletes an alert.
func (m *Manager) Remove(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.alerts, id)
}

// List returns all alerts.
func (m *Manager) List() []Alert {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Alert, 0, len(m.alerts))
	for _, a := range m.alerts {
		out = append(out, a)
	}
	return out
}

// Enable/Disable toggles an alert (spec §2.7.25).
func (m *Manager) Enable(id string, enabled bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.alerts[id]
	if !ok {
		return fmt.Errorf("alerts: alert %s not found", id)
	}
	a.Enabled = enabled
	m.alerts[id] = a
	return nil
}

// Test fires an alert once without a real event (spec §2.7.25).
func (m *Manager) Test(ctx context.Context, id string) error {
	m.mu.Lock()
	a, ok := m.alerts[id]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("alerts: alert %s not found", id)
	}
	return m.deliver(ctx, a, ProcessingError{Flow: "test", Err: "manual test"})
}

// Import replaces alerts from a JSON document; Export serializes them
// (spec §2.7.25).
func (m *Manager) Import(data []byte) error {
	var alerts []Alert
	if err := json.Unmarshal(data, &alerts); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.alerts = make(map[string]Alert, len(alerts))
	for _, a := range alerts {
		m.alerts[a.ID] = a
	}
	return nil
}

// Export serializes all alerts to JSON.
func (m *Manager) Export() ([]byte, error) {
	return json.MarshalIndent(m.List(), "", "  ")
}
