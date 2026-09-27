package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/weavster-dev/weavster/internal/gateway"
	"github.com/weavster-dev/weavster/internal/serverconfig"
)

// TestOpenAPIRepliesMatchSchemas makes a successful call to each operation
// whose reply the generic contract test cannot reach with an empty body,
// and checks the reply against the documented schema for its status.
func TestOpenAPIRepliesMatchSchemas(t *testing.T) {
	doc, err := openapi3.NewLoader().LoadFromData([]byte(gateway.OpenAPISpec()))
	if err != nil {
		t.Fatal(err)
	}
	c := startComposed(t, serverconfig.Default(), io.Discard)
	admin := basic(bootstrapAdmin, testAdminPassword)
	dir := t.TempDir()
	check := func(method, template, url, body string, want int) string {
		t.Helper()
		code, reply, _ := c.do(method, url, body, admin)
		if code != want {
			t.Fatalf("%s %s: %d %.300s, want %d", method, url, code, reply, want)
		}
		op := doc.Paths.Find(template).GetOperation(method)
		mt := op.Responses.Status(code).Value.Content.Get("application/json")
		var v any
		if err := json.Unmarshal([]byte(reply), &v); err != nil {
			t.Fatalf("%s %s: reply is not JSON: %v", method, url, err)
		}
		if err := mt.Schema.Value.VisitJSON(v); err != nil {
			t.Errorf("%s %s: reply does not match the schema: %v\n%.400s", method, url, err, reply)
		}
		return reply
	}
	flow := `{"id":"adt","name":"ADT","enabled":true,"destinations":[{"name":"out","type":"file","dir":"` + dir + `"}]}`

	check(http.MethodPost, "/api/v1/auth/login", "/api/v1/auth/login", `{"username":"`+bootstrapAdmin+`","password":"`+testAdminPassword+`"}`, http.StatusOK)
	check(http.MethodPost, "/api/v1/flows", "/api/v1/flows", flow, http.StatusCreated)
	check(http.MethodGet, "/api/v1/flows/{id}/stats", "/api/v1/flows/adt/stats", "", http.StatusOK) // no message yet
	check(http.MethodPut, "/api/v1/flows", "/api/v1/flows", `{"flows":[`+flow+`]}`, http.StatusOK)
	check(http.MethodPost, "/api/v1/flows/{id}/{action}", "/api/v1/flows/adt/deploy", "", http.StatusOK)
	check(http.MethodPost, "/api/v1/flows/{id}/{action}", "/api/v1/flows/adt/start", "", http.StatusOK)
	reply := check(http.MethodPost, "/api/v1/flows/{id}/messages", "/api/v1/flows/adt/messages", `{"k":"v"}`, http.StatusAccepted)
	var ingest struct{ ID string }
	_ = json.Unmarshal([]byte(reply), &ingest)
	check(http.MethodGet, "/api/v1/messages", "/api/v1/messages", "", http.StatusOK)
	check(http.MethodGet, "/api/v1/messages/{id}", "/api/v1/messages/"+ingest.ID, "", http.StatusOK)
	check(http.MethodPost, "/api/v1/messages/{id}/reprocess", "/api/v1/messages/"+ingest.ID+"/reprocess", "", http.StatusAccepted)
	check(http.MethodGet, "/api/v1/flows/{id}/stats", "/api/v1/flows/adt/stats", "", http.StatusOK) // with lastMessageAt
	check(http.MethodGet, "/api/v1/flows/stats", "/api/v1/flows/stats", "", http.StatusOK)
	check(http.MethodGet, "/api/v1/topology", "/api/v1/topology", "", http.StatusOK)
	check(http.MethodGet, "/api/v1/topology/flows/{flowId}", "/api/v1/topology/flows/adt", "", http.StatusOK)
	check(http.MethodPost, "/api/v1/flows/{id}/destinations/{dest}/{action}", "/api/v1/flows/adt/destinations/out/stop", "", http.StatusOK)
	check(http.MethodPost, "/api/v1/flows/{id}/disable", "/api/v1/flows/adt/disable", "", http.StatusOK)
	check(http.MethodPost, "/api/v1/flows/{id}/enable", "/api/v1/flows/adt/enable", "", http.StatusOK)
	check(http.MethodPost, "/api/v1/flows/stop-all", "/api/v1/flows/stop-all", "", http.StatusOK)
	check(http.MethodPost, "/api/v1/flows/redeploy-all", "/api/v1/flows/redeploy-all", "", http.StatusOK)
	bundle := check(http.MethodGet, "/api/v1/flows/export", "/api/v1/flows/export", "", http.StatusOK)
	check(http.MethodPost, "/api/v1/flows/import", "/api/v1/flows/import?overwrite=true", bundle, http.StatusOK)
	_, archive, _ := c.do(http.MethodGet, "/api/v1/messages/export", "", admin)
	check(http.MethodPost, "/api/v1/messages/import", "/api/v1/messages/import", archive, http.StatusOK)
	for _, kind := range []string{"configmap", "scripts", "settings"} {
		check(http.MethodPut, "/api/v1/"+kind+"/{name}", "/api/v1/"+kind+"/region", `{"value":"eu"}`, http.StatusOK)
		check(http.MethodGet, "/api/v1/"+kind+"/{name}", "/api/v1/"+kind+"/region", "", http.StatusOK)
		check(http.MethodPut, "/api/v1/"+kind, "/api/v1/"+kind, `{"region":"us"}`, http.StatusOK)
		check(http.MethodGet, "/api/v1/"+kind, "/api/v1/"+kind, "", http.StatusOK)
	}
	check(http.MethodPost, "/api/v1/users", "/api/v1/users", `{"username":"ops","password":"Ops-Passw0rd-1","email":"ops@example.com","permissions":["flows:view"]}`, http.StatusCreated)
	check(http.MethodPut, "/api/v1/users/{name}", "/api/v1/users/ops", `{"org":"Ops","permissions":["flows:view"]}`, http.StatusOK)
	if !strings.HasPrefix(check(http.MethodGet, "/api/v1/users", "/api/v1/users", "", http.StatusOK), "[") {
		t.Error("user list is not an array")
	}
}
