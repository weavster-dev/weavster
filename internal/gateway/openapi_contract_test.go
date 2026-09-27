package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/go-chi/chi/v5"

	"github.com/weavster-dev/weavster/internal/flowdef"
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

// TestAllFlowRoutesReserved: every static /flows/<word> route is a
// reserved flow id, so it cannot shadow a flow.
func TestAllFlowRoutesReserved(t *testing.T) {
	words := []string{"stats", "export", "import", "redeploy-all", "connector-names", "ports-in-use"}
	for _, action := range AllFlowActions {
		words = append(words, action+"-all")
	}
	for _, w := range words {
		if !flowdef.Reserved(w) {
			t.Errorf("%q names a route but is not a reserved flow id in flow.schema.json", w)
		}
	}
}

// eachResponse calls fn for every documented response of every operation.
func eachResponse(doc *openapi3.T, fn func(where, code string, r *openapi3.Response)) {
	for path, item := range doc.Paths.Map() {
		for method, op := range item.Operations() {
			for code, r := range op.Responses.Map() {
				if r.Value != nil {
					fn(method+" "+path+" "+code, code, r.Value)
				}
			}
		}
	}
}

// TestOpenAPIResponsesHaveSchemas: every documented success response other
// than 204 describes its body with a schema, so clients can be generated
// from the contract.
func TestOpenAPIResponsesHaveSchemas(t *testing.T) {
	var missing []string
	eachResponse(loadSpec(t), func(where, code string, r *openapi3.Response) {
		if code[0] != '2' || code == "204" {
			return
		}
		ok := len(r.Content) > 0
		for _, mt := range r.Content {
			ok = ok && mt.Schema != nil
		}
		if !ok {
			missing = append(missing, where)
		}
	})
	sort.Strings(missing)
	for _, m := range missing {
		t.Errorf("%s: success response without a schema", m)
	}
}

// TestOpenAPIFlowSchemaMatchesFlowdef: the Flow and FlowDestination
// schemas list exactly the JSON fields of flowdef.Flow and
// flowdef.Destination, so a new field cannot be left out of the contract.
func TestOpenAPIFlowSchemaMatchesFlowdef(t *testing.T) {
	doc := loadSpec(t)
	for name, v := range map[string]any{"Flow": flowdef.Flow{}, "FlowSource": flowdef.Source{}, "FlowDestination": flowdef.Destination{}} {
		var fields []string
		rt := reflect.TypeOf(v)
		for i := 0; i < rt.NumField(); i++ {
			if tag, _, _ := strings.Cut(rt.Field(i).Tag.Get("json"), ","); tag != "" && tag != "-" {
				fields = append(fields, tag)
			}
		}
		var props []string
		for p := range doc.Components.Schemas[name].Value.Properties {
			props = append(props, p)
		}
		sort.Strings(fields)
		sort.Strings(props)
		if strings.Join(fields, ",") != strings.Join(props, ",") {
			t.Errorf("%s schema properties %v, Go JSON fields %v", name, props, fields)
		}
	}
}

// hasExample reports whether a JSON body is illustrated: an example on the
// media type, on its schema, or (for a list) on the item schema.
func hasExample(mt *openapi3.MediaType) bool {
	if mt.Example != nil || len(mt.Examples) > 0 {
		return true
	}
	if mt.Schema == nil || mt.Schema.Value == nil {
		return false
	}
	s := mt.Schema.Value
	return s.Example != nil || (s.Items != nil && s.Items.Value != nil && s.Items.Value.Example != nil)
}

// TestOpenAPIExamples: every operation that returns a JSON body shows an
// example of it, and the error response has one.
func TestOpenAPIExamples(t *testing.T) {
	doc := loadSpec(t)
	var missing []string
	eachResponse(doc, func(where, code string, r *openapi3.Response) {
		if mt := r.Content.Get("application/json"); code[0] == '2' && mt != nil && !hasExample(mt) {
			missing = append(missing, where)
		}
	})
	sort.Strings(missing)
	for _, m := range missing {
		t.Errorf("%s: JSON response without an example", m)
	}
	if mt := doc.Components.Responses["Error"].Value.Content.Get("application/json"); mt == nil || !hasExample(mt) {
		t.Error("the Error response has no example")
	}
}

func init() {
	// Check these formats in examples too (kin-openapi checks only a few
	// by default).
	openapi3.DefineStringFormat("uuid", openapi3.FormatOfStringForUUIDOfRFC4122)
	openapi3.DefineStringFormat("email", openapi3.FormatOfStringForEmail)
	openapi3.DefineStringFormat("uri", `^[A-Za-z][A-Za-z0-9+.-]*://[^\s]+$`)
}

