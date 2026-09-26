package state

import (
	"context"
	"errors"
	"io"
	"testing"
)

// TestDecryptShortCiphertext covers the len(ct) < NonceSize branch of decrypt,
// which must return io.ErrUnexpectedEOF for a ciphertext shorter than the
// AES-GCM nonce rather than panicking or returning garbage.
func TestDecryptShortCiphertext(t *testing.T) {
	key := []byte("test-key")
	if _, err := decrypt([]byte("too-short"), key); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("decrypt(short) = %v, want io.ErrUnexpectedEOF", err)
	}
}

// TestGunzipInvalidInput covers the gzip.NewReader error branch of gunzipBytes,
// which must surface an error for a non-gzip payload.
func TestGunzipInvalidInput(t *testing.T) {
	if _, err := gunzipBytes([]byte("this is not gzip")); err == nil {
		t.Error("gunzipBytes(non-gzip) = nil, want error")
	}
}

// TestImportInvalidArchive covers the Import wrapper's error propagation when
// the archive is not valid gzip (corrupt archive handling on restore).
func TestImportInvalidArchive(t *testing.T) {
	s := NewMemStore()
	n, err := Import(context.Background(), s, []byte("not-a-gzip-archive"))
	if err == nil {
		t.Error("Import(non-gzip) = nil, want error")
	}
	if n != 0 {
		t.Errorf("Import(non-gzip) imported %d, want 0", n)
	}
}

// TestImportEncryptedInvalidArchive covers the ImportEncrypted wrapper's
// gunzip error propagation for a non-gzip payload.
func TestImportEncryptedInvalidArchive(t *testing.T) {
	s := NewMemStore()
	n, err := ImportEncrypted(context.Background(), s, []byte("not-gzip"), []byte("key"))
	if err == nil {
		t.Error("ImportEncrypted(non-gzip) = nil, want error")
	}
	if n != 0 {
		t.Errorf("ImportEncrypted(non-gzip) imported %d, want 0", n)
	}
}

// TestImportEncryptedTruncatedCiphertext covers the decrypt short-ciphertext
// error path end-to-end: a valid gzip archive whose inner plaintext is shorter
// than the AES-GCM nonce must surface io.ErrUnexpectedEOF on import.
func TestImportEncryptedTruncatedCiphertext(t *testing.T) {
	archive, err := gzipBytes([]byte("short"))
	if err != nil {
		t.Fatalf("gzipBytes: %v", err)
	}
	s := NewMemStore()
	if _, err := ImportEncrypted(context.Background(), s, archive, []byte("key")); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("ImportEncrypted(truncated) = %v, want io.ErrUnexpectedEOF", err)
	}
}

func TestExportPropagatesStoreErrors(t *testing.T) {
	ctx := context.Background()

	for _, tc := range []struct {
		name  string
		store exportErrorStore
		ids   []string
	}{
		{name: "search all", store: exportErrorStore{searchErr: errExportStore}},
		{name: "get selected", store: exportErrorStore{getErr: errExportStore}, ids: []string{"message"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Export(ctx, tc.store, tc.ids, FormRaw); !errors.Is(err, errExportStore) {
				t.Errorf("Export() error = %v, want %v", err, errExportStore)
			}
		})
	}
}

func TestImportRejectsMalformedJSON(t *testing.T) {
	archive, err := gzipBytes([]byte("{"))
	if err != nil {
		t.Fatalf("gzipBytes: %v", err)
	}

	count, err := Import(context.Background(), NewMemStore(), archive)
	if err == nil {
		t.Fatal("Import(malformed JSON) error = nil, want error")
	}
	if count != 0 {
		t.Errorf("Import(malformed JSON) count = %d, want 0", count)
	}
}

func TestImportPropagatesStoreWriteError(t *testing.T) {
	source := NewMemStore()
	ctx := context.Background()
	if err := source.Put(ctx, sampleMessage()); err != nil {
		t.Fatalf("Put: %v", err)
	}
	archive, err := Export(ctx, source, nil, FormRaw)
	if err != nil {
		t.Fatalf("Export: %v", err)
	}

	count, err := Import(ctx, exportErrorStore{putErr: errExportStore}, archive)
	if !errors.Is(err, errExportStore) {
		t.Errorf("Import() error = %v, want %v", err, errExportStore)
	}
	if count != 0 {
		t.Errorf("Import() count = %d, want 0", count)
	}
}
