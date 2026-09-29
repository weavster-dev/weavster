// Package durability is the black-box restart/retry/dead-letter suite: it
// runs the weavster binary as its own process on PostgreSQL, crashes it
// (SIGKILL) and stops it (SIGTERM), and checks over HTTP that no stored
// message is lost and each is delivered once its destination recovers.
// Without WEAVSTER_TEST_POSTGRES_DSN (set by the CI PostgreSQL job) it is
// skipped.
package durability

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/weavster-dev/weavster/test/e2e/harness"
)

var bin string

func TestMain(m *testing.M) {
	if os.Getenv("WEAVSTER_TEST_POSTGRES_DSN") == "" {
		os.Exit(m.Run()) // every test skips
	}
	dir, err := os.MkdirTemp("", "weavster-e2e-durability")
	if err != nil {
		panic(err)
	}
	if bin, err = harness.Build(dir); err != nil {
		panic(err)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// freshSchema returns a connection string for a new, empty schema of the
// test database, dropped when the test ends.
func freshSchema(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("WEAVSTER_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("WEAVSTER_TEST_POSTGRES_DSN is not set")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	schema := "e2e_" + hex.EncodeToString(b)
	t.Cleanup(func() {
		_, _ = db.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE")
		_ = db.Close()
	})
	if _, err := db.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(dsn)
	if err != nil || u.Scheme == "" {
		t.Fatalf("WEAVSTER_TEST_POSTGRES_DSN must be a postgres:// URL (%v)", err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	return u.String()
}

// destination is an HTTP receiver that fails (503) until healthy is set,
// and records the Idempotency-Key of every request and how many it
// accepted.
type destination struct {
	*httptest.Server
	healthy  atomic.Bool
	mu       sync.Mutex
	keys     []string
	accepted int
}

func newDestination(t *testing.T) *destination {
	d := &destination{}
	d.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.mu.Lock()
		defer d.mu.Unlock()
		d.keys = append(d.keys, r.Header.Get("Idempotency-Key"))
		if !d.healthy.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		d.accepted++
	}))
	t.Cleanup(d.Close)
	return d
}

func (d *destination) requests() (keys []string, accepted int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.keys...), d.accepted
}

// deliveredOnce fails the test unless the destination accepted exactly one
// request, and every attempt carried the same Idempotency-Key.
func (d *destination) deliveredOnce(t *testing.T) {
	t.Helper()
	time.Sleep(300 * time.Millisecond) // several retry intervals: a second delivery would show
	keys, accepted := d.requests()
	if accepted != 1 {
		t.Errorf("the destination accepted %d deliveries, want 1", accepted)
	}
	for _, k := range keys {
		if k == "" || k != keys[0] {
			t.Errorf("Idempotency-Keys %v: want the same key on every attempt", keys)
			break
		}
	}
}

// server is one test's server: its configuration survives restarts.
type server struct {
	t        *testing.T
	base     string
	config   string
	password string // the admin's, kept in the store across restarts
	proc     *harness.Server
}

func newServer(t *testing.T, delivery string) *server {
	addr, err := harness.FreeAddr()
	if err != nil {
		t.Fatal(err)
	}
	s := &server{t: t, base: "http://" + addr, config: "listen: {address: \"" + addr + "\"}\n" +
		"store: {dialect: postgres, dsn: \"" + freshSchema(t) + "\"}\n" + delivery}
	s.start()
	t.Cleanup(func() {
		if s.proc != nil {
			s.proc.Kill()
		}
	})
	return s
}

func (s *server) start() {
	s.t.Helper()
	p, err := harness.Start(bin, s.t.TempDir(), harness.Options{Config: s.config, BaseURL: s.base, Password: s.password})
	if err != nil {
		s.t.Fatal(err)
	}
	s.proc, s.password = p, p.Password
}

func (s *server) crash() { s.proc.Kill(); s.proc = nil }

func (s *server) stop() {
	s.t.Helper()
	if code := s.proc.Stop(); code != 0 {
		s.t.Errorf("SIGTERM: exit %d\n%s", code, s.proc.Log())
	}
	s.proc = nil
}

func (s *server) api(method, path, body string) (int, string) {
	s.t.Helper()
	code, resp, _, err := harness.Request(http.DefaultClient, method, s.base+path, body, "admin", s.password, false)
	if err != nil {
		s.t.Fatal(err)
	}
	return code, resp
}

// flow creates, deploys, and starts flow f delivering to url.
func (s *server) flow(url string) {
	s.t.Helper()
	if code, body := s.api(http.MethodPost, "/api/v1/flows", `{"id":"f","destinations":[{"name":"ehr","type":"http","url":"`+url+`"}]}`); code != http.StatusCreated {
		s.t.Fatalf("create flow: %d %s", code, body)
	}
	for _, action := range []string{"deploy", "start"} {
		if code, body := s.api(http.MethodPost, "/api/v1/flows/f/"+action, ""); code != http.StatusOK {
			s.t.Fatalf("%s: %d %s", action, code, body)
		}
	}
}

// send sends one message to flow f and returns its id and status.
func (s *server) send() (string, string) {
	s.t.Helper()
	code, body := s.api(http.MethodPost, "/api/v1/flows/f/messages", `{"patient":"Ada"}`)
	var m struct{ ID, Status string }
	if code != http.StatusAccepted || json.Unmarshal([]byte(body), &m) != nil {
		s.t.Fatalf("send: %d %s", code, body)
	}
	return m.ID, m.Status
}

// await waits up to 30 seconds for message id to reach status.
func (s *server) await(id, status string) {
	s.t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		code, body := s.api(http.MethodGet, "/api/v1/messages/"+id, "")
		var m struct{ Status string }
		if code == http.StatusOK && json.Unmarshal([]byte(body), &m) == nil && m.Status == status {
			return
		}
		if time.Now().After(deadline) {
			s.t.Fatalf("message %s did not become %s: %d %s", id, status, code, body)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// TestCrashKeepsQueuedMessages: a message whose delivery failed is stored
// as queued; after a crash (SIGKILL) and a restart it is delivered once the
// destination recovers, with the same Idempotency-Key on every attempt.
func TestCrashKeepsQueuedMessages(t *testing.T) {
	dest := newDestination(t)
	s := newServer(t, "delivery: {maxAttempts: 1000, backoffBaseMs: 50, retryIntervalMs: 50}\n")
	s.flow(dest.URL)
	id, status := s.send()
	if status != "queued" {
		t.Fatalf("with the destination down the message is %s, want queued", status)
	}
	s.crash()
	dest.healthy.Store(true)
	s.start()
	s.await(id, "sent")

	if keys, _ := dest.requests(); len(keys) < 2 {
		t.Fatalf("the destination got %d requests, want a failure and a delivery", len(keys))
	}
	dest.deliveredOnce(t)
	s.stop()
}

// TestDeadLetterSurvivesRestart: when retries run out the message is
// dead-lettered; it stays dead-lettered across a restart, and requeued once
// the destination recovers, it is delivered.
func TestDeadLetterSurvivesRestart(t *testing.T) {
	dest := newDestination(t)
	s := newServer(t, "delivery: {maxAttempts: 2, backoffBaseMs: 50, retryIntervalMs: 50}\n")
	s.flow(dest.URL)
	id, _ := s.send()
	s.await(id, "dead-lettered")
	s.stop()

	s.start()
	code, body := s.api(http.MethodGet, "/api/v1/messages?flowId=f&status=dead-lettered", "")
	var list []struct{ ID string }
	if code != http.StatusOK || json.Unmarshal([]byte(body), &list) != nil || len(list) != 1 || list[0].ID != id {
		t.Fatalf("dead letters after the restart: %d %s", code, body)
	}
	dest.healthy.Store(true)
	if code, body := s.api(http.MethodPost, "/api/v1/messages/"+id+"/requeue", ""); code/100 != 2 {
		t.Fatalf("requeue: %d %s", code, body)
	}
	s.await(id, "sent")
	dest.deliveredOnce(t)
	s.stop()
}
