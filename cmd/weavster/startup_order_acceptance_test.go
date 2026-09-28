package main

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/weavster-dev/weavster/internal/serverconfig"
	"github.com/weavster-dev/weavster/internal/state"
)

// TestMigratesBeforeTraffic: a store left at an older schema version (as
// by an earlier release) is migrated while the server is built, before any
// listener or source exists, and what it held (a flow, a message with its
// attempt record) is kept, the new columns empty.
func TestMigratesBeforeTraffic(t *testing.T) {
	ctx := context.Background()
	dsn := postgresStoreDSN(t)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	all := state.Migrations()
	before := 0 // the store as left before the message tables last changed
	for i, m := range all {
		if m.Name == "attempt-code-and-time" {
			before = i
		}
	}
	if err := state.Migrate(ctx, db, all[:before]); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`INSERT INTO flows (id, document) VALUES ('kept', '{"id":"kept","name":"Kept"}')`,
		`INSERT INTO messages (id, flow_id, status, received_at, updated_at, raw) VALUES ('m1', 'kept', 'queued', 1, 1, 'x')`,
		`INSERT INTO message_attempts (message_id, destination, attempts, last_error, next_attempt_at) VALUES ('m1', 'out', 2, 'Service Unavailable', 0)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	_ = db.Close()

	// Building the server (store open, before any listener or worker)
	// already migrated the store.
	cfg := serverconfig.Default()
	cfg.Store.Dialect, cfg.Store.DSN = serverconfig.DialectPostgres, dsn
	_, closeStore, _, err := buildServerWithWorkers(ctx, slog.New(slog.NewTextHandler(io.Discard, nil)), io.Discard, cfg)
	if err != nil {
		t.Fatal(err)
	}
	check, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = check.Close() })
	var version int
	if err := check.QueryRow(`SELECT max(version) FROM schema_migrations`).Scan(&version); err != nil || version != all[len(all)-1].Version {
		t.Errorf("schema version %d (%v) once the server was built, want %d", version, err, all[len(all)-1].Version)
	}
	_ = closeStore()

	// The server started on the upgraded store keeps what it held.
	addr := freeAddr(t)
	path := writeConfig(t, "listen: {address: \""+addr+"\"}\nstore: {dialect: postgres, dsn: \""+dsn+"\"}\n")
	stop := startCLI(t, []string{"server", "--config", path}, "http://"+addr+"/api/openapi.yaml")
	defer stop()
	c := apiClient{t: t, base: "http://" + addr}
	admin := basic(bootstrapAdmin, testAdminPassword)
	if code, body, _ := c.do(http.MethodGet, "/api/v1/flows/kept", "", admin); code != http.StatusOK || !strings.Contains(body, `"name":"Kept"`) {
		t.Errorf("the flow stored before the upgrade: %d %s", code, body)
	}
	if code, body, _ := c.do(http.MethodGet, "/api/v1/messages/m1", "", admin); code != http.StatusOK ||
		!strings.Contains(body, `"out":{"attempts":2,"lastError":"Service Unavailable"}`) {
		t.Errorf("the message stored before the upgrade: %d %s", code, body)
	}
}

// TestPortTakenBeforeWork: when the API port is taken, the server exits 1
// before any background work starts, so a flow's file source has not taken
// a file.
func TestPortTakenBeforeWork(t *testing.T) {
	in, out := t.TempDir(), t.TempDir()
	addr := freeAddr(t)
	path := writeConfig(t, "listen: {address: \""+addr+"\"}\n"+durableStoreConfig(t))
	stop := startCLI(t, []string{"server", "--config", path}, "http://"+addr+"/api/openapi.yaml")
	createFlow(t, apiClient{t: t, base: "http://" + addr}, `{"id":"files","source":{"type":"file","dir":"`+in+`","pollIntervalMs":100},"destinations":[{"name":"out","type":"file","dir":"`+out+`"}]}`)
	stop()

	file := filepath.Join(in, "a.json")
	if err := os.WriteFile(file, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Minute)
	if err := os.Chtimes(file, old, old); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", addr) // someone else has the API port now
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	var stderr bytes.Buffer
	if code := run([]string{"server", "--config", path}, strings.NewReader(""), io.Discard, &stderr); code != 1 || !strings.Contains(stderr.String(), "address already in use") {
		t.Fatalf("exit %d: %s; want 1 with the port in use", code, stderr.String())
	}
	if _, err := os.Stat(file); err != nil {
		t.Errorf("the file source took a file although the server did not start: %v", err)
	}
}

// TestRefusesNewerSchema: a database a newer release has migrated is
// refused at startup (exit 1) with its version, the newest this release
// knows, and the release that wrote it, and nothing in it is changed; the
// migrations this release applied recorded its version.
func TestRefusesNewerSchema(t *testing.T) {
	ctx := context.Background()
	dsn := postgresStoreDSN(t)
	addr := freeAddr(t)
	path := writeConfig(t, "listen: {address: \""+addr+"\"}\nstore: {dialect: postgres, dsn: \""+dsn+"\"}\n")
	stop := startCLI(t, []string{"server", "--config", path}, "http://"+addr+"/api/openapi.yaml")
	stop()

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	latest := len(state.Migrations())
	var app string
	if err := db.QueryRowContext(ctx, `SELECT app_version FROM schema_migrations WHERE version = $1`, latest).Scan(&app); err != nil || app != version {
		t.Errorf("app_version = %q (%v), want %q", app, err, version)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO schema_migrations (version, name, app_version) VALUES ($1, 'future', '99.0.0')`, latest+1); err != nil {
		t.Fatal(err)
	}

	var stderr bytes.Buffer
	code := run([]string{"server", "--config", path}, strings.NewReader(""), io.Discard, &stderr)
	want := fmt.Sprintf("the database schema is at version %d (written by weavster 99.0.0), newer than this release supports (%d)", latest+1, latest)
	if code != 1 || !strings.Contains(stderr.String(), want) {
		t.Fatalf("exit %d: %s; want 1 with %q", code, stderr.String(), want)
	}
	if strings.Contains(stderr.String(), "giving up after") {
		t.Errorf("the refusal was retried as a connection failure: %s", stderr.String())
	}
	var n int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&n); err != nil || n != latest+1 {
		t.Errorf("schema_migrations has %d rows (%v), want %d: the database was changed", n, err, latest+1)
	}
}
