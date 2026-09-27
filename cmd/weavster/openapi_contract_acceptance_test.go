package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/weavster-dev/weavster/internal/gateway"
	"github.com/weavster-dev/weavster/internal/serverconfig"
)

// TestOpenAPIContract calls every documented operation on the composed
// server. Each must reach its handler (never the router's own 404/405),
// answer a status the operation documents explicitly, and use the error
// envelope for every error.
func TestOpenAPIContract(t *testing.T) {
	doc, err := openapi3.NewLoader().LoadFromData([]byte(gateway.OpenAPISpec()))
	if err != nil {
		t.Fatal(err)
	}
	c := startComposed(t, serverconfig.Default(), io.Discard)
	admin := basic(bootstrapAdmin, testAdminPassword)
	if code, body, _ := c.do(http.MethodPost, "/api/v1/flows", `{"id":"f","destinations":[{"name":"d","type":"file","dir":"`+t.TempDir()+`"}]}`, admin); code != http.StatusCreated {
		t.Fatalf("create: %d %q", code, body)
	}
	// Values for path parameters without an enum, naming things that exist
	// so operations reach their logic.
	samples := map[string]string{"id": "f", "flowId": "f", "dest": "d"}
	var ops []string
	for path, item := range doc.Paths.Map() {
		for method := range item.Operations() {
			ops = append(ops, method+" "+path)
		}
	}
	// Deletes run last, so the other operations find the flow.
	sort.Slice(ops, func(i, j int) bool {
		di, dj := strings.HasPrefix(ops[i], http.MethodDelete), strings.HasPrefix(ops[j], http.MethodDelete)
		if di != dj {
			return dj
		}
		return ops[i] < ops[j]
	})
	for _, key := range ops {
		method, path, _ := strings.Cut(key, " ")
		op := doc.Paths.Find(path).GetOperation(method)
		t.Run(key, func(t *testing.T) {
			url := path
			params := append(doc.Paths.Find(path).Parameters, op.Parameters...)
			for _, ref := range params {
				p := ref.Value
				if p.In != openapi3.ParameterInPath {
					continue
				}
				v, ok := samples[p.Name]
				if p.Schema != nil && p.Schema.Value != nil && len(p.Schema.Value.Enum) > 0 {
					v, ok = fmt.Sprint(p.Schema.Value.Enum[0]), true
				}
				if !ok {
					t.Fatalf("no value for path parameter %q; add one to samples", p.Name)
				}
				url = strings.ReplaceAll(url, "{"+p.Name+"}", v)
			}
			if strings.Contains(url, "{") {
				t.Fatalf("path %s has a placeholder that is not a documented path parameter", url)
			}
			body := ""
			if method == http.MethodPost || method == http.MethodPut {
				body = "{}"
			}
			status, reply, _ := c.do(method, url, body, admin)
			var env struct {
				Error struct{ Code, Message string } `json:"error"`
			}
			isEnvelope := json.Unmarshal([]byte(reply), &env) == nil && env.Error.Code != ""
			switch {
			case isEnvelope && (env.Error.Message == gateway.RouteNotFoundMessage || status == http.StatusMethodNotAllowed):
				t.Errorf("%s %s did not reach a handler: %d %s", method, url, status, reply)
			case status >= 400 && !isEnvelope:
				t.Errorf("%s %s: error %d without the envelope: %q", method, url, status, reply)
			case op.Responses.Status(status) == nil:
				t.Errorf("%s %s: %d is not documented for this operation", method, url, status)
			}
			t.Logf("%s %s -> %s", method, url, strconv.Itoa(status))
		})
	}
}
