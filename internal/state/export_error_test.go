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
