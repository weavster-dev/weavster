package state

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

// Message archives (spec §2.6.19–20): a JSON document of complete messages
// (every content part, so queued messages can resume after an import),
// gzipped, then optionally encrypted with AES-256-GCM.

// archiveFormat names the archive document version.
const archiveFormat = "weavster-messages-v1"

// maxArchiveExpanded caps an archive's decompressed size, so a small upload
// cannot expand without bound (a gzip bomb).
var maxArchiveExpanded = 512 << 20

type archiveDocument struct {
	Format string        `json:"format"`
	Items  []archiveItem `json:"items"`
}

type archiveItem struct {
	ID          string                        `json:"id"`
	FlowID      string                        `json:"flowId"`
	Status      string                        `json:"status"`
	ContentType string                        `json:"contentType"`
	ReceivedAt  time.Time                     `json:"receivedAt"`
	UpdatedAt   time.Time                     `json:"updatedAt"`
	Raw         []byte                        `json:"raw,omitempty"`
	Processed   []byte                        `json:"processed,omitempty"`
	Transformed []byte                        `json:"transformed,omitempty"`
	Encoded     []byte                        `json:"encoded,omitempty"`
	Response    []byte                        `json:"response,omitempty"`
	Original    []byte                        `json:"original,omitempty"`
	Metadata    map[string]string             `json:"metadata,omitempty"`
	Attempts    map[string]DestinationAttempt `json:"attempts,omitempty"`
}

// ExportOptions selects what an archive holds: the messages matching Query,
// or exactly IDs when set; encrypted when Key is set (use 32 random bytes;
// the cipher key is its SHA-256).
type ExportOptions struct {
	IDs   []string
	Query Query
	Key   []byte
}

// ExportArchive writes an archive and returns how many messages it holds.
func ExportArchive(ctx context.Context, s Store, opts ExportOptions) ([]byte, int, error) {
	var msgs []Message
	var err error
	if len(opts.IDs) > 0 {
		msgs, err = collectByID(ctx, s, opts.IDs)
	} else {
		msgs, err = s.Search(ctx, opts.Query)
	}
	if err != nil {
		return nil, 0, err
	}
	doc := archiveDocument{Format: archiveFormat, Items: make([]archiveItem, 0, len(msgs))}
	for _, m := range msgs {
		doc.Items = append(doc.Items, archiveItem{
			ID: m.ID, FlowID: m.FlowID, Status: string(m.Status), ContentType: m.ContentType,
			ReceivedAt: m.ReceivedAt, UpdatedAt: m.UpdatedAt,
			Raw: m.Raw, Processed: m.Processed, Transformed: m.Transformed,
			Encoded: m.Encoded, Response: m.Response, Original: m.Original,
			Metadata: m.Metadata, Attempts: m.Attempts,
		})
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return nil, 0, err
	}
	out, err := gzipBytes(raw) // compress first: ciphertext does not compress
	if err == nil && opts.Key != nil {
		out, err = encrypt(out, opts.Key)
	}
	return out, len(msgs), err
}

