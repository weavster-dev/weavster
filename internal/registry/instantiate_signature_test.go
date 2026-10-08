package registry

import (
	"context"
	"crypto/ed25519"
	"testing"
)

func TestInstantiateRejectsInvalidSignature(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Module)
	}{
		{"nil", func(m *Module) { m.Signature = nil }},
		{"empty", func(m *Module) { m.Signature = []byte{} }},
		{"truncated", func(m *Module) { m.Signature = m.Signature[:ed25519.SignatureSize-1] }},
		{"oversized", func(m *Module) { m.Signature = append(m.Signature, 0) }},
		{"corrupted", func(m *Module) { m.Signature[0] ^= 1 }},
		{"different digest", func(m *Module) {
			m.Wasm = []byte("replacement")
			m.Digest = Digest(m.Wasm)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pub, priv := newKey(t)
			r := New(pub, nil)
			ctx := context.Background()
			m, err := r.Add(ctx, "normalize", "1", []byte("wasm"), "yaml", "test", priv)
			if err != nil {
				t.Fatal(err)
			}
			if err := r.Promote(ctx, m.Name, m.Version); err != nil {
				t.Fatal(err)
			}
			if got, err := r.Instantiate(m.Name); err != nil || got != m {
				t.Fatalf("valid module = %v, %v; want original module", got, err)
			}

			tc.mutate(m)
			got, err := r.Instantiate(m.Name)
			const want = "registry: signature verification failed for normalize@1"
			if err == nil || err.Error() != want || got != nil {
				t.Fatalf("invalid signature = %v, %v; want nil, %q", got, err, want)
			}
			if m.State != StateActive {
				t.Fatalf("rejection changed state to %s", m.State)
			}

			m.Signature = Sign(priv, m.Digest)
			if got, err := r.Instantiate(m.Name); err != nil || got != m {
				t.Fatalf("re-signed module = %v, %v; want original module", got, err)
			}
		})
	}
}
