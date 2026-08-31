package config

import "testing"

func TestValidateJSONRejectsMalformedJSON(t *testing.T) {
	if err := validateJSON([]byte(`{"flows":`)); err == nil {
		t.Fatal("expected malformed JSON to be rejected")
	}
}
