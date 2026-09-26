package state

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func testBackends(t *testing.T) map[string]Store {
	t.Helper()
	sqlite, err := OpenSQLite(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlite.Close() })
	return map[string]Store{
		"sqlite": sqlite,
		"memory": NewMemStore(),
	}
}

func sampleMessage() Message {
	return Message{
		ID:          "100",
		FlowID:      "flow:a",
		Status:      StatusSent,
		ContentType: "hl7v2",
		ReceivedAt:  time.Now(),
		Raw:         []byte("MSH|^~\\&|A|B|C|D|20240101120000||ADT^A01|1|P|2.5\r"),
		Transformed: []byte("transformed"),
		Metadata:    map[string]string{"patient": "123", "env": "prod"},
		Attempts:    map[string]DestinationAttempt{"tcp-1": {Attempts: 3, LastError: ""}},
	}
}

func TestStoreCRUDAndSearch(t *testing.T) {
	for name, s := range testBackends(t) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			m := sampleMessage()
			if err := s.Put(ctx, m); err != nil {
				t.Fatal(err)
			}

			got, err := s.Get(ctx, "100")
			if err != nil {
				t.Fatal(err)
			}
			if got.FlowID != "flow:a" || got.Status != StatusSent ||
				got.Metadata["patient"] != "123" || got.Attempts["tcp-1"].Attempts != 3 {
				t.Errorf("roundtrip mismatch: %+v", got)
			}

			cases := []struct {
				name string
				q    Query
				want int
			}{
				{"status", Query{Status: StatusSent}, 1},
				{"status-miss", Query{Status: StatusQueued}, 0},
				{"content-type", Query{ContentType: "hl7v2"}, 1},
				{"metadata", Query{Metadata: map[string]string{"patient": "123"}}, 1},
				{"metadata-miss", Query{Metadata: map[string]string{"patient": "999"}}, 0},
				{"attempts", Query{MinAttempts: 2, MaxAttempts: 5}, 1},
				{"attempts-miss", Query{MinAttempts: 5}, 0},
				{"id-range", Query{IDFrom: "100", IDTo: "100"}, 1},
				{"id-range-miss", Query{IDFrom: "200", IDTo: "300"}, 0},
			}
			for _, tc := range cases {
				res, err := s.Search(ctx, tc.q)
				if err != nil {
					t.Fatalf("%s: %v", tc.name, err)
				}
				if len(res) != tc.want {
					t.Errorf("%s: got %d results, want %d", tc.name, len(res), tc.want)
				}
			}

			if err := s.Delete(ctx, "100"); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Get(ctx, "100"); err != ErrNotFound {
				t.Errorf("expected ErrNotFound, got %v", err)
			}
		})
	}
}

func TestSearchSortAndPagination(t *testing.T) {
	s := NewMemStore()
	ctx := context.Background()
	base := time.Now()
	for i := 0; i < 5; i++ {
		m := Message{
			ID: string(rune('1' + i)), FlowID: "f", Status: StatusReceived,
			ReceivedAt: base.Add(time.Duration(i) * time.Minute),
		}
		if err := s.Put(ctx, m); err != nil {
			t.Fatal(err)
		}
	}

	res, err := s.Search(ctx, Query{Sort: "-id", Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 || res[0].ID != "5" {
		t.Errorf("desc id page = %+v", ids(res))
	}

	res, err = s.Search(ctx, Query{Sort: "received_at", Offset: 2, Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 || res[0].ID != "3" {
		t.Errorf("received_at offset page = %+v", ids(res))
	}
}

func ids(ms []Message) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.ID
	}
	return out
}

func TestExportImport(t *testing.T) {
	s := NewMemStore()
	ctx := context.Background()
	if err := s.Put(ctx, sampleMessage()); err != nil {
		t.Fatal(err)
	}

	archive, err := Export(ctx, s, nil, FormRaw)
	if err != nil {
		t.Fatal(err)
	}
	fresh := NewMemStore()
	n, err := Import(ctx, fresh, archive)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("imported %d, want 1", n)
	}
	got, _ := fresh.Get(ctx, "100")
	if string(got.Raw) != string(sampleMessage().Raw) {
		t.Errorf("raw content mismatch after import")
	}
}

func TestExportImportEncrypted(t *testing.T) {
	s := NewMemStore()
	ctx := context.Background()
	if err := s.Put(ctx, sampleMessage()); err != nil {
		t.Fatal(err)
	}

	enc, err := ExportEncrypted(ctx, s, nil, FormTransformed, []byte("secret-key"))
	if err != nil {
		t.Fatal(err)
	}
	fresh := NewMemStore()
	n, err := ImportEncrypted(ctx, fresh, enc, []byte("secret-key"))
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("imported %d, want 1", n)
	}
	got, _ := fresh.Get(ctx, "100")
	if string(got.Transformed) != "transformed" {
		t.Errorf("transformed content mismatch after encrypted import: %q", got.Transformed)
	}

	// Wrong key must fail.
	fresh2 := NewMemStore()
	if _, err := ImportEncrypted(ctx, fresh2, enc, []byte("wrong")); err == nil {
		t.Error("expected decrypt failure with wrong key")
	}
}

