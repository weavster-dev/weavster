package codecs

import (
	"errors"
	"testing"
)

// TestRegistryNames verifies Names() returns every registered codec name in
// sorted order, which previously had no coverage.
func TestRegistryNames(t *testing.T) {
	r := Standard()
	names := r.Names()
	want := []string{"delimited", "hl7v2", "json", "ncpdp", "raw", "x12", "xml"}
	if len(names) != len(want) {
		t.Fatalf("Names() = %v, want %v", names, want)
	}
	for i, n := range want {
		if names[i] != n {
			t.Errorf("Names()[%d] = %q, want %q", i, names[i], n)
		}
	}

	empty := NewRegistry()
	if got := empty.Names(); len(got) != 0 {
		t.Errorf("empty registry Names() = %v, want empty", got)
	}
}

// TestCodecNames verifies the Name() identifier for every codec, and that
// Register keys entries by Name() so Get can later resolve them.
func TestCodecNames(t *testing.T) {
	cases := []struct {
		name string
		c    Codec
		want string
	}{
		{"JSON", JSON(), "json"},
		{"XML", XML(), "xml"},
		{"Raw", Raw(), "raw"},
		{"Delimited", NewDelimited(',', false), "delimited"},
		{"DICOM", DICOM(), "dicom"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.c.Name(); got != tc.want {
				t.Errorf("Name() = %q, want %q", got, tc.want)
			}
			r := NewRegistry(tc.c)
			if _, err := r.Get(tc.want); err != nil {
				t.Errorf("Get(%q) after Register = %v", tc.want, err)
			}
		})
	}
}

// TestAcknowledgeNotSupported verifies codecs without acknowledgment
// semantics consistently return ErrNotSupported; these branches previously
// had no coverage.
func TestAcknowledgeNotSupported(t *testing.T) {
	cases := []struct {
		name string
		c    Codec
	}{
		{"json", JSON()},
		{"xml", XML()},
		{"raw", Raw()},
		{"delimited", NewDelimited('|', false)},
		{"ncpdp", NCPDP()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tc.c.Acknowledge([]byte("anything")); !errors.Is(err, ErrNotSupported) {
				t.Errorf("Acknowledge() = %v, want ErrNotSupported", err)
			}
		})
	}
}

// TestDICOMAcknowledge verifies the Enterprise DICOM stub's Acknowledge path
// returns ErrEnterprise (Parse/Serialize were already covered, Acknowledge
// was not).
func TestDICOMAcknowledge(t *testing.T) {
	if _, err := DICOM().Acknowledge(nil); !errors.Is(err, ErrEnterprise) {
		t.Errorf("Acknowledge() = %v, want ErrEnterprise", err)
	}
}