func collectByID(ctx context.Context, s Store, ids []string) ([]Message, error) {
	out := make([]Message, 0, len(ids))
	for _, id := range ids {
		m, err := s.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

// ImportOptions controls restoring an archive.
type ImportOptions struct {
	// FlowID, when set, assigns every message to that flow.
	FlowID string
	// Overwrite replaces messages whose id exists; they are skipped
	// otherwise.
	Overwrite bool
	// Key decrypts an encrypted archive.
	Key []byte
	// CheckFlow, when set, is called for each flow the messages will belong
	// to, before anything is written; an error aborts the import.
	CheckFlow func(flowID string) error
	// Hold, when set, reserves a message id while it is written (against
	// the pipeline); ok false skips the message as busy.
	Hold func(id string) (release func(), ok bool)
}

// ImportResult counts what an import did.
type ImportResult struct {
	Imported int `json:"imported"`
	Skipped  int `json:"skipped"` // the id exists and Overwrite is off
	Busy     int `json:"busy"`    // being processed; left as it is
}

// Archive errors.
var (
	// ErrBadArchive: not readable (wrong key, not gzip, too large when
	// expanded, or not an archive document); wrapped with the reason.
	ErrBadArchive = errors.New("state: not a readable message archive")
	// ErrImportIncomplete: a store write failed part-way; the result counts
	// what was already written.
	ErrImportIncomplete = errors.New("state: import stopped part-way")
)

// ImportArchive restores the messages of an archive.
func ImportArchive(ctx context.Context, s Store, archive []byte, opts ImportOptions) (ImportResult, error) {
	var res ImportResult
	var err error
	if opts.Key != nil {
		if archive, err = decrypt(archive, opts.Key); err != nil {
			return res, fmt.Errorf("%w: cannot decrypt (wrong key?)", ErrBadArchive)
		}
	}
	raw, err := gunzipBytes(archive)
	if err != nil {
		return res, fmt.Errorf("%w: %w", ErrBadArchive, err)
	}
	var doc archiveDocument
	if err := json.Unmarshal(raw, &doc); err != nil || doc.Format != archiveFormat {
		return res, fmt.Errorf("%w: not a %s document", ErrBadArchive, archiveFormat)
	}
	if opts.CheckFlow != nil {
		checked := map[string]bool{}
		for _, it := range doc.Items {
			flow := it.FlowID
			if opts.FlowID != "" {
				flow = opts.FlowID
			}
			if !checked[flow] {
				checked[flow] = true
				if err := opts.CheckFlow(flow); err != nil {
					return res, err
				}
			}
		}
	}
	for _, it := range doc.Items {
		m := itemMessage(it)
		if opts.FlowID != "" {
			m.FlowID = opts.FlowID
		}
		wrote, busy, err := importOne(ctx, s, m, opts)
		switch {
		case err != nil:
			return res, fmt.Errorf("%w: message %s: %w", ErrImportIncomplete, m.ID, err)
		case busy:
			res.Busy++
		case wrote:
			res.Imported++
		default:
			res.Skipped++
		}
	}
	return res, nil
}

// importOne writes one message under its hold; it reports whether it wrote
// the message, or skipped it because the pipeline was busy with it.
func importOne(ctx context.Context, s Store, m Message, opts ImportOptions) (wrote, busy bool, err error) {
	if opts.Hold != nil {
		release, ok := opts.Hold(m.ID)
		if !ok {
			return false, true, nil
		}
		defer release()
	}
	if !opts.Overwrite {
		if _, err := s.Get(ctx, m.ID); err == nil {
			return false, false, nil
		} else if !errors.Is(err, ErrNotFound) {
			return false, false, err
		}
	}
	if err := s.Put(ctx, m); err != nil {
		return false, false, err
	}
	return true, false, nil
}

// itemMessage turns an archive item back into a message.
func itemMessage(it archiveItem) Message {
	return Message{
		ID: it.ID, FlowID: it.FlowID, Status: Status(it.Status), ContentType: it.ContentType,
		ReceivedAt: it.ReceivedAt, UpdatedAt: it.UpdatedAt,
		Raw: it.Raw, Processed: it.Processed, Transformed: it.Transformed,
		Encoded: it.Encoded, Response: it.Response, Original: it.Original,
		Metadata: it.Metadata, Attempts: it.Attempts,
	}
}

func gzipBytes(b []byte) ([]byte, error) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(b); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// gunzipBytes expands b, refusing more than maxArchiveExpanded bytes.
func gunzipBytes(b []byte) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	defer func() { _ = zr.Close() }()
	out, err := io.ReadAll(io.LimitReader(zr, int64(maxArchiveExpanded)+1))
	if err != nil {
		return nil, err
	}
	if len(out) > maxArchiveExpanded {
		return nil, fmt.Errorf("expands to more than %d MiB", maxArchiveExpanded>>20)
	}
	return out, nil
}

func encrypt(plain, key []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plain, nil), nil
}

func decrypt(ct, key []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	if len(ct) < gcm.NonceSize() {
		return nil, io.ErrUnexpectedEOF
	}
	nonce, body := ct[:gcm.NonceSize()], ct[gcm.NonceSize():]
	return gcm.Open(nil, nonce, body, nil)
}

func newGCM(key []byte) (cipher.AEAD, error) {
	k := sha256.Sum256(key)
	block, err := aes.NewCipher(k[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
