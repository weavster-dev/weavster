package config

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestArtifactsSerializesEveryConfigKind(t *testing.T) {
	c := &Config{
		Flows: map[string]Flow{
			"admit": {
				Name:         "Patient Admit",
				Source:       Source{Type: "file"},
				Destinations: []Destination{{Name: "archive", Type: "file"}},
			},
		},
		Alerts: map[string]Alert{
			"on-error": {Trigger: "processing-error", Recipients: []string{"ops@example.com"}},
		},
		Snippets: map[string]string{"patient-id": "PID.3.1"},
		Scripts:  map[string]string{"normalize": "return message"},
		Map:      map[string]string{"facility": "central"},
		Settings: map[string]any{"retryLimit": float64(3)},
	}

	artifacts := c.Artifacts()
	if len(artifacts) != 6 {
		t.Fatalf("Artifacts() returned %d entries, want 6: %v", len(artifacts), artifacts)
	}

	textCases := map[string]string{
		"snippet/patient-id": "PID.3.1",
		"script/normalize":   "return message",
		"map/facility":       "central",
	}
	for key, want := range textCases {
		if got := string(artifacts[key]); got != want {
			t.Errorf("Artifacts()[%q] = %q, want %q", key, got, want)
		}
	}

	jsonCases := map[string]any{
		"flow/admit":          c.Flows["admit"],
		"alert/on-error":      c.Alerts["on-error"],
		"settings/retryLimit": float64(3),
	}
	for key, want := range jsonCases {
		var got any
		if err := json.Unmarshal(artifacts[key], &got); err != nil {
			t.Fatalf("Artifacts()[%q] is not valid JSON: %v", key, err)
		}

		wantJSON, err := json.Marshal(want)
		if err != nil {
			t.Fatalf("marshal expected %q artifact: %v", key, err)
		}
		var normalizedWant any
		if err := json.Unmarshal(wantJSON, &normalizedWant); err != nil {
			t.Fatalf("normalize expected %q artifact: %v", key, err)
		}
		if !reflect.DeepEqual(got, normalizedWant) {
			t.Errorf("Artifacts()[%q] = %#v, want %#v", key, got, normalizedWant)
		}
	}
}
