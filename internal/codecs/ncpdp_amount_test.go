package codecs

import (
	"testing"
)

func TestParseAmountDefaultsEmptyField(t *testing.T) {
	got, err := ParseAmount("  ")
	if err != nil {
		t.Fatalf("ParseAmount returned an error: %v", err)
	}
	if got != "0.00" {
		t.Errorf("ParseAmount() = %q, want 0.00", got)
	}
}
