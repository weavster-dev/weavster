package main

import (
	"database/sql"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/weavster-dev/weavster/internal/outbox"
)

// TestDatabaseDestination: a database destination inserts each message's
// transformed values into a table, with parameters (a quote in a value is
// data) and NULL for a missing path; with keyColumn, a retry after a lost
// success inserts nothing; an unset dsnEnv or a missing table fails the
// delivery; invalid definitions are refused.
func TestDatabaseDestination(t *testing.T) {
	dbFile := filepath.Join(t.TempDir(), "lab.db") + sqliteShared
	db, err := sql.Open("sqlite", dbFile)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(`CREATE TABLE results (mrn TEXT, value REAL, note TEXT, delivery TEXT UNIQUE)`); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WEAVSTER_DB_LAB", dbFile)
	addr := freeAddr(t)
	cfg := writeConfig(t, "listen: {address: \""+addr+"\"}\n"+storeConfig(t)+
		"delivery: {maxAttempts: 50, backoffBaseMs: 10, retryIntervalMs: 20}\n")
	stop := startCLI(t, []string{"server", "--config", cfg}, "http://"+addr+"/api/openapi.yaml")
	defer stop()
	c := apiClient{t: t, base: "http://" + addr}
	admin := basic(bootstrapAdmin, testAdminPassword)

	createFlow(t, c, `{"id":"lab","transform":{"steps":[{"map":{"from":"PID.mrn","to":"patient.mrn"}},{"map":{"from":"value","to":"obs.value"}}]},`+
		`"destinations":[{"name":"db","type":"database","driver":"sqlite","dsnEnv":"WEAVSTER_DB_LAB","table":"results",`+
		`"columns":{"mrn":"patient.mrn","value":"obs.value","note":"missing.path"},"keyColumn":"delivery"}]}`)
	_, status := sendMessage(t, c, "lab", `{"PID":{"mrn":"O'Brien-123"},"value":5.4}`)
	if status != "sent" {
		t.Fatalf("status = %s, want sent", status)
	}
	rows := func() (n int, mrn string, value float64, note sql.NullString) {
		t.Helper()
		if err := db.QueryRow(`SELECT (SELECT count(*) FROM results), mrn, value, note FROM results`).Scan(&n, &mrn, &value, &note); err != nil {
			t.Fatal(err)
		}
		return
	}
	if n, mrn, value, note := rows(); n != 1 || mrn != "O'Brien-123" || value != 5.4 || note.Valid {
		t.Errorf("row: %d %q %v %v", n, mrn, value, note)
	}

	// A retry after a lost success: the row with the delivery's key is
	// already there (as if the first attempt's reply was lost), so the
	// delivery inserts nothing and succeeds.
	if code, resp, _ := c.do(http.MethodPost, "/api/v1/flows/lab/destinations/db/stop", "", admin); code != http.StatusOK {
		t.Fatalf("stop destination: %d %s", code, resp)
	}
	id2, _ := sendMessage(t, c, "lab", `{"PID":{"mrn":"456"},"value":1}`)
	if _, err := db.Exec(`INSERT INTO results (mrn, delivery) VALUES ('earlier', ?)`, outbox.IdempotencyKey(id2, "db")); err != nil {
		t.Fatal(err)
	}
	if code, resp, _ := c.do(http.MethodPost, "/api/v1/flows/lab/destinations/db/start", "", admin); code != http.StatusOK {
		t.Fatalf("start destination: %d %s", code, resp)
	}
	waitStatus(t, c, id2, "sent")
	var n int
	var mrn string
	if err := db.QueryRow(`SELECT count(*), max(CASE WHEN delivery = ? THEN mrn END) FROM results`, outbox.IdempotencyKey(id2, "db")).Scan(&n, &mrn); err != nil || n != 2 || mrn != "earlier" {
		t.Errorf("after the retry: %d rows, the keyed row's mrn %q (%v)", n, mrn, err)
	}

	// Failures are retried: an unset variable, a missing table.
	for flow, want := range map[string]string{
		`{"id":"noenv","destinations":[{"name":"db","type":"database","driver":"sqlite","dsnEnv":"WEAVSTER_DB_UNSET","table":"results","columns":{"mrn":"a"}}]}`: "environment variable WEAVSTER_DB_UNSET is not set",
		`{"id":"notable","destinations":[{"name":"db","type":"database","driver":"sqlite","dsnEnv":"WEAVSTER_DB_LAB","table":"nope","columns":{"mrn":"a"}}]}`:    "no such table",
	} {
		createFlow(t, c, flow)
		fid := flow[strings.Index(flow, `"id":"`)+6 : strings.Index(flow, `","destinations"`)]
		mid, status := sendMessage(t, c, fid, `{"a":"1"}`)
		if status != "queued" {
			t.Errorf("%s: status %s, want queued", fid, status)
		}
		deadline := time.Now().Add(15 * time.Second) // beyond a lock wait (5 s)
		for {
			_, body, _ := c.do(http.MethodGet, "/api/v1/messages/"+mid, "", admin)
			if strings.Contains(body, want) {
				break
			}
			if time.Now().After(deadline) {
				t.Errorf("%s: no attempt error %q: %s", fid, want, body)
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
	}

	for body, want := range map[string]string{
		`{"id":"x","destinations":[{"name":"db","type":"database","driver":"mysql","dsnEnv":"WEAVSTER_DB_LAB","table":"t","columns":{"a":"a"}}]}`:                        "flow.schema.json",
		`{"id":"x","destinations":[{"name":"db","type":"database","driver":"sqlite","dsnEnv":"HOME","table":"t","columns":{"a":"a"}}]}`:                                  "flow.schema.json",
		`{"id":"x","destinations":[{"name":"db","type":"database","driver":"sqlite","dsnEnv":"WEAVSTER_DB_LAB","table":"t; DROP","columns":{"a":"a"}}]}`:                 "flow.schema.json",
		`{"id":"x","destinations":[{"name":"db","type":"database","driver":"sqlite","dsnEnv":"WEAVSTER_DB_LAB","table":"t","columns":{"a":"a..b"}}]}`:                    "path must be dot-separated names",
		`{"id":"x","destinations":[{"name":"db","type":"database","driver":"sqlite","dsnEnv":"WEAVSTER_DB_LAB","table":"t","columns":{"k":"a"},"keyColumn":"k"}]}`:       "is the keyColumn",
		`{"id":"x","inputFormat":"hl7v2","destinations":[{"name":"db","type":"database","driver":"sqlite","dsnEnv":"WEAVSTER_DB_LAB","table":"t","columns":{"a":"a"}}]}`: "needs a JSON object",
		`{"id":"x","destinations":[{"name":"out","type":"file","dir":"/tmp/x","table":"t"}]}`:                                                                            "flow.schema.json",
	} {
		if code, resp, _ := c.do(http.MethodPost, "/api/v1/flows", body, admin); code != http.StatusBadRequest || !strings.Contains(resp, want) {
			t.Errorf("%s: %d %s", body, code, resp)
		}
	}
}
