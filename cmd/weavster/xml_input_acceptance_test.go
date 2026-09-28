package main

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestXMLInput: a flow with inputFormat xml transforms XML documents sent
// to it with element paths and delivers JSON; malformed XML is refused and
// DTD entities are never expanded.
func TestXMLInput(t *testing.T) {
	addr := freeAddr(t)
	cfg := writeConfig(t, "listen: {address: \""+addr+"\"}\n"+storeConfig(t))
	stop := startCLI(t, []string{"server", "--config", cfg}, "http://"+addr+"/api/openapi.yaml")
	defer stop()
	c := apiClient{t: t, base: "http://" + addr}
	admin := basic(bootstrapAdmin, testAdminPassword)
	out := t.TempDir()
	createFlow(t, c, `{"id":"orders","inputFormat":"xml","transform":{"name":"t","steps":[`+
		`{"map":{"from":"order.@id","to":"order.id"}},`+
		`{"map":{"from":"order.patient.name.#text","to":"patient"}},`+
		`{"map":{"from":"order.item.1.@sku","to":"secondSku"}}]},`+
		`"destinations":[{"name":"out","type":"file","dir":"`+out+`"}]}`)
	doc := `<?xml version="1.0"?><order id="42" xmlns="urn:orders"><patient><name>DOE</name></patient><item sku="A"/><item sku="B"/></order>`
	if code, body, _ := c.do(http.MethodPost, "/api/v1/flows/orders/messages", doc, admin); code != http.StatusAccepted {
		t.Fatalf("send: %d %s", code, body)
	}
	entries, _ := os.ReadDir(out)
	if len(entries) != 1 {
		t.Fatalf("delivered %d files", len(entries))
	}
	got, _ := os.ReadFile(filepath.Join(out, entries[0].Name()))
	for _, want := range []string{`"patient":"DOE"`, `"secondSku":"B"`, `"id":"42"`} {
		if !strings.Contains(string(got), want) {
			t.Errorf("delivered %s, missing %s", got, want)
		}
	}
	for body, want := range map[string]string{
		`<order><unclosed></order>`: "body must be a single well-formed XML document",
		`{"order":{}}`:              "body must be a single well-formed XML document",
		`<!DOCTYPE a [<!ENTITY x SYSTEM "file:///etc/passwd">]><a>&x;</a>`: "body must be a single well-formed XML document",
	} {
		if code, resp, _ := c.do(http.MethodPost, "/api/v1/flows/orders/messages", body, admin); code != http.StatusBadRequest || !strings.Contains(resp, want) || strings.Contains(resp, "passwd") {
			t.Errorf("%s: %d %s", body, code, resp)
		}
	}
}
