package codecs

import (
	"fmt"
	"strings"
	"testing"
)

func namespaceDocument(n, children int) []byte {
	var b strings.Builder
	b.WriteString("<r")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, ` xmlns:p%d="urn:%d"`, i, i)
	}
	b.WriteString(">")
	for i := 0; i < children; i++ {
		b.WriteString(`<p0:c xmlns:local="urn:local"/>`)
	}
	b.WriteString("</r>")
	return []byte(b.String())
}

func BenchmarkXMLJSONNamespaces(b *testing.B) {
	for _, tc := range []struct{ n, children int }{{1000, 0}, {2000, 0}, {1000, 1000}} {
		b.Run(fmt.Sprintf("declarations=%d/children=%d", tc.n, tc.children), func(b *testing.B) {
			in := namespaceDocument(tc.n, tc.children)
			b.ReportAllocs()
			b.SetBytes(int64(len(in)))
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := XMLJSON(in); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestXMLJSONNamespaceAllocation(t *testing.T) {
	// A small XML body must not copy its inherited namespace map for every
	// declaration or child. Use allocated bytes, not a wall-clock threshold.
	in := namespaceDocument(1000, 1000)
	result := testing.Benchmark(func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := XMLJSON(in); err != nil {
				b.Fatal(err)
			}
		}
	})
	if got := result.AllocedBytesPerOp(); got > 16<<20 {
		t.Fatalf("%d-byte XML allocated %d bytes; want at most 16 MiB", len(in), got)
	}
}

func TestXMLJSONNamespaceScopes(t *testing.T) {
	in := `<r xmlns="urn:root" xmlns:p="urn:outer" xmlns:q="urn:q">
 <p:before/>
 <scope xmlns="" xmlns:p="urn:inner" xmlns:local="urn:local">
  <p:inside q:id="1"/><plain/><local:only/>
  <nested xmlns:p="urn:deep"><p:deep/></nested><p:restored/>
 </scope>
 <p:after/><plain/>
 </r>`
	doc := view(t, in)
	for path, want := range map[string]any{
		"r.#ns":                   "urn:root",
		"r.before.#ns":            "urn:outer",
		"r.scope.#ns":             nil,
		"r.scope.inside.#ns":      "urn:inner",
		"r.scope.inside.@q:id":    "1",
		"r.scope.plain.#ns":       nil,
		"r.scope.only.#ns":        "urn:local",
		"r.scope.nested.deep.#ns": "urn:deep",
		"r.scope.restored.#ns":    "urn:inner",
		"r.after.#ns":             "urn:outer",
		"r.plain.#ns":             "urn:root",
	} {
		if got := lookup(doc, path); got != want {
			t.Errorf("%s = %v, want %v", path, got, want)
		}
	}
	for _, in := range []string{
		`<r><child xmlns:p="urn:local"/><p:outside/></r>`,
		`<r><child xmlns:p="urn:local"/><outside p:id="1"/></r>`,
	} {
		if _, err := XMLJSON([]byte(in)); err == nil {
			t.Fatal("child namespace leaked to sibling")
		}
	}
}
