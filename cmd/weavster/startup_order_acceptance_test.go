package main

import (
	"context"
	"database/sql"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/weavster-dev/weavster/internal/state"
)

// TestMigratesBeforeTraffic: a store left at an older schema version (as
// by an earlier release) is migrated to the latest version before the
// server answers its first request, and what it held is kept.
func TestMigratesBeforeTraffic(t *testing.T) {
	ctx := context.Background()
	dataDir := t.TempDir()
	file := filepath.Join(dataDir, "weavster.db")
	db, err := sql.Open("sqlite", file)
	if err != nil {
		t.Fatal(err)
	}
	all := state.Migrations()
	older := all[:len(all)-1] // the latest migration not applied yet
	if err := state.Migrate(ctx, db, older); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO flows (id, document) VALUES ('kept', '{"id":"kept","name":"Kept"}')`); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	addr := freeAddr(t)
	cfg := writeConfig(t, "listen: {address: \""+addr+"\"}\nstore: {dialect: sqlite}\npaths: {dataDir: \""+dataDir+"\"}\n")
	stop := startCLI(t, []string{"server", "--config", cfg}, "http://"+addr+"/api/openapi.yaml")
	defer stop()

	// The first answer came after the store was migrated.
	check, err := sql.Open("sqlite", file)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = check.Close() }()
	var version int
	if err := check.QueryRow(`SELECT max(version) FROM schema_migrations`).Scan(&version); err != nil || version != all[len(all)-1].Version {
		t.Errorf("schema version %d (%v) when the server first answered, want %d", version, err, all[len(all)-1].Version)
	}
	c := apiClient{t: t, base: "http://" + addr}
	if code, body, _ := c.do(http.MethodGet, "/api/v1/flows/kept", "", basic(bootstrapAdmin, testAdminPassword)); code != http.StatusOK || !strings.Contains(body, `"name":"Kept"`) {
		t.Errorf("the flow stored before the upgrade: %d %s", code, body)
	}
}
