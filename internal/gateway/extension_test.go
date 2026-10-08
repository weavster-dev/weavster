package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// The Enterprise edition plugs its own adapters into the gateway's ports
// (#107 §20, arch §3.1): an external identity provider (OIDC/SAML) as the
// AuthProvider, a policy engine (OPA/Cedar/ABAC) as the Authorizer, and an
// immutable or SIEM audit adapter as the AuditSink. These fakes stand in
// for them, so the ports stay sufficient for those adapters.

// externalIdP accepts a token instead of a password, with a one-time code
// (the MFA hook); its groups become the identity's permissions.
type externalIdP struct{}

func (externalIdP) Authenticate(_ context.Context, user, token, mfa string) (Identity, error) {
	if user != "carol" || token != "token-abc" {
		return Identity{}, errors.New("idp: invalid token")
	}
	if mfa != "123456" {
		return Identity{}, errors.New("mfa: code required")
	}
	// The provider also asserts admin: only the policy decides what it means.
	return Identity{Username: "carol", Permissions: []string{"group:integration", "admin"}}, nil
}

// policyEngine decides by attributes, not by the built-in permissions:
// the integration group may view flows and nothing else, and even an admin
// permission grants nothing it does not allow.
type policyEngine struct{}

func (policyEngine) Authorize(_ context.Context, id Identity, resource, action string) bool {
	for _, p := range id.Permissions {
		if p == "group:integration" && resource == "flows" && action == "view" {
			return true
		}
	}
	return false
}

// siem collects audit entries, or fails every one.
type siem struct {
	mu      sync.Mutex
	entries []AuditEvent
	fail    bool
}

func (s *siem) Record(_ context.Context, e AuditEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail {
		return errors.New("siem: unreachable")
	}
	s.entries = append(s.entries, e)
	return nil
}

func (s *siem) actions() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, e := range s.entries {
		out = append(out, e.Actor+" "+e.Action+" "+e.Detail["status"])
	}
	return out
}

// TestEnterpriseExtensionPorts: with an external identity provider, a
// policy engine, and a SIEM sink plugged in, requests authenticate through
// the provider (MFA code by header or login field), are authorized only by
// the policy, and every audit entry reaches the sink; a failing sink never
// changes a response.
func TestEnterpriseExtensionPorts(t *testing.T) {
	sink := &siem{}
	s := New(Config{Auth: externalIdP{}, Authorizer: policyEngine{}, Audit: sink, Flows: &stubFlows{}, System: fakeSystem{}})
	carol := func(mfa string) func(*http.Request) {
		return func(r *http.Request) {
			r.SetBasicAuth("carol", "token-abc")
			if mfa != "" {
				r.Header.Set("X-Weavster-MFA", mfa)
			}
		}
	}
	for _, tt := range []struct {
		name, method, path, body string
		creds                    func(*http.Request)
		status                   int
	}{
		{"allowed by the policy", http.MethodGet, "/api/v1/flows", "", carol("123456"), http.StatusOK},
		{"MFA code missing", http.MethodGet, "/api/v1/flows", "", carol(""), http.StatusUnauthorized},
		{"denied by the policy", http.MethodPost, "/api/v1/flows", `{"id":"x"}`, carol("123456"), http.StatusForbidden},
		{"not a flows route", http.MethodGet, "/api/v1/events", "", carol("123456"), http.StatusForbidden},
	} {
		if rec := serve(s, tt.method, tt.path, tt.body, tt.creds); rec.Code != tt.status {
			t.Errorf("%s: %d %s", tt.name, rec.Code, rec.Body.String())
		}
	}
	rec := serve(s, http.MethodPost, "/api/v1/auth/login", `{"username":"carol","password":"token-abc","mfaCode":"123456"}`, nil)
	var login struct{ Token string }
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &login) != nil || login.Token == "" {
		t.Fatalf("login: %d %s", rec.Code, rec.Body.String())
	}
	if rec := serve(s, http.MethodGet, "/api/v1/flows", "", func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+login.Token) }); rec.Code != http.StatusOK {
		t.Errorf("with the session: %d", rec.Code)
	}
	got := strings.Join(sink.actions(), "\n")
	for _, want := range []string{" auth.failure 401", "carol POST /api/v1/flows 403", "carol auth.login 200"} {
		if !strings.Contains(got, want) {
			t.Errorf("the sink lacks %q:\n%s", want, got)
		}
	}

	failing := New(Config{Auth: externalIdP{}, Authorizer: policyEngine{}, Audit: &siem{fail: true}, Flows: &stubFlows{}, System: fakeSystem{}})
	if rec := serve(failing, http.MethodGet, "/api/v1/flows", "", carol("123456")); rec.Code != http.StatusOK {
		t.Errorf("with a failing sink: %d", rec.Code)
	}
}
