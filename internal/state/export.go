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
	"io"
	"time"
)

// Content forms for export/import (spec §2.6.19).
const (
	FormRaw         = "raw"
	FormProcessed   = "processed"
	FormTransformed = "transformed"
	FormEncoded     = "encoded"
	FormResponse    = "response"
	FormOriginal    = "original"
)

// ContentForm returns the content bytes for a message form.
func (m Message) ContentForm(form string) []byte {
	switch form {
	case FormProcessed:
		return m.Processed
	case FormTransformed:
		return m.Transformed
	case FormEncoded:
		return m.Encoded
	case FormResponse:
		return m.Response
	case FormOriginal:
		return m.Original
	default:
		return m.Raw
	}
}

type exportDocument struct {
	Format string       `json:"format"`
	Form   string       `json:"form"`
	Items  []exportItem `json:"items"`
}

type exportItem struct {
	ID          string                        `json:"id"`
	FlowID      string                        `json:"flowId"`
	Status      string                        `json:"status"`
	ContentType string                        `json:"contentType"`
	ReceivedAt  time.Time                     `json:"receivedAt"`
	UpdatedAt   time.Time                     `json:"updatedAt"`
	Content     []byte                        `json:"content"`
	Metadata    map[string]string             `json:"metadata,omitempty"`
	Attempts    map[string]DestinationAttempt `json:"attempts,omitempty"`
}

// Export produces a gzipped archive of messages in the given content form
// (spec §2.6.19: archive + compression). ids may be empty to export all.
func Export(ctx context.Context, s Store, ids []string, form string) ([]byte, error) {
	doc, err := buildExport(ctx, s, ids, form)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	return gzipBytes(raw)
}

// Import restores messages from an archive produced by Export (spec §2.6.20).
func Import(ctx context.Context, s Store, archive []byte) (int, error) {
	raw, err := gunzipBytes(archive)
	if err != nil {
		return 0, err
	}
	return importJSON(ctx, s, raw)
}

// ExportEncrypted produces an AES-GCM-encrypted, gzipped archive.
func ExportEncrypted(ctx context.Context, s Store, ids []string, form string, key []byte) ([]byte, error) {
	doc, err := buildExport(ctx, s, ids, form)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	enc, err := encrypt(raw, key)
	if err != nil {
		return nil, err
	}
	return gzipBytes(enc)
}

// ImportEncrypted restores messages from an encrypted archive.
func ImportEncrypted(ctx context.Context, s Store, archive, key []byte) (int, error) {
	raw, err := gunzipBytes(archive)
	if err != nil {
		return 0, err
	}
	plain, err := decrypt(raw, key)
	if err != nil {
		return 0, err
	}
	return importJSON(ctx, s, plain)
}

func buildExport(ctx context.Context, s Store, ids []string, form string) (*exportDocument, error) {
	msgs, err := collectForExport(ctx, s, ids)
	if err != nil {
		return nil, err
	}
	doc := &exportDocument{Format: "weavster-export-v1", Form: form}
	for _, m := range msgs {
		doc.Items = append(doc.Items, exportItem{
			ID:          m.ID,
			FlowID:      m.FlowID,
			Status:      string(m.Status),
			ContentType: m.ContentType,
			ReceivedAt:  m.ReceivedAt,
			UpdatedAt:   m.UpdatedAt,
			Content:     m.ContentForm(form),
			Metadata:    m.Metadata,
			Attempts:    m.Attempts,
		})
	}
	return doc, nil
}

func collectForExport(ctx context.Context, s Store, ids []string) ([]Message, error) {
	if len(ids) == 0 {
		return s.Search(ctx, Query{Limit: 100000})
	}
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

func importJSON(ctx context.Context, s Store, raw []byte) (int, error) {
	var doc exportDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		return 0, err
	}
	count := 0
	for _, it := range doc.Items {
		m := Message{
			ID:          it.ID,
			FlowID:      it.FlowID,
			Status:      Status(it.Status),
			ContentType: it.ContentType,
			ReceivedAt:  it.ReceivedAt,
			UpdatedAt:   it.UpdatedAt,
			Metadata:    it.Metadata,
			Attempts:    it.Attempts,
		}
		switch doc.Form {
		case FormProcessed:
			m.Processed = it.Content
		case FormTransformed:
			m.Transformed = it.Content
		case FormEncoded:
			m.Encoded = it.Content
		case FormResponse:
			m.Response = it.Content
		case FormOriginal:
			m.Original = it.Content
		default:
			m.Raw = it.Content
		}
		if err := s.Put(ctx, m); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
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

func gunzipBytes(b []byte) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	defer func() { _ = zr.Close() }()
	return io.ReadAll(zr)
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
