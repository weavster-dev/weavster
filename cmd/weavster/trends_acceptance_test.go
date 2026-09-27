package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/weavster-dev/weavster/internal/serverconfig"
)

// TestMessageTrends: real processed messages are counted per hour by
// status, with a flow filter, the same on the SQLite and memory stores.
func TestMessageTrends(t *testing.T) {
	for _, dialect := range []string{serverconfig.DialectSQLite, "memory"} {
		t.Run(dialect, func(t *testing.T) {
			cfg := serverconfig.Default()
			cfg.Store.Dialect = dialect
			cfg.Paths.DataDir = t.TempDir()
			c := startComposed(t, cfg, io.Discard)
			admin := basic(bootstrapAdmin, testAdminPassword)
			// The hour is taken before sending, and the range covers the next
			// hour too, so crossing an hour boundary cannot move the messages
			// out of it.
			hour := time.Now().UTC().Truncate(time.Hour)
			createFlow(t, c, `{"id":"adt","destinations":[{"name":"out","type":"file","dir":"`+t.TempDir()+`"}]}`)
			createFlow(t, c, `{"id":"conv","transform":{"name":"t","steps":[{"map":{"from":"n","to":"n","type":"number"}}]}}`)
			sendMessage(t, c, "adt", `{"k":"v"}`)
			sendMessage(t, c, "adt", `{"k":"w"}`)
			sendMessage(t, c, "conv", `{"n":"not a number"}`) // errored

			rng := "from=" + hour.Add(-time.Hour).Format(time.RFC3339) + "&to=" + hour.Add(2*time.Hour).Format(time.RFC3339)
			var buckets []struct {
				Start    time.Time
				Total    int
				Statuses map[string]int
			}
			get := func(q string) {
				t.Helper()
				code, body, _ := c.do(http.MethodGet, "/api/v1/messages/trends?"+q, "", admin)
				if err := json.Unmarshal([]byte(body), &buckets); code != http.StatusOK || err != nil {
					t.Fatalf("%s: %d %s", q, code, body)
				}
			}
			sum := func() (total, sent, errored int) {
				for _, b := range buckets {
					total, sent, errored = total+b.Total, sent+b.Statuses["sent"], errored+b.Statuses["errored"]
				}
				return
			}
			get(rng)
			if total, sent, errored := sum(); len(buckets) != 3 || buckets[0].Total != 0 || total != 3 || sent != 2 || errored != 1 || !buckets[1].Start.Equal(hour) {
				t.Errorf("all flows = %+v", buckets)
			}
			get(rng + "&flowId=conv")
			if total, _, errored := sum(); total != 1 || errored != 1 {
				t.Errorf("flow conv = %+v", buckets)
			}
			if code, body, _ := c.do(http.MethodGet, "/api/v1/messages/trends?"+rng+"&flowId=nope", "", admin); code != http.StatusNotFound || !strings.Contains(body, "flow not found") {
				t.Errorf("unknown flow: %d %s", code, body)
			}
			get("interval=day&from=" + hour.Add(-48*time.Hour).Format(time.RFC3339) + "&to=" + hour.Add(24*time.Hour).Format(time.RFC3339))
			if total, _, _ := sum(); len(buckets) != 3 || total != 3 {
				t.Errorf("days = %+v", buckets)
			}
			if code, body, _ := c.do(http.MethodGet, "/api/v1/messages/trends", "", admin); code != http.StatusBadRequest || !strings.Contains(body, "from and to are required") {
				t.Errorf("no range: %d %s", code, body)
			}
			c.do(http.MethodPost, "/api/v1/users", `{"username":"ops","password":"Ops-Passw0rd-1","permissions":["flows:view"],"mustChangePassword":false}`, admin)
			if code, body, _ := c.do(http.MethodGet, "/api/v1/messages/trends?"+rng, "", basic("ops", "Ops-Passw0rd-1")); code != http.StatusForbidden || !strings.Contains(body, "messages:view") {
				t.Errorf("ops: %d %s", code, body)
			}
		})
	}
}
