package gateway

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/go-chi/chi/v5"
)

// TestOpenAPIPublished keeps agent-docs/openapi.yaml identical to the
// embedded contract (single source of truth).
func TestOpenAPIPublished(t *testing.T) {
	published, err := os.ReadFile("../../agent-docs/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(published, []byte(OpenAPISpec())) {
		t.Error("agent-docs/openapi.yaml differs from internal/gateway/openapi.yaml; run go generate ./internal/gateway")
	}
}

func loadSpec(t *testing.T) *openapi3.T {
	t.Helper()
	doc, err := openapi3.NewLoader().LoadFromData([]byte(OpenAPISpec()))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

// TestOpenAPIValid validates the contract with kin-openapi.
func TestOpenAPIValid(t *testing.T) {
	if err := loadSpec(t).Validate(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// TestOpenAPIMatchesRoutes: every route the router serves is documented,
// and every documented operation is routed.
func TestOpenAPIMatchesRoutes(t *testing.T) {
	documented := map[string]bool{}
	for path, item := range loadSpec(t).Paths.Map() {
		for method := range item.Operations() {
			documented[method+" "+path] = true
		}
	}
	routed := map[string]bool{}
	mux, ok := New(Config{}).Router().(*chi.Mux)
	if !ok {
		t.Fatal("router is not a chi mux")
	}
	err := chi.Walk(mux, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		routed[method+" "+strings.TrimSuffix(route, "/")] = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var missing, extra []string
	for r := range routed {
		if !documented[r] {
			missing = append(missing, r)
		}
	}
	for d := range documented {
		if !routed[d] {
			extra = append(extra, d)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) > 0 || len(extra) > 0 {
		t.Errorf("routes not in openapi.yaml: %v\ndocumented but not routed: %v", missing, extra)
	}
}

// TestOpenAPIVersionHeader: every response of a versioned operation declares
// the Weavster-API-Version header the server sends.
func TestOpenAPIVersionHeader(t *testing.T) {
	for path, item := range loadSpec(t).Paths.Map() {
		if path == "/api/openapi.yaml" {
			continue
		}
		for method, op := range item.Operations() {
			for status, resp := range op.Responses.Map() {
				if resp.Value == nil || resp.Value.Headers["Weavster-API-Version"] == nil {
					t.Errorf("%s %s %s: no Weavster-API-Version header", method, path, status)
				}
			}
		}
	}
}