func TestMigrationsForwardOnly(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(1)
	ctx := context.Background()

	if err := Migrate(ctx, db, Migrations()); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	// Second run must be a forward-only no-op.
	if err := Migrate(ctx, db, Migrations()); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
}

func TestContentForm(t *testing.T) {
	m := Message{
		Raw:         []byte("raw"),
		Processed:   []byte("processed"),
		Transformed: []byte("transformed"),
		Encoded:     []byte("encoded"),
		Response:    []byte("response"),
		Original:    []byte("original"),
	}
	cases := []struct {
		form string
		want []byte
	}{
		{FormProcessed, m.Processed},
		{FormTransformed, m.Transformed},
		{FormEncoded, m.Encoded},
		{FormResponse, m.Response},
		{FormOriginal, m.Original},
		{"unknown", m.Raw},
	}
	for _, tc := range cases {
		got := m.ContentForm(tc.form)
		if string(got) != string(tc.want) {
			t.Errorf("ContentForm(%q) = %q, want %q", tc.form, got, tc.want)
		}
	}
}

func TestExportSpecificIDs(t *testing.T) {
	s := NewMemStore()
	ctx := context.Background()

	msgs := []Message{
		{ID: "m1", FlowID: "f1", Status: StatusReceived, ContentType: "text/plain", Raw: []byte("a"), ReceivedAt: time.Now()},
		{ID: "m2", FlowID: "f1", Status: StatusReceived, ContentType: "text/plain", Raw: []byte("b"), ReceivedAt: time.Now()},
	}
	for _, m := range msgs {
		if err := s.Put(ctx, m); err != nil {
			t.Fatal(err)
		}
	}

	// Export only m1
	data, err := Export(ctx, s, []string{"m1"}, FormOriginal)
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	fresh := NewMemStore()
	n, err := Import(ctx, fresh, data)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if n != 1 {
		t.Errorf("imported %d, want 1", n)
	}
}

func TestNextAttemptAtRoundTrip(t *testing.T) {
	ctx := context.Background()
	s, err := OpenSQLite(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	due := time.UnixMilli(time.Now().Add(time.Minute).UnixMilli())
	m := Message{ID: "n", FlowID: "f", Status: StatusQueued, Attempts: map[string]DestinationAttempt{
		"a": {Attempts: 1, LastError: "x", NextAttemptAt: due},
		"b": {Attempts: 1},
	}}
	if err := s.Put(ctx, m); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(ctx, "n")
	if err != nil || !got.Attempts["a"].NextAttemptAt.Equal(due) || !got.Attempts["b"].NextAttemptAt.IsZero() {
		t.Errorf("attempts = %+v, %v", got.Attempts, err)
	}
}

// TestSQLiteCancelReleasesFile: a statement cancelled by its context must
// not keep the database file locked after Close, or the next open of the
// same file (a restart) fails with SQLITE_BUSY.
func TestSQLiteCancelReleasesFile(t *testing.T) {
	for i := 0; i < 20; i++ {
		dsn := filepath.Join(t.TempDir(), "x.db")
		s, err := OpenSQLite(context.Background(), dsn)
		if err != nil {
			t.Fatal(err)
		}
		for j := 0; j < 20; j++ {
			if err := s.Put(context.Background(), Message{ID: fmt.Sprint(j), FlowID: "f", Status: StatusQueued, Raw: []byte("x")}); err != nil {
				t.Fatal(err)
			}
		}
		ctx, cancel := context.WithCancel(context.Background())
		go func() { time.Sleep(time.Duration(i) * 50 * time.Microsecond); cancel() }()
		for ctx.Err() == nil {
			_, _ = s.Search(ctx, Query{Status: StatusQueued, Sort: "id", Limit: 100})
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
		s2, err := OpenSQLite(context.Background(), dsn)
		if err != nil {
			t.Fatalf("iteration %d: reopen: %v", i, err)
		}
		if err := s2.Put(context.Background(), Message{ID: "z", FlowID: "f", Status: StatusQueued, Raw: []byte("x")}); err != nil {
			t.Fatalf("iteration %d: write after a cancelled query and Close: %v", i, err)
		}
		_ = s2.Close()
	}
}
