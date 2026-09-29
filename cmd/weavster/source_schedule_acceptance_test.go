package main

import (
	"database/sql"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestSourceSchedules: file and database sources poll on a schedule
// instead of an interval; a schedule that is not cron, one set together
// with pollIntervalMs, and one on an http source are refused.
func TestSourceSchedules(t *testing.T) {
	dbFile := filepath.Join(t.TempDir(), "his.db") + sqliteShared
	db, err := sql.Open("sqlite", dbFile)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(`CREATE TABLE orders (id INTEGER PRIMARY KEY, done INTEGER NOT NULL DEFAULT 0); INSERT INTO orders (id) VALUES (1)`); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WEAVSTER_DB_SCHED", dbFile)
	addr := freeAddr(t)
	cfg := writeConfig(t, "listen: {address: \""+addr+"\"}\n"+storeConfig(t))
	stop := startCLI(t, []string{"server", "--config", cfg}, "http://"+addr+"/api/openapi.yaml")
	defer stop()
	c := apiClient{t: t, base: "http://" + addr}
	admin := basic(bootstrapAdmin, testAdminPassword)
	in, fromFiles, fromRows := t.TempDir(), t.TempDir(), t.TempDir()
	createFlow(t, c, `{"id":"files","source":{"type":"file","dir":"`+in+`","schedule":"@every 1s"},"destinations":[{"name":"out","type":"file","dir":"`+fromFiles+`"}]}`)
	createFlow(t, c, `{"id":"rows","source":{"type":"database","driver":"sqlite","dsnEnv":"WEAVSTER_DB_SCHED","query":"SELECT id FROM orders WHERE done = 0","idColumn":"id",`+
		`"update":{"table":"orders","key":"id","set":{"done":"1"}},"schedule":"CRON_TZ=Europe/Berlin @every 1s"},"destinations":[{"name":"out","type":"file","dir":"`+fromRows+`"}]}`)
	old := time.Now().Add(-time.Minute)
	if err := os.WriteFile(filepath.Join(in, "a.json"), []byte(`{"a":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = os.Chtimes(filepath.Join(in, "a.json"), old, old)
	for _, dir := range []string{fromFiles, fromRows} {
		deadline := time.Now().Add(10 * time.Second)
		for {
			if entries, _ := os.ReadDir(dir); len(entries) == 1 {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s: nothing delivered on schedule", dir)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}

	for body, want := range map[string]string{
		`{"id":"x","source":{"type":"file","dir":"` + t.TempDir() + `","schedule":"every day"}}`:                      "is not a cron expression",
		`{"id":"x","source":{"type":"file","dir":"` + t.TempDir() + `","schedule":"@hourly","pollIntervalMs":1000}}`:  "flow.schema.json",
		`{"id":"x","source":{"type":"http","address":"` + freeAddr(t) + `","schedule":"@hourly"}}`:                    "flow.schema.json",
		`{"id":"x","source":{"type":"file","dir":"` + t.TempDir() + `","schedule":"CRON_TZ=Mars/Olympus 0 9 * * *"}}`: "is not a cron expression",
	} {
		if code, resp, _ := c.do(http.MethodPost, "/api/v1/flows", body, admin); code != http.StatusBadRequest || !strings.Contains(resp, want) {
			t.Errorf("%s: %d %s", body, code, resp)
		}
	}
}
