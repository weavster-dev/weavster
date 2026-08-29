package state

import (
	"context"
	"errors"
	"testing"
)

var errExportStore = errors.New("export store failure")

type exportErrorStore struct {
	searchErr error
	getErr    error
	putErr    error
}

func (s exportErrorStore) Put(context.Context, Message) error { return s.putErr }
func (s exportErrorStore) Get(context.Context, string) (Message, error) {
	return Message{}, s.getErr
}
func (exportErrorStore) Delete(context.Context, string) error { return nil }
func (s exportErrorStore) Search(context.Context, Query) ([]Message, error) {
	return nil, s.searchErr
}
func (exportErrorStore) Close() error { return nil }

func TestExportEncryptedPropagatesStoreErrors(t *testing.T) {
	ctx := context.Background()

	t.Run("search all", func(t *testing.T) {
		_, err := ExportEncrypted(ctx, exportErrorStore{searchErr: errExportStore}, nil, FormRaw, []byte("key"))
		if !errors.Is(err, errExportStore) {
			t.Errorf("ExportEncrypted() error = %v, want %v", err, errExportStore)
		}
	})

	t.Run("get selected", func(t *testing.T) {
		_, err := ExportEncrypted(ctx, exportErrorStore{getErr: errExportStore}, []string{"message"}, FormRaw, []byte("key"))
		if !errors.Is(err, errExportStore) {
			t.Errorf("ExportEncrypted() error = %v, want %v", err, errExportStore)
		}
	})
}

func TestImportEncryptedPreservesAllContentForms(t *testing.T) {
	ctx := context.Background()
	key := []byte("export-key")
	message := sampleMessage()
	message.Processed = []byte("processed")
	message.Encoded = []byte("encoded")
	message.Response = []byte("response")
	message.Original = []byte("original")

	for _, form := range []string{
		FormRaw,
		FormProcessed,
		FormTransformed,
		FormEncoded,
		FormResponse,
		FormOriginal,
	} {
		t.Run(form, func(t *testing.T) {
			source := NewMemStore()
			if err := source.Put(ctx, message); err != nil {
				t.Fatalf("Put: %v", err)
			}

			archive, err := ExportEncrypted(ctx, source, []string{message.ID}, form, key)
			if err != nil {
				t.Fatalf("ExportEncrypted: %v", err)
			}

			destination := NewMemStore()
			count, err := ImportEncrypted(ctx, destination, archive, key)
			if err != nil {
				t.Fatalf("ImportEncrypted: %v", err)
			}
			if count != 1 {
				t.Fatalf("ImportEncrypted() count = %d, want 1", count)
			}

			got, err := destination.Get(ctx, message.ID)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if string(got.ContentForm(form)) != string(message.ContentForm(form)) {
				t.Errorf("%s content = %q, want %q", form, got.ContentForm(form), message.ContentForm(form))
			}
		})
	}
}

func TestImportEncryptedPropagatesStoreWriteError(t *testing.T) {
	ctx := context.Background()
	key := []byte("export-key")
	source := NewMemStore()
	if err := source.Put(ctx, sampleMessage()); err != nil {
		t.Fatalf("Put: %v", err)
	}

	archive, err := ExportEncrypted(ctx, source, nil, FormRaw, key)
	if err != nil {
		t.Fatalf("ExportEncrypted: %v", err)
	}

	count, err := ImportEncrypted(ctx, exportErrorStore{putErr: errExportStore}, archive, key)
	if !errors.Is(err, errExportStore) {
		t.Errorf("ImportEncrypted() error = %v, want %v", err, errExportStore)
	}
	if count != 0 {
		t.Errorf("ImportEncrypted() count = %d, want 0", count)
	}
}
