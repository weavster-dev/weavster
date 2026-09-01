package gateway

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/weavster-dev/weavster/internal/observability"
	"github.com/weavster-dev/weavster/internal/topology"
)

type fakeTopology struct{}

func (fakeTopology) Overview(context.Context) (topology.Graph, error) {
	return topology.Overview([]topology.FlowSummary{{ID: "a", Name: "Admit", Status: "started"}}), nil
}

func (fakeTopology) FlowInternal(_ context.Context, id string) (topology.Graph, error) {
	return topology.FlowInternal(topology.FlowDetail{ID: id, Name: "Admit"}), nil
}

type fakeFlows struct{ flows []Flow }

func (f *fakeFlows) List(context.Context) ([]Flow, error) { return f.flows, nil }
func (f *fakeFlows) Get(_ context.Context, id string) (Flow, error) {
	for _, fl := range f.flows {
		if fl.ID == id {
			return fl, nil
		}
	}
	return Flow{}, nil
}
func (f *fakeFlows) Create(_ context.Context, fl Flow) error {
	f.flows = append(f.flows, fl)
	return nil
}
func (f *fakeFlows) Delete(_ context.Context, id string) error { return nil }

type fakeMessages struct{}

func (fakeMessages) Search(context.Context, MessageQuery) ([]Message, error) {
	return []Message{{ID: "1", Status: "sent"}}, nil
}

func newTestServer(requireCSRF bool) *Server {
	return New(Config{
		Topology:    fakeTopology{},
		Flows:       &fakeFlows{flows: []Flow{{ID: "f1", Name: "Admit", SourceType: "file", Status: "started", Enabled: true}}},
		Messages:    fakeMessages{},
		System:      observability.SystemStatus("weavster-1", "0.1.0", "2026-08-23"),
		RequireCSRF: requireCSRF,
	})
}

func do(t *testing.T, h http.Handler, method, path string, marker bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if marker {
		req.Header.Set(MarkerHeader, MarkerValue)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestSecurityHeadersAndTraceBlock(t *testing.T) {
	srv := newTestServer(false).Router()

	rec := do(t, srv, http.MethodGet, "/api/openapi.yaml", false)
	if rec.Code != http.StatusOK {
		t.Fatalf("openapi status = %d", rec.Code)
	}
	for hdr, want := range map[string]string{
		"Strict-Transport-Security": "max-age=31536000; includeSubDomains",
		"X-Frame-Options":           "DENY",
		"Content-Security-Policy":   "frame-ancestors 'none'",
		"X-Content-Type-Options":    "nosniff",
	} {
		if got := rec.Header().Get(hdr); got != want {
			t.Errorf("%s = %q, want %q", hdr, got, want)
		}
	}

	rec = do(t, srv, http.MethodTrace, "/", false)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("TRACE status = %d, want 405", rec.Code)
	}
	rec = do(t, srv, "TRACK", "/", false)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("TRACK status = %d, want 405", rec.Code)
	}
}

func TestCSRFMarker(t *testing.T) {
	srv := newTestServer(true).Router()

	if rec := do(t, srv, http.MethodGet, "/api/v1/topology", false); rec.Code != http.StatusBadRequest {
		t.Errorf("missing marker status = %d, want 400", rec.Code)
	}
	if rec := do(t, srv, http.MethodGet, "/api/v1/topology", true); rec.Code != http.StatusOK {
		t.Errorf("with marker status = %d, want 200", rec.Code)
	}
}

func TestTopologyOverview(t *testing.T) {
	srv := newTestServer(true).Router()
	rec := do(t, srv, http.MethodGet, "/api/v1/topology", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "flow:a") {
		t.Errorf("body = %s", rec.Body.String())
	}
}

func TestFlowList(t *testing.T) {
	srv := newTestServer(true).Router()
	rec := do(t, srv, http.MethodGet, "/api/v1/flows", true)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Admit") {
		t.Errorf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}

