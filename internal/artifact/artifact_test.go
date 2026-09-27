package artifact

import (
	"strings"
	"testing"
)

func TestCheckAlert(t *testing.T) {
	ok := Alert{ID: "a", Name: "A", Trigger: AlertTrigger{Events: []string{"message.queued"}}, Actions: []AlertAction{{Type: "webhook", URL: "http://h/x"}}}
	tests := []struct {
		name   string
		change func(*Alert)
		want   string
	}{
		{"valid", func(*Alert) {}, ""},
		{"reserved id", func(a *Alert) { a.ID = "import" }, "reserved"},
		{"no name", func(a *Alert) { a.Name = "" }, "name must be"},
		{"no events", func(a *Alert) { a.Trigger.Events = nil }, "at least one of"},
		{"unknown event", func(a *Alert) { a.Trigger.Events = []string{"message.sent"} }, "unknown trigger event"},
		{"bad flow", func(a *Alert) { a.Trigger.Flows = []string{"a b"} }, "trigger.flows"},
		{"no actions", func(a *Alert) { a.Actions = nil }, "at least one action"},
		{"email without to", func(a *Alert) { a.Actions = []AlertAction{{Type: "email"}} }, `needs "to" with at least one address`},
		{"email with url", func(a *Alert) { a.Actions = []AlertAction{{Type: "email", To: []string{"a@b.c"}, URL: "http://h"}} }, `needs "to" with at least one address`},
		{"bad address", func(a *Alert) { a.Actions = []AlertAction{{Type: "email", To: []string{"nobody"}}} }, "not a plain email address"},
		{"display name", func(a *Alert) { a.Actions = []AlertAction{{Type: "email", To: []string{"Ops <ops@example.com>"}}} }, "not a plain email address"},
		{"webhook credentials", func(a *Alert) { a.Actions = []AlertAction{{Type: "webhook", URL: "https://u:p@h/x"}} }, "must not contain a user name"},
		{"200 wide characters", func(a *Alert) { a.Name = strings.Repeat("警", 200) }, ""},
		{"201 characters", func(a *Alert) { a.Name = strings.Repeat("a", 201) }, "name must be"},
		{"webhook without url", func(a *Alert) { a.Actions = []AlertAction{{Type: "webhook"}} }, `needs "url"`},
		{"webhook ftp", func(a *Alert) { a.Actions = []AlertAction{{Type: "webhook", URL: "ftp://h/x"}} }, `needs "url"`},
		{"webhook with to", func(a *Alert) { a.Actions = []AlertAction{{Type: "webhook", URL: "http://h", To: []string{"a@b.c"}}} }, `needs "url"`},
		{"unknown type", func(a *Alert) { a.Actions = []AlertAction{{Type: "sms"}} }, "type must be one of"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := ok
			tt.change(&a)
			err := CheckAlert(a)
			if (tt.want == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), tt.want)) {
				t.Errorf("checkAlert = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestCheckName(t *testing.T) {
	for name, ok := range map[string]bool{"a": true, "A-b_c.1": true, "": false, "a b": false, strings.Repeat("x", 129): false} {
		if err := CheckName(name); (err == nil) != ok {
			t.Errorf("CheckName(%q) = %v, want ok=%v", name, err, ok)
		}
	}
}

func TestKey(t *testing.T) {
	a, s, l := Alert{ID: "a"}, Snippet{Name: "s"}, SnippetLibrary{Name: "l"}
	for _, tt := range []struct {
		key   func() (*string, string)
		value string
		field string
	}{{a.Key, "a", "id"}, {s.Key, "s", "name"}, {l.Key, "l", "name"}} {
		if ref, field := tt.key(); *ref != tt.value || field != tt.field {
			t.Errorf("Key = %q %q, want %q %q", *ref, field, tt.value, tt.field)
		}
	}
}
