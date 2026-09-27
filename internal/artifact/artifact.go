// Package artifact defines the configuration artifacts besides flows —
// alerts, code snippets, and snippet libraries — and their validation. The
// API and the config-as-code document share these types (#107 D-43, D-46).
package artifact

import (
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"regexp"
	"slices"
	"unicode/utf8"

	"github.com/invopop/jsonschema"
)

// Alert is an alert definition (spec §2.7): which processing events of which
// flows trigger it, and whom it notifies.
type Alert struct {
	ID      string        `json:"id" yaml:"id"`
	Name    string        `json:"name" yaml:"name" jsonschema:"required,minLength=1,maxLength=200"`
	Enabled bool          `json:"enabled" yaml:"enabled"`
	Trigger AlertTrigger  `json:"trigger" yaml:"trigger" jsonschema:"required"`
	Actions []AlertAction `json:"actions" yaml:"actions" jsonschema:"required,minItems=1"`
}

// AlertTrigger selects the events an alert reacts to; no flows means every
// flow.
type AlertTrigger struct {
	Events []string `json:"events" yaml:"events" jsonschema:"required,minItems=1"`
	Flows  []string `json:"flows,omitempty" yaml:"flows,omitempty"`
}

// AlertAction is one notification: email (to) or webhook (url).
type AlertAction struct {
	Type string   `json:"type" yaml:"type" jsonschema:"required"`
	To   []string `json:"to,omitempty" yaml:"to,omitempty"`
	URL  string   `json:"url,omitempty" yaml:"url,omitempty"`
}

// Snippet is a reusable piece of code (spec §5 code snippets).
type Snippet struct {
	Name        string `json:"name" yaml:"name"`
	Library     string `json:"library,omitempty" yaml:"library,omitempty"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	Code        string `json:"code,omitempty" yaml:"code,omitempty"`
}

// SnippetLibrary groups snippets.
type SnippetLibrary struct {
	Name        string `json:"name" yaml:"name"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
}

// Key returns the field that names an artifact, and that field's name.
func (a *Alert) Key() (*string, string)          { return &a.ID, "id" }
func (s *Snippet) Key() (*string, string)        { return &s.Name, "name" }
func (l *SnippetLibrary) Key() (*string, string) { return &l.Name, "name" }

// AlertEvents are the events an alert can trigger on; AlertActionTypes are
// its notification types.
var (
	AlertEvents      = []string{"message.errored", "message.queued", "message.dead-lettered"}
	AlertActionTypes = []string{"email", "webhook"}
)

// ValidName matches an artifact, item, or flow reference name: 1–128 of
// A-Z a-z 0-9 . _ -.
var ValidName = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

// CheckName validates a name; the error is safe to show.
func CheckName(name string) error { return CheckValue("name", name) }

// CheckValue validates a name held in the field label ("name", "alert id"):
// the error names the label and is safe to show.
func CheckValue(label, name string) error {
	if !ValidName.MatchString(name) {
		return fmt.Errorf("%s %q must be 1-128 characters from A-Z a-z 0-9 . _ -", label, name)
	}
	return nil
}

// JSONSchemaExtend sets the allowed trigger events in generated schemas
// from AlertEvents, the list CheckAlert uses.
func (AlertTrigger) JSONSchemaExtend(s *jsonschema.Schema) {
	if p, ok := s.Properties.Get("events"); ok && p.Items != nil {
		p.Items.Enum = anys(AlertEvents)
	}
}

// JSONSchemaExtend sets the allowed action types from AlertActionTypes.
func (AlertAction) JSONSchemaExtend(s *jsonschema.Schema) {
	if p, ok := s.Properties.Get("type"); ok {
		p.Enum = anys(AlertActionTypes)
	}
}

func anys(list []string) []any {
	out := make([]any, len(list))
	for i, v := range list {
		out[i] = v
	}
	return out
}

// CheckAlert validates one alert definition; the error is safe to show.
func CheckAlert(a Alert) error {
	if a.ID == "import" || a.ID == "options" {
		return fmt.Errorf("id %q is reserved", a.ID)
	}
	if n := utf8.RuneCountInString(a.Name); n == 0 || n > 200 {
		return fmt.Errorf("alert %s: name must be 1-200 characters", a.ID)
	}
	if len(a.Trigger.Events) == 0 {
		return fmt.Errorf("alert %s: trigger.events needs at least one of %v", a.ID, AlertEvents)
	}
	for _, e := range a.Trigger.Events {
		if !slices.Contains(AlertEvents, e) {
			return fmt.Errorf("alert %s: unknown trigger event %q; use %v", a.ID, e, AlertEvents)
		}
	}
	for _, f := range a.Trigger.Flows {
		if err := CheckName(f); err != nil {
			return fmt.Errorf("alert %s: trigger.flows: %w", a.ID, err)
		}
	}
	if len(a.Actions) == 0 {
		return fmt.Errorf("alert %s: actions needs at least one action", a.ID)
	}
	for i, act := range a.Actions {
		if err := checkAlertAction(act); err != nil {
			return fmt.Errorf("alert %s: actions[%d]: %w", a.ID, i, err)
		}
	}
	return nil
}

func checkAlertAction(act AlertAction) error {
	switch act.Type {
	case "email":
		if len(act.To) == 0 || act.URL != "" {
			return errors.New(`an email action needs "to" with at least one address, and no "url"`)
		}
		for _, addr := range act.To {
			// A bare address only: no display name ("Ops <ops@example.com>").
			if parsed, err := mail.ParseAddress(addr); err != nil || parsed.Name != "" || parsed.Address != addr {
				return fmt.Errorf("%q is not a plain email address (like ops@example.com)", addr)
			}
		}
	case "webhook":
		u, err := url.Parse(act.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || len(act.To) != 0 {
			return errors.New(`a webhook action needs "url" (an http or https URL), and no "to"`)
		}
		if u.User != nil {
			return errors.New("a webhook url must not contain a user name or password; they would be shown to everyone who can read alerts")
		}
	default:
		return fmt.Errorf("type must be one of %v", AlertActionTypes)
	}
	return nil
}
