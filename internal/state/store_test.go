package state

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
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
	backends := map[string]Store{
		"sqlite": sqlite,
		"memory": NewMemStore(),
	}
	if pg := testPostgres(t); pg != nil {
		backends["postgres"] = pg
	}
	return backends
}

// testPostgres opens a store in a fresh schema of the PostgreSQL database
// WEAVSTER_TEST_POSTGRES_DSN names (the CI PostgreSQL job sets it), or
// returns nil when it is not set: no test needs PostgreSQL to run.
func testPostgres(t *testing.T) Store {
	t.Helper()
	dsn := testPostgresDSN(t)
	if dsn == "" {
		return nil
	}
	s, err := OpenPostgres(context.Background(), dsn, 4)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// testPostgresDSN creates an empty schema for the test and returns a DSN
// whose search_path selects it, or "" without WEAVSTER_TEST_POSTGRES_DSN.
// The schema is dropped when the test ends (after the stores it opened are
// closed, as cleanups run last-registered first).
func testPostgresDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("WEAVSTER_TEST_POSTGRES_DSN")
	if dsn == "" {
		return ""
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	schema := "t_" + hex.EncodeToString(b)
	t.Cleanup(func() {
		_, _ = admin.ExecContext(ctx, "DROP SCHEMA IF EXISTS "+schema+" CASCADE")
		_ = admin.Close()
	})
	if _, err := admin.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	return u.String()
}

// TestPostgresConcurrentMigrate: servers starting together on one empty
// database migrate it once; none fails on a table another is creating.
func TestPostgresConcurrentMigrate(t *testing.T) {
	dsn := testPostgresDSN(t)
	if dsn == "" {
		t.Skip("WEAVSTER_TEST_POSTGRES_DSN not set")
	}
	errs := make(chan error, 4)
	for i := range 4 {
		go func() {
			// A pool of one connection too: migrating must not wait for
			// a second one while it holds the lock.
			s, err := OpenPostgres(context.Background(), dsn, 1+i%2)
			if err == nil {
				err = s.Close()
			}
			errs <- err
		}()
	}
	for range 4 {
		if err := <-errs; err != nil {
			t.Error(err)
		}
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
	data, _, err := ExportArchive(ctx, s, ExportOptions{IDs: []string{"m1"}})
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	fresh := NewMemStore()
	res, err := ImportArchive(ctx, fresh, data, ImportOptions{})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Imported != 1 {
		t.Errorf("imported %d, want 1", res.Imported)
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
		"a": {Attempts: 1, LastError: "x", LastCode: "http:503", LastAttemptAt: due.Add(-time.Minute), NextAttemptAt: due},
		"b": {Attempts: 1},
	}}
	if err := s.Put(ctx, m); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(ctx, "n")
	a := got.Attempts["a"]
	if err != nil || !a.NextAttemptAt.Equal(due) || !got.Attempts["b"].NextAttemptAt.IsZero() ||
		a.LastCode != "http:503" || !a.LastAttemptAt.Equal(due.Add(-time.Minute)) || !got.Attempts["b"].LastAttemptAt.IsZero() {
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

// TestSearchIDAfter: IDAfter pages through messages in id order after a
// cursor, on every backend (no NUL bytes in the query: PostgreSQL refuses
// them).
func TestSearchIDAfter(t *testing.T) {
	ctx := context.Background()
	for name, s := range testBackends(t) {
		for _, id := range []string{"a", "b", "c", "B"} {
			_ = s.Put(ctx, Message{ID: id, FlowID: "f", Status: StatusQueued})
		}
		var seen []string
		cursor := ""
		for {
			page, err := s.Search(ctx, Query{IDAfter: cursor, Sort: "id", Limit: 2})
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			for _, m := range page {
				seen = append(seen, m.ID)
			}
			if len(page) < 2 {
				break
			}
			cursor = page[len(page)-1].ID
		}
		if fmt.Sprint(seen) != "[B a b c]" { // byte order on every backend
			t.Errorf("%s: pages %v, want [B a b c]", name, seen)
		}
	}
}
