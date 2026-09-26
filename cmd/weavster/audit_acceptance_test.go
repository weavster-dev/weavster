package main

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/weavster-dev/weavster/internal/audit"
	"github.com/weavster-dev/weavster/internal/gateway"
	"github.com/weavster-dev/weavster/internal/serverconfig"
)

// TestAuditLog proves administrative actions, protected-content reads, and
// logins are written to the audit log with sensitive parameters redacted.
// TestAuditActionNames pins the gateway's audit action names to the audit
// package's, which the hexagonal rule keeps the gateway from importing.
func TestAuditActionNames(t *testing.T) {
	if gateway.AuditLogin != audit.ActionLogin || gateway.AuditPHIAccess != audit.ActionPHIAccess {
		t.Errorf("gateway actions %q/%q != audit actions %q/%q", gateway.AuditLogin, gateway.AuditPHIAccess, audit.ActionLogin, audit.ActionPHIAccess)
	}
}

func TestAuditLog(t *testing.T) {
	logs := &syncBuffer{}
	handler, closeStore, err := buildServer(context.Background(), slog.New(slog.NewTextHandler(logs, nil)), logs, serverconfig.Default())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = closeStore() }()
	ts := httptest.NewServer(handler)
	defer ts.Close()
	c := apiClient{t: t, base: ts.URL}
	admin := basic(bootstrapAdmin, testAdminPassword)

	c.do(http.MethodPost, "/api/v1/flows", `{"id":"lab","name":"Lab"}`, admin)
	c.do(http.MethodDelete, "/api/v1/flows/lab", "", admin)
	c.do(http.MethodGet, "/api/v1/messages?status=sent&apiToken=s3cr3t-value", "", admin)
	c.do(http.MethodGet, "/api/v1/flows", "", basic(bootstrapAdmin, "wrong-password"))
	c.login(bootstrapAdmin, testAdminPassword)

	out := logs.String()
	for _, want := range []string{
		`actor=admin action="POST /api/v1/flows" resource=/api/v1/flows detail=map[status:201]`,
		`actor=admin action="DELETE /api/v1/flows/{id}" resource=/api/v1/flows/lab detail=map[status:204]`,
		`actor=admin action=phi.access resource=/api/v1/messages detail="map[query.apiToken:[redacted] query.status:sent status:200]"`,
		`actor=admin action=auth.failure resource=/api/v1/flows detail=map[status:401]`,
		`actor=admin action=auth.login resource=/api/v1/auth/login detail=map[status:200]`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("audit log missing %s", want)
		}
	}
	for _, secret := range []string{"s3cr3t-value", "wrong-password", testAdminPassword} {
		if strings.Contains(out, secret) {
			t.Errorf("audit log leaked %q", secret)
		}
	}
	if strings.Contains(out, `action="GET /api/v1/flows"`) {
		t.Error("plain reads should not be audited")
	}
}
