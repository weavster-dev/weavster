package executor

import (
	"context"
	"strings"
	"testing"
)

var memoryOnlyWasm = []byte{
	0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
	0x05, 0x03, 0x01, 0x00, 0x01,
	0x07, 0x0a, 0x01, 0x06, 'm', 'e', 'm', 'o', 'r', 'y', 0x02, 0x00,
}

var transformWithoutResultWasm = []byte{
	0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
	0x01, 0x06, 0x01, 0x60, 0x02, 0x7f, 0x7f, 0x00,
	0x03, 0x02, 0x01, 0x00,
	0x05, 0x03, 0x01, 0x00, 0x01,
	0x07, 0x0d, 0x01, 0x09, 't', 'r', 'a', 'n', 's', 'f', 'o', 'r', 'm', 0x00, 0x00,
	0x0a, 0x04, 0x01, 0x02, 0x00, 0x0b,
}

var transformWithOversizedResultWasm = []byte{
	0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
	0x01, 0x07, 0x01, 0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7f,
	0x03, 0x02, 0x01, 0x00,
	0x05, 0x03, 0x01, 0x00, 0x01,
	0x07, 0x0d, 0x01, 0x09, 't', 'r', 'a', 'n', 's', 'f', 'o', 'r', 'm', 0x00, 0x00,
	0x0a, 0x08, 0x01, 0x06, 0x00, 0x41, 0x80, 0x80, 0x04, 0x0b,
}

func TestTransformRejectsInvalidGuestABI(t *testing.T) {
	tests := []struct {
		name  string
		wasm  []byte
		input []byte
		want  string
	}{
		{name: "missing transform export", wasm: memoryOnlyWasm, want: "does not export transform"},
		{name: "input exceeds memory", wasm: identityWasm, input: make([]byte, 65536), want: "write input to guest memory"},
		{name: "transform returns no result", wasm: transformWithoutResultWasm, want: "returned no result"},
		{name: "result exceeds memory", wasm: transformWithOversizedResultWasm, want: "read output from guest memory"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewEngine(nil).Transform(context.Background(), Request{
				ModuleName: "invalid-abi",
				Version:    "7",
				Wasm:       tt.wasm,
				Input:      tt.input,
			})
			if err == nil {
				t.Fatal("expected malformed guest ABI to fail")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %q, want %q", err, tt.want)
			}
			if !strings.Contains(err.Error(), "invalid-abi@7") {
				t.Fatalf("error lacks module identity: %v", err)
			}
		})
	}
}
