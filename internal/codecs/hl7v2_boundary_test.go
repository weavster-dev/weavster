package codecs

import (
	"strings"
	"testing"
)

func TestMSHEncodingBoundaries(t *testing.T) {
	tests := []struct {
		name string
		line string
		want string
	}{
		{name: "followed by field", line: `MSH|^~\&|sender`, want: `^~\&`},
		{name: "terminal field", line: `MSH|^~\&`, want: `^~\&`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mshEncoding(tt.line, '|'); got != tt.want {
				t.Errorf("mshEncoding(%q) = %q, want %q", tt.line, got, tt.want)
			}
		})
	}
}

func TestHL7ParseShortMSHEncodingKeepsDefaultSeparators(t *testing.T) {
	v, err := HL7v2().Parse([]byte("MSH|!|sender\rPID|A^B~C^D\r"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	msg := v.(*HL7Message)
	if len(msg.Segments) != 2 || len(msg.Segments[1].Fields) != 1 {
		t.Fatalf("parsed message = %+v, want MSH and one-field PID", msg)
	}
	got := msg.Segments[1].Fields[0]
	want := [][]string{{"A", "B"}, {"C", "D"}}
	if len(got) != len(want) {
		t.Fatalf("PID-2 repetitions = %#v, want %#v", got, want)
	}
	for i := range want {
		if strings.Join(got[i], "|") != strings.Join(want[i], "|") {
			t.Errorf("PID-2 repetition %d = %#v, want %#v", i, got[i], want[i])
		}
	}
}

func TestHL7SerializeRejectsWrongType(t *testing.T) {
	tests := []struct {
		name string
		in   any
		want string
	}{
		{name: "nil", want: "got <nil>"},
		{name: "string", in: "not a message", want: "got string"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := HL7v2().Serialize(tt.in)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Serialize(%T) error = %v, want error containing %q", tt.in, err, tt.want)
			}
			if got != nil {
				t.Errorf("Serialize(%T) = %q, want nil bytes", tt.in, got)
			}
		})
	}
}

func TestHL7SerializePreservesEmptyAndRepeatedFields(t *testing.T) {
	msg := &HL7Message{Segments: []HL7Segment{{
		Name: "PID",
		Fields: [][][]string{
			{{"1"}},
			nil,
			{{"A", "B"}, {"C", "D"}},
			{{""}},
		},
	}}}

	got, err := HL7v2().Serialize(msg)
	if err != nil {
		t.Fatalf("Serialize: %v", err)
	}
	if want := "PID|1||A^B~C^D|\r"; string(got) != want {
		t.Errorf("Serialize() = %q, want %q", got, want)
	}
}