func TestOpenAPISpecValid(t *testing.T) {
	if err := ValidateSpec(); err != nil {
		t.Fatalf("openapi spec invalid: %v", err)
	}
	if !strings.Contains(OpenAPISpec(), "openapi: 3.1.0") {
		t.Error("spec missing version")
	}
}

func TestTLSConfig(t *testing.T) {
	cfg, err := BuildTLSConfig(DefaultTLSOptions())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MinVersion != tls.VersionTLS12 {
		t.Errorf("min version = %x", cfg.MinVersion)
	}
	if len(cfg.CipherSuites) == 0 || len(cfg.CurvePreferences) == 0 {
		t.Errorf("ciphers/curves empty")
	}
	if _, err := BuildTLSConfig(TLSOptions{}); err != ErrInvalidTLS {
		t.Errorf("expected ErrInvalidTLS, got %v", err)
	}
}

func TestFlowsGetCreateDelete(t *testing.T) {
	srv := newTestServer(true).Router()

	// GET /api/v1/flows/f1 — existing flow.
	rec := do(t, srv, http.MethodGet, "/api/v1/flows/f1", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("FlowsGet status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "f1") {
		t.Errorf("FlowsGet body = %s", rec.Body.String())
	}

	// POST /api/v1/flows — create a new flow.
	body := `{"id":"f2","name":"Discharge","sourceType":"file","status":"stopped","enabled":false}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/flows", strings.NewReader(body))
	req.Header.Set(MarkerHeader, MarkerValue)
	req.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	srv.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusCreated {
		t.Fatalf("FlowsCreate status = %d, want 201; body: %s", rec2.Code, rec2.Body.String())
	}
	if !strings.Contains(rec2.Body.String(), "Discharge") {
		t.Errorf("FlowsCreate body = %s", rec2.Body.String())
	}

	// DELETE /api/v1/flows/f1 — delete a flow.
	rec3 := do(t, srv, http.MethodDelete, "/api/v1/flows/f1", true)
	if rec3.Code != http.StatusNoContent {
		t.Fatalf("FlowsDelete status = %d, want 204", rec3.Code)
	}
}

func TestMessagesSearch(t *testing.T) {
	srv := newTestServer(true).Router()
	rec := do(t, srv, http.MethodGet, "/api/v1/messages", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("MessagesSearch status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "sent") {
		t.Errorf("MessagesSearch body = %s", rec.Body.String())
	}
}

func TestSystemStatus(t *testing.T) {
	srv := newTestServer(true).Router()
	rec := do(t, srv, http.MethodGet, "/api/v1/system", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("System status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "weavster") {
		t.Errorf("System body = %s", rec.Body.String())
	}
}

func TestTopologyFlowInternal(t *testing.T) {
	srv := newTestServer(true).Router()
	rec := do(t, srv, http.MethodGet, "/api/v1/topology/flows/flow-x", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("TopologyFlow status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "flow-x") {
		t.Errorf("TopologyFlow body = %s", rec.Body.String())
	}
}

func TestNilServiceReturns503(t *testing.T) {
	// A server with no optional services wired up must return 503.
	bare := New(Config{RequireCSRF: false}).Router()

	for _, tc := range []struct {
		method, path string
	}{
		{http.MethodGet, "/api/v1/topology"},
		{http.MethodGet, "/api/v1/topology/flows/x"},
		{http.MethodGet, "/api/v1/flows"},
		{http.MethodGet, "/api/v1/flows/x"},
		{http.MethodPost, "/api/v1/flows"},
		{http.MethodDelete, "/api/v1/flows/x"},
		{http.MethodGet, "/api/v1/messages"},
	} {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		rec := httptest.NewRecorder()
		bare.ServeHTTP(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s %s → %d, want 503", tc.method, tc.path, rec.Code)
		}
	}
}

// errFlows is a FlowStore that always returns an error for mutating operations.
type errFlows struct{}

func (e *errFlows) List(_ context.Context) ([]Flow, error) {
	return nil, errors.New("store unavailable")
}
func (e *errFlows) Get(_ context.Context, _ string) (Flow, error) {
	return Flow{}, errors.New("store unavailable")
}
func (e *errFlows) Create(_ context.Context, _ Flow) error { return errors.New("store unavailable") }
func (e *errFlows) Delete(_ context.Context, _ string) error {
	return errors.New("store unavailable")
}

// errMessages is a MessageStore that always errors.
type errMessages struct{}

func (errMessages) Search(_ context.Context, _ MessageQuery) ([]Message, error) {
	return nil, errors.New("search unavailable")
}

// errTopology is a TopologyProvider that always errors.
type errTopology struct{}

func (errTopology) Overview(_ context.Context) (topology.Graph, error) {
	return topology.Graph{}, errors.New("topology unavailable")
}
func (errTopology) FlowInternal(_ context.Context, _ string) (topology.Graph, error) {
	return topology.Graph{}, errors.New("topology unavailable")
}

func newErrServer() *Server {
	return New(Config{
		Topology:    errTopology{},
		Flows:       &errFlows{},
		Messages:    errMessages{},
		System:      observability.SystemStatus("weavster-1", "0.1.0", "2026-08-23"),
		RequireCSRF: false,
	})
}

func TestFlowsHandlerErrorPaths(t *testing.T) {
	srv := newErrServer().Router()

	// List error → 500
	rec := do(t, srv, http.MethodGet, "/api/v1/flows", false)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("FlowsList error: want 500, got %d", rec.Code)
	}

	// Get error → 404
	rec = do(t, srv, http.MethodGet, "/api/v1/flows/missing", false)
	if rec.Code != http.StatusNotFound {
		t.Errorf("FlowsGet error: want 404, got %d", rec.Code)
	}

	// Create bad JSON → 400
	req := httptest.NewRequest(http.MethodPost, "/api/v1/flows", strings.NewReader("{bad json"))
	req.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	srv.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusBadRequest {
		t.Errorf("FlowsCreate bad JSON: want 400, got %d", rec2.Code)
	}

	// Create store error → 500
	body := `{"id":"x","name":"X","sourceType":"file","status":"stopped","enabled":false}`
	req = httptest.NewRequest(http.MethodPost, "/api/v1/flows", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec3 := httptest.NewRecorder()
	srv.ServeHTTP(rec3, req)
	if rec3.Code != http.StatusInternalServerError {
		t.Errorf("FlowsCreate store error: want 500, got %d", rec3.Code)
	}

	// Delete error → 404
	rec = do(t, srv, http.MethodDelete, "/api/v1/flows/missing", false)
	if rec.Code != http.StatusNotFound {
		t.Errorf("FlowsDelete error: want 404, got %d", rec.Code)
	}
}

func TestMessagesSearchErrorPath(t *testing.T) {
	srv := newErrServer().Router()
	rec := do(t, srv, http.MethodGet, "/api/v1/messages", false)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("MessagesSearch error: want 500, got %d", rec.Code)
	}
}

func TestTopologyHandlerErrorPaths(t *testing.T) {
	srv := newErrServer().Router()

	rec := do(t, srv, http.MethodGet, "/api/v1/topology", false)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("TopologyOverview error: want 500, got %d", rec.Code)
	}

	rec = do(t, srv, http.MethodGet, "/api/v1/topology/flows/x", false)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("TopologyFlow error: want 500, got %d", rec.Code)
	}
}

func TestValidateSpecValid(t *testing.T) {
	// ValidateSpec validates the embedded spec — it must return nil normally.
	if err := ValidateSpec(); err != nil {
		t.Fatalf("ValidateSpec on valid spec: %v", err)
	}
}

func TestMessagesSearchQueryParams(t *testing.T) {
	// Ensure query parameters are forwarded to the search (covers the q.Limit default branch).
	srv := newTestServer(false).Router()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/messages?status=sent&flowId=f1", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("MessagesSearch with params: want 200, got %d", rec.Code)
	}
}

// --- auth test fakes ---

// fakeAuth is an AuthProvider stub.
type fakeAuth struct {
	users map[string]fakeUser // username → user
}

type fakeUser struct {
	password    string
	permissions []string
}

func (a *fakeAuth) Authenticate(_ context.Context, username, password, _ string) (Identity, error) {
	if a == nil {
		return Identity{}, errors.New("no auth provider")
	}
	u, ok := a.users[username]
	if !ok || u.password != password {
		return Identity{}, errors.New("auth failed")
	}
	return Identity{Username: username, Permissions: u.permissions}, nil
}

// fakeAuthorizer is an Authorizer stub that mirrors the real LocalAuthorizer logic.
type fakeAuthorizer struct{}

func (fakeAuthorizer) Authorize(_ context.Context, id Identity, resource, action string) bool {
	need := resource + ":" + action
	for _, p := range id.Permissions {
		if p == "admin" || p == need {
			return true
		}
	}
	return false
}

// fakeAudit is an AuditSink that captures entries.
type fakeAudit struct {
	entries []auditEntry
}

type auditEntry struct {
	actor, action, resource string
}

func (a *fakeAudit) Record(_ context.Context, actor, action, resource string) error {
	a.entries = append(a.entries, auditEntry{actor, action, resource})
	return nil
}

// newAuthedServer returns a Server wired with fake auth providers. When
// authCfg is nil, no auth/authorizer/audit adapters are set (degraded).
func newAuthedServer(authCfg *fakeAuth, az Authorizer, aud *fakeAudit) *Server {
	s := New(Config{
		Topology:    fakeTopology{},
		Flows:       &fakeFlows{flows: []Flow{{ID: "f1", Name: "Admit", SourceType: "file", Status: "started", Enabled: true}}},
		Messages:    fakeMessages{},
		System:      observability.SystemStatus("weavster-1", "0.1.0", "2026-08-23"),
		RequireCSRF: false,
	})
	if authCfg != nil {
		s.cfg.Auth = authCfg
		s.cfg.Authorizer = az
		s.cfg.Audit = aud
	}
	return s
}

// doAuth is like do but sets HTTP Basic Auth credentials.
func doAuth(t *testing.T, h http.Handler, method, path, user, pass string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if user != "" || pass != "" {
		req.SetBasicAuth(user, pass)
		req.Header.Set("X-Forwarded-Proto", "https")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestAuthUnauthenticatedReturns401(t *testing.T) {
	auth := &fakeAuth{users: map[string]fakeUser{"admin": {"pass", []string{"admin"}}}}
	srv := newAuthedServer(auth, fakeAuthorizer{}, &fakeAudit{}).Router()

	paths := []string{
		"/api/v1/system",
		"/api/v1/topology",
		"/api/v1/topology/flows/x",
		"/api/v1/flows",
		"/api/v1/flows/f1",
		"/api/v1/messages",
	}
	for _, p := range paths {
		rec := do(t, srv, http.MethodGet, p, false)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("GET %s without auth = %d, want 401", p, rec.Code)
		}
		if www := rec.Header().Get("WWW-Authenticate"); www == "" {
			t.Errorf("GET %s: missing WWW-Authenticate header", p)
		}
	}

	// POST /api/v1/flows without auth
	rec := do(t, srv, http.MethodPost, "/api/v1/flows", false)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("POST /api/v1/flows without auth = %d, want 401", rec.Code)
	}

	// DELETE /api/v1/flows/f1 without auth
	rec = do(t, srv, http.MethodDelete, "/api/v1/flows/f1", false)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("DELETE /api/v1/flows/f1 without auth = %d, want 401", rec.Code)
	}
}

func TestAuthWrongPasswordReturns401(t *testing.T) {
	auth := &fakeAuth{users: map[string]fakeUser{"admin": {"pass", []string{"admin"}}}}
	srv := newAuthedServer(auth, fakeAuthorizer{}, &fakeAudit{}).Router()

	rec := doAuth(t, srv, http.MethodGet, "/api/v1/system", "admin", "wrong")
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("wrong password: want 401, got %d", rec.Code)
	}
}

func TestAuthAdminCanAccessEverything(t *testing.T) {
	auth := &fakeAuth{users: map[string]fakeUser{"admin": {"pass", []string{"admin"}}}}
	aud := &fakeAudit{}
	srv := newAuthedServer(auth, fakeAuthorizer{}, aud).Router()

	tests := []struct {
		method, path string
		body         string
		want         int
	}{
		{http.MethodGet, "/api/v1/system", "", http.StatusOK},
		{http.MethodGet, "/api/v1/topology", "", http.StatusOK},
		{http.MethodGet, "/api/v1/topology/flows/f1", "", http.StatusOK},
		{http.MethodGet, "/api/v1/flows", "", http.StatusOK},
		{http.MethodGet, "/api/v1/flows/f1", "", http.StatusOK},
		{http.MethodPost, "/api/v1/flows", `{"id":"f2","name":"Discharge","sourceType":"file","status":"stopped","enabled":false}`, http.StatusCreated},
		{http.MethodDelete, "/api/v1/flows/f1", "", http.StatusNoContent},
		{http.MethodGet, "/api/v1/messages", "", http.StatusOK},
	}
	for _, tc := range tests {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		req.SetBasicAuth("admin", "pass")
		req.Header.Set("X-Forwarded-Proto", "https")
		if tc.body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if rec.Code != tc.want {
			t.Errorf("%s %s as admin: want %d, got %d", tc.method, tc.path, tc.want, rec.Code)
		}
	}

	// Audit should have login + accesses.
	if len(aud.entries) == 0 {
		t.Error("expected audit entries but got none")
	}
	// First entry must be the login.
	if aud.entries[0].action != "authenticate" || aud.entries[0].resource != "api:success" {
		t.Errorf("first audit = %+v, want authenticate:api:success", aud.entries[0])
	}
}

func TestAuthViewerCannotMutate(t *testing.T) {
	auth := &fakeAuth{users: map[string]fakeUser{
		"viewer": {"pass", []string{"flows:view", "messages:view", "system:view", "topology:view"}},
	}}
	srv := newAuthedServer(auth, fakeAuthorizer{}, &fakeAudit{}).Router()

	// Viewer can read.
	for _, p := range []string{"/api/v1/system", "/api/v1/topology", "/api/v1/flows", "/api/v1/flows/f1", "/api/v1/messages"} {
		rec := doAuth(t, srv, http.MethodGet, p, "viewer", "pass")
		if rec.Code != http.StatusOK {
			t.Errorf("viewer GET %s: want 200, got %d", p, rec.Code)
		}
	}

	// Viewer cannot create or delete.
	rec := doAuth(t, srv, http.MethodPost, "/api/v1/flows", "viewer", "pass")
	if rec.Code != http.StatusForbidden {
		t.Errorf("viewer POST /api/v1/flows: want 403, got %d", rec.Code)
	}
	rec = doAuth(t, srv, http.MethodDelete, "/api/v1/flows/f1", "viewer", "pass")
	if rec.Code != http.StatusForbidden {
		t.Errorf("viewer DELETE /api/v1/flows/f1: want 403, got %d", rec.Code)
	}
}

func TestAuthFailedLoginAudited(t *testing.T) {
	auth := &fakeAuth{users: map[string]fakeUser{"admin": {"pass", []string{"admin"}}}}
	aud := &fakeAudit{}
	srv := newAuthedServer(auth, fakeAuthorizer{}, aud).Router()

	// Attempt login with wrong password.
	rec := doAuth(t, srv, http.MethodGet, "/api/v1/system", "admin", "wrong")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}

	// Make a request without credentials to trigger missing-credentials audit.
	rec2 := do(t, srv, http.MethodGet, "/api/v1/system", false)
	if rec2.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without creds, got %d", rec2.Code)
	}

	// Audit must show the failed attempt.
	foundFailed := false
	foundMissing := false
	for _, e := range aud.entries {
		if e.action == "authenticate" && e.resource == "api:failed" {
			foundFailed = true
		}
		if e.action == "authenticate" && e.resource == "api:missing" {
			foundMissing = true
		}
	}
	if !foundFailed {
		t.Errorf("missing audit entry for failed login; entries: %+v", aud.entries)
	}
	if !foundMissing {
		t.Errorf("missing audit entry for missing credentials; entries: %+v", aud.entries)
	}
}

func TestAuthForbiddenAudited(t *testing.T) {
	auth := &fakeAuth{users: map[string]fakeUser{"viewer": {"pass", []string{"messages:view"}}}}
	aud := &fakeAudit{}
	srv := newAuthedServer(auth, fakeAuthorizer{}, aud).Router()

	rec := doAuth(t, srv, http.MethodPost, "/api/v1/flows", "viewer", "pass")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}

	// Must have an authorize-failure audit entry.
	found := false
	for _, e := range aud.entries {
		if e.action == "authorize:flows:edit" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("missing audit entry for forbidden access; entries: %+v", aud.entries)
	}
}

func TestOpenAPISpecUnauthenticated(t *testing.T) {
	// /api/openapi.yaml must remain accessible without auth.
	auth := &fakeAuth{users: map[string]fakeUser{"admin": {"pass", []string{"admin"}}}}
	srv := newAuthedServer(auth, fakeAuthorizer{}, &fakeAudit{}).Router()

	rec := do(t, srv, http.MethodGet, "/api/openapi.yaml", false)
	if rec.Code != http.StatusOK {
		t.Errorf("openapi.yaml without auth: want 200, got %d", rec.Code)
	}
}

func TestDegradedModeNoAuth(t *testing.T) {
	// Without auth/authorizer/audit configured, all routes work as before.
	srv := newAuthedServer(nil, nil, nil).Router()

	paths := []string{
		"/api/v1/system",
		"/api/v1/topology",
		"/api/v1/topology/flows/f1",
		"/api/v1/flows",
		"/api/v1/flows/f1",
		"/api/v1/messages",
	}
	for _, p := range paths {
		rec := do(t, srv, http.MethodGet, p, false)
		if rec.Code != http.StatusOK {
			t.Errorf("degraded GET %s: want 200, got %d", p, rec.Code)
		}
	}

	body := `{"id":"f2","name":"Discharge","sourceType":"file","status":"stopped","enabled":false}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/flows", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Errorf("degraded POST /api/v1/flows: want 201, got %d", rec.Code)
	}

	rec = do(t, srv, http.MethodDelete, "/api/v1/flows/f1", false)
	if rec.Code != http.StatusNoContent {
		t.Errorf("degraded DELETE /api/v1/flows/f1: want 204, got %d", rec.Code)
	}
}

func TestAuthRejectsCleartextCredentials(t *testing.T) {
	auth := &fakeAuth{users: map[string]fakeUser{"admin": {"pass", []string{"admin"}}}}
	srv := newAuthedServer(auth, fakeAuthorizer{}, &fakeAudit{}).Router()

	// Credentials over cleartext (no TLS, no proxy header) → 400.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/system", nil)
	req.SetBasicAuth("admin", "pass")
	// deliberately omit X-Forwarded-Proto header
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("cleartext credentials: want 400, got %d", rec.Code)
	}

	// Same credentials with proxy header → 200.
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/system", nil)
	req2.SetBasicAuth("admin", "pass")
	req2.Header.Set("X-Forwarded-Proto", "https")
	rec2 := httptest.NewRecorder()
	srv.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Errorf("credentials with proxy header: want 200, got %d", rec2.Code)
	}
}
