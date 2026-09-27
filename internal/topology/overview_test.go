package topology

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestEmptyGraphSerializesEmptyLists: no flows, or a flow without edges,
// gives [] rather than null.
func TestEmptyGraphSerializesEmptyLists(t *testing.T) {
	for name, g := range map[string]Graph{"overview": Overview(nil), "flow": FlowInternal(FlowDetail{})} {
		b, err := json.Marshal(g)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), `"edges":[]`) || strings.Contains(string(b), "null") {
			t.Errorf("%s = %s", name, b)
		}
	}
}
