package executor

import (
	"context"
	"errors"
	"testing"
	"time"
)

// identityWasm exports transform(ptr, len) -> len (identity via the in-place
// output ABI: output is read from the input address).
var identityWasm = []byte{
	0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
	0x01, 0x07, 0x01, 0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7f,
	0x05, 0x03, 0x01, 0x00, 0x01,
	0x03, 0x02, 0x01, 0x00,
	0x07, 0x0d, 0x01, 0x09, 't', 'r', 'a', 'n', 's', 'f', 'o', 'r', 'm', 0x00, 0x00,
	0x0a, 0x06, 0x01, 0x04, 0x00, 0x20, 0x01, 0x0b,
}

// spinWasm exports transform(ptr, len) that loops forever, to trigger the
// fuel/time wall-clock deadline.
var spinWasm = []byte{
	0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
	0x01, 0x07, 0x01, 0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7f,
	0x05, 0x03, 0x01, 0x00, 0x01,
	0x03, 0x02, 0x01, 0x00,
	0x07, 0x0d, 0x01, 0x09, 't', 'r', 'a', 'n', 's', 'f', 'o', 'r', 'm', 0x00, 0x00,
	0x0a, 0x0a, 0x01, 0x08, 0x00, 0x03, 0x40, 0x0c, 0x00, 0x0b, 0x00, 0x0b,
}

// hashImportWasm imports weavster.hash_sha256(i32,i32,i32)->i32 and exports
// transform(ptr,len)->i32 that calls hash_sha256(0,0,0).
var hashImportWasm = []byte{
	0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
	0x01, 0x0e, 0x02, 0x60, 0x03, 0x7f, 0x7f, 0x7f, 0x01, 0x7f, 0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7f,
	0x02, 0x18, 0x01, 0x08, 'w', 'e', 'a', 'v', 's', 't', 'e', 'r',
	0x0b, 'h', 'a', 's', 'h', '_', 's', 'h', 'a', '2', '5', '6', 0x00, 0x00,
	0x05, 0x03, 0x01, 0x00, 0x01,
	0x03, 0x02, 0x01, 0x01,
	0x07, 0x0d, 0x01, 0x09, 't', 'r', 'a', 'n', 's', 'f', 'o', 'r', 'm', 0x00, 0x01,
	0x0a, 0x0c, 0x01, 0x0a, 0x00, 0x41, 0x00, 0x41, 0x00, 0x41, 0x00, 0x10, 0x00, 0x0b,
}

func TestTransformIdentity(t *testing.T) {
	e := NewEngine(nil)
	out, err := e.Transform(context.Background(), Request{
		ModuleName: "ident", Version: "1", Wasm: identityWasm, Input: []byte("hello"),
	})
	if err != nil {
		t.Fatalf("transform: %v", err)
	}
	if string(out) != "hello" {
		t.Errorf("output = %q, want hello", out)
	}
}

func TestFuelLimit(t *testing.T) {
	e := NewEngine(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := e.Transform(ctx, Request{
		ModuleName: "spin", Version: "1", Wasm: spinWasm, Input: []byte("x"),
		Limits: Limits{Fuel: 50 * time.Millisecond},
	})
	var le *LimitError
	if !errors.As(err, &le) {
		t.Fatalf("expected LimitError, got %v", err)
	}
	if le.Limit != LimitFuel {
		t.Errorf("limit = %q, want fuel", le.Limit)
	}
	if le.Module != "spin" || le.Version != "1" || le.InputHash == "" {
		t.Errorf("structured error missing fields: %+v", le)
	}
}

func TestTimeoutLimit(t *testing.T) {
	e := NewEngine(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := e.Transform(ctx, Request{
		ModuleName: "spin", Version: "1", Wasm: spinWasm, Input: []byte("x"),
		Limits: Limits{Timeout: 50 * time.Millisecond},
	})
	var le *LimitError
	if !errors.As(err, &le) {
		t.Fatalf("expected LimitError, got %v", err)
	}
	if le.Limit != LimitTime {
		t.Errorf("limit = %q, want time", le.Limit)
	}
}

func TestLeastPrivilegeHostFunctions(t *testing.T) {
	e := NewEngine(nil)

	// Declared capability -> instantiation succeeds.
	if _, err := e.Transform(context.Background(), Request{
		ModuleName: "h", Version: "1", Wasm: hashImportWasm, Input: []byte("x"),
		Capabilities: []string{CapCrypto},
	}); err != nil {
		t.Fatalf("crypto declared should succeed: %v", err)
	}

	// Undeclared capability -> import resolution fails.
	if _, err := e.Transform(context.Background(), Request{
		ModuleName: "h", Version: "1", Wasm: hashImportWasm, Input: []byte("x"),
		Capabilities: []string{CapRoute},
	}); err == nil {
		t.Error("expected failure when crypto capability is not declared")
	}
}

func TestInputHash(t *testing.T) {
	input := []byte("abc")
	r := Request{Input: input}
	same := Request{Input: input}.InputHash()
	different := Request{Input: []byte("abd")}.InputHash()
	if r.InputHash() != same {
		t.Error("input hash must be deterministic")
	}
	if r.InputHash() == different {
		t.Error("input hash must differ")
	}
}

func TestFanOut(t *testing.T) {
	var sent []string
	errs := FanOut([]string{"a", "b", "c"}, map[string]bool{"b": true}, func(d string) error {
		sent = append(sent, d)
		return nil
	})
	if len(errs) != 0 || len(sent) != 2 || sent[0] != "a" || sent[1] != "c" {
		t.Errorf("sent = %v, errs = %v", sent, errs)
	}
}

func TestSelectResponse(t *testing.T) {
	cands := [][]byte{[]byte("ack"), []byte("nak"), []byte("ok")}
	idx, content, ok := SelectResponse(cands, func(b []byte) bool { return string(b) == "ok" })
	if !ok || idx != 2 || string(content) != "ok" {
		t.Errorf("select = (%d, %q, %v)", idx, content, ok)
	}
	if _, _, ok := SelectResponse(cands, func([]byte) bool { return false }); ok {
		t.Error("expected no selection")
	}
}
