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

// TestDatabaseSource: a started flow with a database source turns each row
// its query returns into a message and marks it with update; a new row is
// picked up by the next poll and marked rows are not read again; an unset
// variable is reported; invalid sources are refused.
func TestDatabaseSource(t *testing.T) {
	dbFile := filepath.Join(t.TempDir(), "his.db") + sqliteShared
	db, err := sql.Open("sqlite", dbFile)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(`CREATE TABLE orders (id INTEGER PRIMARY KEY, mrn TEXT, amount REAL, exported INTEGER NOT NULL DEFAULT 0);
		INSERT INTO orders (id, mrn, amount) VALUES (1, 'A-1', 5.5), (2, 'O''Brien', NULL);`); err != nil {
		t.Fatal(err)
	}
	// The server gets the plain path: its connections wait for the test's
	// lock by themselves (sqliteWaits).
	t.Setenv("WEAVSTER_DB_HIS", strings.TrimSuffix(dbFile, sqliteShared))
	addr := freeAddr(t)
	cfg := writeConfig(t, "listen: {address: \""+addr+"\"}\n"+storeConfig(t))
	stop := startCLI(t, []string{"server", "--config", cfg}, "http://"+addr+"/api/openapi.yaml")
	defer stop()
	c := apiClient{t: t, base: "http://" + addr}
	admin := basic(bootstrapAdmin, testAdminPassword)
	marked := t.TempDir()
	createFlow(t, c, `{"id":"orders","source":{"type":"database","driver":"sqlite","dsnEnv":"WEAVSTER_DB_HIS",`+
		`"query":"SELECT id, mrn, amount FROM orders WHERE exported = 0 ORDER BY id","idColumn":"id",`+
		`"update":{"table":"orders","key":"id","set":{"exported":"1"}},"pollIntervalMs":1000},`+
		`"destinations":[{"name":"out","type":"file","dir":"`+marked+`"}]}`)

	waitFiles := func(dir string, n int) []string {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for {
			entries, _ := os.ReadDir(dir)
			if len(entries) >= n {
				var bodies []string
				for _, e := range entries {
					b, _ := os.ReadFile(filepath.Join(dir, e.Name()))
					bodies = append(bodies, string(b))
				}
				return bodies
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s: %d files, want %d", dir, len(entries), n)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	bodies := strings.Join(waitFiles(marked, 2), "\n")
	for _, want := range []string{`{"amount":5.5,"id":1,"mrn":"A-1"}`, `{"amount":null,"id":2,"mrn":"O'Brien"}`} {
		if !strings.Contains(bodies, want) {
			t.Errorf("delivered %s, want %s", bodies, want)
		}
	}
	exported := func() (n int) {
		t.Helper()
		if err := db.QueryRow(`SELECT count(*) FROM orders WHERE exported = 1`).Scan(&n); err != nil {
			t.Fatalf("reading the marks: %v", err)
		}
		return n
	}
	deadline := time.Now().Add(15 * time.Second) // beyond a lock wait (5 s)
	for exported() != 2 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if n := exported(); n != 2 {
		t.Errorf("%d rows marked, want 2", n)
	}
	if _, body, _ := c.do(http.MethodGet, "/api/v1/messages?flowId=orders", "", admin); !strings.Contains(body, `"source.database.id":"2"`) {
		t.Errorf("messages lack the row id: %s", body)
	}

	// A new row arrives with the next poll; marked rows are not read again.
	if _, err := db.Exec(`INSERT INTO orders (id, mrn, amount) VALUES (3, 'C-3', 1)`); err != nil {
		t.Fatal(err)
	}
	waitFiles(marked, 3)
	time.Sleep(2500 * time.Millisecond) // two more polls
	if entries, _ := os.ReadDir(marked); len(entries) != 3 {
		t.Errorf("rows read again: %d messages, want 3", len(entries))
	}

	// An unset variable is reported as an event.
	createFlow(t, c, `{"id":"noenv","source":{"type":"database","driver":"sqlite","dsnEnv":"WEAVSTER_DB_UNSET","query":"SELECT 1 AS id","idColumn":"id","update":{"table":"orders","key":"id","set":{"exported":"1"}}}}`)
	deadline = time.Now().Add(10 * time.Second)
	for {
		_, body, _ := c.do(http.MethodGet, "/api/v1/events?flowId=noenv", "", admin)
		if strings.Contains(body, "source.database.failed") && strings.Contains(body, "WEAVSTER_DB_UNSET is not set") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no source.database.failed event: %s", body)
		}
		time.Sleep(100 * time.Millisecond)
	}

	src := func(extra string) string {
		return `{"id":"x","source":{"type":"database","driver":"sqlite","dsnEnv":"WEAVSTER_DB_HIS","query":"SELECT id FROM orders","idColumn":"id"` + extra + `}}`
	}
	mark := `,"update":{"table":"orders","key":"id","set":{"exported":"1"}}`
	for body, want := range map[string]string{
		`{"id":"x","source":{"type":"database","driver":"sqlite","dsnEnv":"WEAVSTER_DB_HIS","query":"DELETE FROM orders","idColumn":"id","update":{"table":"orders","key":"id","set":{"exported":"1"}}}}`:          "must be one SELECT or WITH statement",
		`{"id":"x","source":{"type":"database","driver":"sqlite","dsnEnv":"WEAVSTER_DB_HIS","query":"SELECT 1; DROP TABLE orders","idColumn":"id","update":{"table":"orders","key":"id","set":{"exported":"1"}}}}`: "must be one SELECT or WITH statement",
		src(`,"update":{"table":"orders; x","key":"id","set":{"a":"1"}}`): "flow.schema.json",
		src(`,"update":{"table":"orders","key":"id","set":{"a b":"1"}}`):  "flow.schema.json",
		src(mark + `,"pollIntervalMs":100`):                               "flow.schema.json",
		src(``):                                                           "flow.schema.json", // update is required
		`{"id":"x","inputFormat":"hl7v2","source":{"type":"database","driver":"sqlite","dsnEnv":"WEAVSTER_DB_HIS","query":"SELECT 1 AS id","idColumn":"id","update":{"table":"orders","key":"id","set":{"exported":"1"}}}}`: "inputFormat must be json",
		`{"id":"x","source":{"type":"file","dir":"/tmp/x","query":"SELECT 1"}}`: "flow.schema.json",
	} {
		if code, resp, _ := c.do(http.MethodPost, "/api/v1/flows", body, admin); code != http.StatusBadRequest || !strings.Contains(resp, want) {
			t.Errorf("%s: %d %s", body, code, resp)
		}
	}
}
