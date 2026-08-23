package state

import (
	"context"
	"database/sql"
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
