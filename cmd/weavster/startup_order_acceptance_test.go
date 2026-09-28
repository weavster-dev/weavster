package main

import (
	"bytes"
	"context"
	"database/sql"
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
	dataDir := t.TempDir()
	file := filepath.Join(dataDir, "weavster.db")
	db, err := sql.Open("sqlite", file)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	all := state.Migrations()
	if err := state.Migrate(ctx, db, all[:len(all)-1]); err != nil { // the latest migration not applied yet
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
	cfg.Store.Dialect, cfg.Paths.DataDir = serverconfig.DialectSQLite, dataDir
	_, closeStore, _, err := buildServerWithWorkers(ctx, slog.New(slog.NewTextHandler(io.Discard, nil)), io.Discard, cfg)
	if err != nil {
		t.Fatal(err)
	}
	check, err := sql.Open("sqlite", file+"?_pragma=busy_timeout(5000)")
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
	path := writeConfig(t, "listen: {address: \""+addr+"\"}\nstore: {dialect: sqlite}\npaths: {dataDir: \""+dataDir+"\"}\n")
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
	dataDir, in, out := t.TempDir(), t.TempDir(), t.TempDir()
	addr := freeAddr(t)
	path := writeConfig(t, "listen: {address: \""+addr+"\"}\nstore: {dialect: sqlite}\npaths: {dataDir: \""+dataDir+"\"}\n")
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
