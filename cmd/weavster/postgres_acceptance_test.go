package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

// postgresStoreDSN is a connection string for a fresh schema of the
// PostgreSQL database WEAVSTER_TEST_POSTGRES_DSN names (the CI PostgreSQL
// job sets it); without it the test is skipped: no test needs PostgreSQL.
func postgresStoreDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("WEAVSTER_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("WEAVSTER_TEST_POSTGRES_DSN is not set")
	}
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	schema := "t_" + hex.EncodeToString(b)
	t.Cleanup(func() {
		_, _ = admin.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE")
		_ = admin.Close()
	})
	if _, err := admin.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	return u.String()
}

// TestServerOnPostgres: with store.dialect postgres the server runs its
// schema migrations, stores flows, messages (with their attempts), users,
// and lookups, answers message search and trends, and keeps all of it
// across a restart.
func TestServerOnPostgres(t *testing.T) {
	dsn := postgresStoreDSN(t)
	addr := freeAddr(t)
	cfg := writeConfig(t, "listen: {address: \""+addr+"\"}\nstore: {dialect: postgres, dsn: \""+dsn+"\", maxRetry: 2, retryWaitMs: 500}\n")
	stop := startCLI(t, []string{"server", "--config", cfg}, "http://"+addr+"/api/openapi.yaml")
	c := apiClient{t: t, base: "http://" + addr}
	admin := basic(bootstrapAdmin, testAdminPassword)
	out := t.TempDir()
	createFlow(t, c, `{"id":"pg","transform":{"steps":[{"set":{"field":"ok","expr":"yes"}}]},"destinations":[{"name":"out","type":"file","dir":"`+out+`"}]}`)
	var ids []string
	for i := 0; i < 3; i++ {
		id, status := sendMessage(t, c, "pg", `{"n":1}`)
		if status != "sent" {
			t.Fatalf("message %d: %s", i, status)
		}
		ids = append(ids, id)
	}
	if code, _, _ := c.do(http.MethodPut, "/api/v1/lookups/mrn/k1", `{"value":"v1"}`, admin); code != http.StatusOK {
		t.Fatalf("lookup put: %d", code)
	}
	from := time.Now().Add(-time.Hour).UTC().Truncate(time.Hour).Format(time.RFC3339)
	to := time.Now().Add(time.Hour).UTC().Truncate(time.Hour).Format(time.RFC3339)
	if code, body, _ := c.do(http.MethodGet, "/api/v1/messages/trends?from="+from+"&to="+to+"&interval=hour", "", admin); code != http.StatusOK || !strings.Contains(body, `"sent":3`) {
		t.Errorf("trends: %d %s", code, body)
	}
	stop()

	// A restart on the same database keeps everything.
	stop = startCLI(t, []string{"server", "--config", cfg}, "http://"+addr+"/api/openapi.yaml")
	defer stop()
	if code, body, _ := c.do(http.MethodGet, "/api/v1/flows/pg", "", admin); code != http.StatusOK || !strings.Contains(body, `"status":"started"`) {
		t.Errorf("flow after restart: %d %s", code, body)
	}
	for _, id := range ids {
		if code, body, _ := c.do(http.MethodGet, "/api/v1/messages/"+id, "", admin); code != http.StatusOK || !strings.Contains(body, `"out":{"attempts":1`) {
			t.Errorf("message %s after restart: %d %s", id, code, body)
		}
	}
	if code, body, _ := c.do(http.MethodGet, "/api/v1/messages?flowId=pg&limit=2&sort=-receivedAt", "", admin); code != http.StatusOK || strings.Count(body, `"id"`) != 2 {
		t.Errorf("search after restart: %d %s", code, body)
	}
	if code, body, _ := c.do(http.MethodGet, "/api/v1/lookups/mrn/k1", "", admin); code != http.StatusOK || !strings.Contains(body, `"v1"`) {
		t.Errorf("lookup after restart: %d %s", code, body)
	}
}
