package gateway

import (
	"context"
	"encoding/json"
	"testing"
)

func TestConfigApplyArtifactDispatch(t *testing.T) {
	alerts := &memAlerts{alerts: map[string]Alert{}}
	snippets := &memSnippets{
		snippets:  map[string]Snippet{},
		libraries: map[string]SnippetLibrary{},
	}
	items := memItems{}
	s := New(Config{Alerts: alerts, Snippets: snippets, Items: items})

	tests := []struct {
		kind, name string
		value      json.RawMessage
		exists     func() bool
	}{
		{"alert", "alert-a", json.RawMessage(`{"id":"alert-a"}`), func() bool { _, ok := alerts.alerts["alert-a"]; return ok }},
		{"snippet", "snippet-a", json.RawMessage(`{"name":"snippet-a"}`), func() bool { _, ok := snippets.snippets["snippet-a"]; return ok }},
		{"library", "library-a", json.RawMessage(`{"name":"library-a"}`), func() bool { _, ok := snippets.libraries["library-a"]; return ok }},
		{"script", "script-a", json.RawMessage(`"return true"`), func() bool { _, ok := items["scripts"]["script-a"]; return ok }},
		{"configmap", "config-a", json.RawMessage(`"value"`), func() bool { _, ok := items["configmap"]["config-a"]; return ok }},
		{"settings", "setting-a", json.RawMessage(`{"enabled":true}`), func() bool { _, ok := items["settings"]["setting-a"]; return ok }},
	}
	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			if err := s.putArtifact(tt.kind, tt.name, tt.value)(context.Background()); err != nil {
				t.Fatalf("putArtifact: %v", err)
			}
			if !tt.exists() {
				t.Fatal("putArtifact did not write to the expected store")
			}
			if err := s.deleteArtifact(context.Background(), tt.kind, tt.name); err != nil {
				t.Fatalf("deleteArtifact: %v", err)
			}
			if tt.exists() {
				t.Fatal("deleteArtifact did not remove from the expected store")
			}
		})
	}
}

func TestConfigApplyArtifactRejectsMalformedJSON(t *testing.T) {
	s := New(Config{
		Alerts:   &memAlerts{alerts: map[string]Alert{}},
		Snippets: &memSnippets{snippets: map[string]Snippet{}, libraries: map[string]SnippetLibrary{}},
	})
	for _, kind := range []string{"alert", "snippet", "library"} {
		t.Run(kind, func(t *testing.T) {
			if err := s.putArtifact(kind, "name", json.RawMessage(`{`))(context.Background()); err == nil {
				t.Fatal("putArtifact accepted malformed JSON")
			}
		})
	}
}
