package state

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
)

var errArchiveStore = errors.New("archive store failure")

// failingStore fails the named operation.
type failingStore struct {
	*MemStore
	search, get, put error
}

func (s failingStore) Search(ctx context.Context, q Query) ([]Message, error) {
	if s.search != nil {
		return nil, s.search
	}
	return s.MemStore.Search(ctx, q)
}

func (s failingStore) Get(ctx context.Context, id string) (Message, error) {
	if s.get != nil {
		return Message{}, s.get
	}
	return s.MemStore.Get(ctx, id)
}

func (s failingStore) Put(ctx context.Context, m Message) error {
	if s.put != nil {
		return s.put
	}
	return s.MemStore.Put(ctx, m)
}

func fullMessage(id, flow string) Message {
	return Message{ID: id, FlowID: flow, Status: StatusQueued, ContentType: "json",
		Raw: []byte("raw-" + id), Processed: []byte("p"), Transformed: []byte("t-" + id),
		Encoded: []byte("e"), Response: []byte("r"), Original: []byte("o-" + id),
		Metadata: map[string]string{"k": "v"}, Attempts: map[string]DestinationAttempt{"d": {Attempts: 1, LastError: "down"}}}
}

// TestArchiveRoundTrip: every content part survives, so a queued message
// can resume after an import.
func TestArchiveRoundTrip(t *testing.T) {
	ctx := context.Background()
	src := NewMemStore()
	want := fullMessage("m1", "f")
	_ = src.Put(ctx, want)
	for _, key := range [][]byte{nil, []byte("0123456789abcdef0123456789abcdef")} {
		archive, n, err := ExportArchive(ctx, src, ExportOptions{Query: Query{FlowID: "f"}, Key: key})
		if err != nil || n != 1 {
			t.Fatalf("export = %d, %v", n, err)
		}
		dst := NewMemStore()
		if res, err := ImportArchive(ctx, dst, archive, ImportOptions{Key: key}); err != nil || res.Imported != 1 {
			t.Fatalf("import = %+v, %v", res, err)
		}
		got, _ := dst.Get(ctx, "m1")
		for part, pair := range map[string][2][]byte{
			"raw": {got.Raw, want.Raw}, "processed": {got.Processed, want.Processed}, "transformed": {got.Transformed, want.Transformed},
			"encoded": {got.Encoded, want.Encoded}, "response": {got.Response, want.Response}, "original": {got.Original, want.Original},
		} {
			if !bytes.Equal(pair[0], pair[1]) {
				t.Errorf("%s = %q, want %q", part, pair[0], pair[1])
			}
		}
		if got.Status != StatusQueued || got.Attempts["d"].LastError != "down" || got.Metadata["k"] != "v" {
			t.Errorf("message = %+v", got)
		}
	}
}

func TestImportArchiveOptions(t *testing.T) {
	ctx := context.Background()
	key := []byte("0123456789abcdef0123456789abcdef")
	src := NewMemStore()
	for _, id := range []string{"m1", "m2"} {
		_ = src.Put(ctx, fullMessage(id, "f"))
	}
	plain, _, _ := ExportArchive(ctx, src, ExportOptions{})
	encrypted, _, _ := ExportArchive(ctx, src, ExportOptions{IDs: []string{"m1"}, Key: key})
	errFlow := errors.New("no such flow")
	tests := []struct {
		name    string
		archive []byte
		opts    ImportOptions
		want    ImportResult
		wantErr error
	}{
		{"into an empty store", plain, ImportOptions{}, ImportResult{Imported: 2}, nil},
		{"existing ids skipped", plain, ImportOptions{}, ImportResult{Skipped: 2}, nil},
		{"overwrite and reassign", plain, ImportOptions{Overwrite: true, FlowID: "g"}, ImportResult{Imported: 2}, nil},
		{"busy messages left alone", plain, ImportOptions{Overwrite: true, FlowID: "g", Hold: func(id string) (func(), bool) { return func() {}, id != "m1" }}, ImportResult{Imported: 1, Busy: 1}, nil},
		{"unknown flow aborts before writing", plain, ImportOptions{Overwrite: true, CheckFlow: func(string) error { return errFlow }}, ImportResult{}, errFlow},
		{"encrypted with the key", encrypted, ImportOptions{Key: key, Overwrite: true}, ImportResult{Imported: 1}, nil},
		{"encrypted without the key", encrypted, ImportOptions{}, ImportResult{}, ErrBadArchive},
		{"wrong key", encrypted, ImportOptions{Key: []byte("another key of thirty-two bytes!")}, ImportResult{}, ErrBadArchive},
		{"not gzip", []byte("hello"), ImportOptions{}, ImportResult{}, ErrBadArchive},
	}
	dst := NewMemStore()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ImportArchive(ctx, dst, tt.archive, tt.opts)
			if !errors.Is(err, tt.wantErr) || got != tt.want {
				t.Errorf("ImportArchive = %+v, %v; want %+v, %v", got, err, tt.want, tt.wantErr)
			}
		})
	}
	if m2, _ := dst.Get(ctx, "m2"); m2.FlowID != "g" {
		t.Errorf("m2 flow = %q, want g", m2.FlowID)
	}
}

func TestArchiveErrors(t *testing.T) {
	ctx := context.Background()
	src := NewMemStore()
	_ = src.Put(ctx, fullMessage("m1", "f"))
	archive, _, _ := ExportArchive(ctx, src, ExportOptions{})

	if _, _, err := ExportArchive(ctx, failingStore{MemStore: src, search: errArchiveStore}, ExportOptions{}); !errors.Is(err, errArchiveStore) {
		t.Errorf("export with a failing search = %v", err)
	}
	if _, _, err := ExportArchive(ctx, failingStore{MemStore: src, get: errArchiveStore}, ExportOptions{IDs: []string{"m1"}}); !errors.Is(err, errArchiveStore) {
		t.Errorf("export with a failing get = %v", err)
	}
	res, err := ImportArchive(ctx, failingStore{MemStore: NewMemStore(), put: errArchiveStore}, archive, ImportOptions{})
	if !errors.Is(err, ErrImportIncomplete) || !errors.Is(err, errArchiveStore) || res.Imported != 0 {
		t.Errorf("import with a failing put = %+v, %v", res, err)
	}
	if _, err := ImportArchive(ctx, failingStore{MemStore: NewMemStore(), get: errArchiveStore}, archive, ImportOptions{}); !errors.Is(err, ErrImportIncomplete) {
		t.Errorf("import with a failing get = %v", err)
	}
	foreign, _ := gzipBytes([]byte(`{"format":"other"}`))
	if _, err := ImportArchive(ctx, NewMemStore(), foreign, ImportOptions{}); !errors.Is(err, ErrBadArchive) {
		t.Errorf("foreign document = %v", err)
	}
	if _, err := decrypt([]byte("short"), []byte("k")); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("decrypt(short) = %v", err)
	}
}

// TestArchiveExpansionCap: an archive expanding past the cap is refused.
func TestArchiveExpansionCap(t *testing.T) {
	old := maxArchiveExpanded
	maxArchiveExpanded = 1024
	defer func() { maxArchiveExpanded = old }()
	bomb, _ := gzipBytes(bytes.Repeat([]byte("0"), 4096))
	if _, err := ImportArchive(context.Background(), NewMemStore(), bomb, ImportOptions{}); !errors.Is(err, ErrBadArchive) {
		t.Errorf("oversized expansion = %v, want ErrBadArchive", err)
	}
}
