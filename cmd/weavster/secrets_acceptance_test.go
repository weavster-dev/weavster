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

// TestSecretsFromFiles: with secrets.dir, an http source's Basic password
// and a database destination's connection string are read from files in
// that directory (a trailing newline dropped), and a message goes through
// both; on PostgreSQL the server's own store connects through
// store.dsnEnv read from a file.
func TestSecretsFromFiles(t *testing.T) {
	secrets := t.TempDir()
	dbFile := filepath.Join(t.TempDir(), "lab.db") + sqliteShared
	db, err := sql.Open("sqlite", dbFile)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(`CREATE TABLE results (mrn TEXT)`); err != nil {
		t.Fatal(err)
	}
	write := func(name, value string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(secrets, name), []byte(value+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("WEAVSTER_SOURCE_FILE_LAB", "from-a-file")
	write("WEAVSTER_DB_FILE_LAB", dbFile)
	store := storeConfig(t)
	if testPostgres() { // the store's own connection string from a file too
		write("WEAVSTER_STORE_DSN", postgresStoreDSN(t))
		store = "store: {dialect: postgres, dsnEnv: WEAVSTER_STORE_DSN}\n"
	}

	addr, src := freeAddr(t), freeAddr(t)
	cfg := writeConfig(t, "listen: {address: \""+addr+"\"}\nsecrets: {dir: \""+secrets+"\"}\n"+store)
	stop := startCLI(t, []string{"server", "--config", cfg}, "http://"+addr+"/api/openapi.yaml")
	defer stop()
	c := apiClient{t: t, base: "http://" + addr}
	createFlow(t, c, `{"id":"lab","source":{"type":"http","address":"`+src+`","username":"lab","passwordEnv":"WEAVSTER_SOURCE_FILE_LAB"},`+
		`"destinations":[{"name":"db","type":"database","driver":"sqlite","dsnEnv":"WEAVSTER_DB_FILE_LAB","table":"results","columns":{"mrn":"mrn"}}]}`)

	post := func(password string) int {
		req, _ := http.NewRequest(http.MethodPost, "http://"+src+"/", strings.NewReader(`{"mrn":"123"}`))
		req.SetBasicAuth("lab", password)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return 0
		}
		_ = res.Body.Close()
		return res.StatusCode
	}
	for deadline := time.Now().Add(10 * time.Second); post("from-a-file") != http.StatusAccepted; time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the source never accepted the password from the file")
		}
	}
	if code := post("from-a-file\n"); code != http.StatusUnauthorized {
		t.Errorf("the password with the file's newline = %d, want 401", code)
	}
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		var n int
		if err := db.QueryRow(`SELECT count(*) FROM results WHERE mrn = '123'`).Scan(&n); err == nil && n == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the message never reached the database named in the secret file")
		}
	}
}
