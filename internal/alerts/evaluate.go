package alerts

import "strings"

// Evaluate returns the enabled alerts whose trigger and scope match the event
// (spec §2.7.24).
func (m *Manager) Evaluate(e ProcessingError) []Alert {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Alert
	for _, a := range m.alerts {
		if !a.Enabled {
			continue
		}
		if a.Trigger != "" && a.Trigger != "processing-error" && a.Trigger != e.Err {
			continue
		}
		if !scopeMatches(a.Scope, e) {
			continue
		}
		out = append(out, a)
	}
	return out
}

func scopeMatches(scope string, e ProcessingError) bool {
	if scope == "" {
		return true
	}
	if strings.HasPrefix(scope, "flow:") {
		return e.Flow == strings.TrimPrefix(scope, "flow:")
	}
	if strings.HasPrefix(scope, "source:") {
		return e.Source == strings.TrimPrefix(scope, "source:")
	}
	return false
}