// TestOpenAPIExamplesMatchSchemas: every example — on a schema, a media
// type, or in an examples map — is valid for its schema, and every flow in
// an example passes the server's flow validation (flow.schema.json).
func TestOpenAPIExamplesMatchSchemas(t *testing.T) {
	doc := loadSpec(t)
	for name, s := range doc.Components.Schemas {
		ex := s.Value.Example
		if ex == nil {
			t.Errorf("schema %s has no example", name)
			continue
		}
		if err := s.Value.VisitJSON(ex); err != nil {
			t.Errorf("schema %s: example does not match: %v", name, err)
		}
	}
	check := func(where string, mt *openapi3.MediaType) {
		if mt == nil || mt.Schema == nil || mt.Schema.Value == nil {
			return
		}
		// Examples on an inline schema (or its item schema) count too.
		for which, sch := range map[string]*openapi3.SchemaRef{"schema example": mt.Schema, "items example": mt.Schema.Value.Items} {
			if sch != nil && sch.Value != nil && sch.Value.Example != nil {
				if err := sch.Value.VisitJSON(sch.Value.Example); err != nil {
					t.Errorf("%s %s: does not match: %v", where, which, err)
				}
			}
		}
		values := map[string]any{"example": mt.Example}
		for name, e := range mt.Examples {
			if e.Value != nil {
				values["examples."+name] = e.Value.Value
			}
		}
		for which, v := range values {
			if v == nil {
				continue
			}
			if err := mt.Schema.Value.VisitJSON(v); err != nil {
				t.Errorf("%s %s: does not match: %v", where, which, err)
			}
		}
	}
	for path, item := range doc.Paths.Map() {
		for method, op := range item.Operations() {
			if op.RequestBody != nil && op.RequestBody.Value != nil {
				check(method+" "+path+" request", op.RequestBody.Value.Content.Get("application/json"))
			}
		}
	}
	eachResponse(doc, func(where, _ string, r *openapi3.Response) { check(where, r.Content.Get("application/json")) })
	check("Error response", doc.Components.Responses["Error"].Value.Content.Get("application/json"))

	// Flows in examples are flows the server accepts.
	validFlow := func(where string, f any) {
		m, ok := f.(map[string]any)
		if !ok {
			t.Errorf("%s: not a flow object", where)
			return
		}
		def := map[string]any{}
		for k, v := range m {
			if k != "status" && k != "stoppedDestinations" { // runtime fields
				def[k] = v
			}
		}
		b, _ := json.Marshal(def)
		if err := flowdef.ValidateJSON(b); err != nil {
			t.Errorf("%s: %v", where, err)
		}
	}
	validFlow("Flow example", doc.Components.Schemas["Flow"].Value.Example)
	for _, name := range []string{"FlowBundle", "ConfigBundle"} {
		for i, f := range doc.Components.Schemas[name].Value.Example.(map[string]any)["flows"].([]any) {
			validFlow(fmt.Sprintf("%s example flows[%d]", name, i), f)
		}
	}
	for _, key := range []string{"POST /api/v1/flows", "PUT /api/v1/flows/{id}"} {
		method, path, _ := strings.Cut(key, " ")
		validFlow(key+" request example", doc.Paths.Find(path).GetOperation(method).RequestBody.Value.Content.Get("application/json").Example)
	}
	for i, f := range doc.Paths.Find("/api/v1/flows").Put.RequestBody.Value.Content.Get("application/json").Example.(map[string]any)["flows"].([]any) {
		validFlow(fmt.Sprintf("PUT /api/v1/flows request example flows[%d]", i), f)
	}
}

// TestOpenAPIRequestExamples: the main JSON request bodies show an example.
func TestOpenAPIRequestExamples(t *testing.T) {
	doc := loadSpec(t)
	for _, key := range []string{
		"POST /api/v1/auth/login", "POST /api/v1/flows", "PUT /api/v1/flows", "PUT /api/v1/flows/{id}",
		"POST /api/v1/flows/{id}/messages", "POST /api/v1/users", "PUT /api/v1/configmap", "PUT /api/v1/configmap/{name}",
		"PUT /api/v1/scripts/{name}", "PUT /api/v1/settings/{name}", "PUT /api/v1/lookups/{group}/{key}",
	} {
		method, path, _ := strings.Cut(key, " ")
		op := doc.Paths.Find(path).GetOperation(method)
		if op == nil || op.RequestBody == nil || !hasExample(op.RequestBody.Value.Content.Get("application/json")) {
			t.Errorf("%s: request body without an example", key)
		}
	}
}

// TestOpenAPIFlowSourceVariants: the FlowSource component accepts a file or
// an http source with its own required fields, like flow.schema.json.
func TestOpenAPIFlowSourceVariants(t *testing.T) {
	schema := loadSpec(t).Components.Schemas["FlowSource"].Value
	for _, tt := range []struct {
		doc string
		ok  bool
	}{
		{`{"type":"file","dir":"/in","pattern":"*.json"}`, true},
		{`{"type":"http","address":":9001","path":"/adt","method":"PUT"}`, true},
		{`{"type":"file"}`, false},
		{`{"type":"http"}`, false},
		{`{"type":"http","address":":9001","dir":"/in"}`, false},
		{`{"type":"file","dir":"/in","method":"POST"}`, false},
		{`{"type":"file","dir":"/in","recursive":true}`, false},
		{`{"type":"http","address":":9001","username":"lab","passwordEnv":"WEAVSTER_SOURCE_LAB","certFile":"/c","keyFile":"/k"}`, true},
		{`{"type":"http","address":":9001","username":"lab"}`, false},
		{`{"type":"http","address":":9001","keyFile":"/k"}`, false},
		{`{"type":"http","address":":9001","certFile":"","keyFile":""}`, false},
		{`{"type":"file","dir":"/in","username":"lab","passwordEnv":"WEAVSTER_SOURCE_LAB"}`, false},
		{`{"type":"http","address":":9001","username":"lab","passwordEnv":"DATABASE_URL"}`, false},
		{`{"type":"http","address":":9001","username":"a:b","passwordEnv":"WEAVSTER_SOURCE_LAB"}`, false},
	} {
		var v any
		if err := json.Unmarshal([]byte(tt.doc), &v); err != nil {
			t.Fatal(err)
		}
		if err := schema.VisitJSON(v); (err == nil) != tt.ok {
			t.Errorf("%s: %v", tt.doc, err)
		}
	}
}
