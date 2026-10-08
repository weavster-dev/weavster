package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// TestProcessingOrder: the flow's steps run in the order written on the
// input; each destination's steps then run in order on the flow's output;
// the response transform's steps run in order on the selected destination's
// reply. Each case writes the same steps in two orders with different
// outcomes.
func TestProcessingOrder(t *testing.T) {
	var mu sync.Mutex
	var bodies []string
	ehr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(b))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":"OK"}`))
	}))
	defer ehr.Close()
	addr := freeAddr(t)
	cfg := writeConfig(t, "listen: {address: \""+addr+"\"}\n"+storeConfig(t))
	stop := startCLI(t, []string{"server", "--config", cfg}, "http://"+addr+"/api/openapi.yaml")
	defer stop()
	c := apiClient{t: t, base: "http://" + addr}
	admin := basic(bootstrapAdmin, testAdminPassword)

	mapAdult := `{"map":{"from":"age.flag","to":"adult"}}`
	adultOnly := `{"filter":{"when":"adult == 'y'","action":"accept"}}`
	for _, tt := range []struct {
		name      string
		flow      string // steps of the flow transform
		dest      string // steps of the destination transform
		reply     string // steps of the response transform
		status    string // the message's status
		delivered string // what the EHR received ("" = nothing)
		response  string // the reply returned to the sender ("" = none)
	}{
		{"flow: map, then filter", mapAdult + `,` + adultOnly, ``, ``, "sent", `{"adult":"y","age":{"flag":"y"}}`, `{"code":"OK"}`},
		{"flow: filter, then map", adultOnly + `,` + mapAdult, ``, ``, "filtered", "", ""},
		{"destination: set, then filter on it", mapAdult,
			`{"set":{"field":"to","expr":"ehr"}},{"filter":{"when":"to == 'ehr'","action":"accept"}}`, ``,
			"sent", `{"adult":"y","age":{"flag":"y"},"to":"ehr"}`, `{"code":"OK"}`},
		{"destination: filter on it, then set", mapAdult,
			`{"filter":{"when":"to == 'ehr'","action":"accept"}},{"set":{"field":"to","expr":"ehr"}}`, ``,
			"filtered", "", ""},
		{"destination: filter on the flow's output", mapAdult, adultOnly, ``, "sent", `{"adult":"y","age":{"flag":"y"}}`, `{"code":"OK"}`},
		{"response: map, then set from it", ``, ``,
			`{"map":{"from":"code","to":"ack"}},{"set":{"field":"text","expr":"ack {{ack}}"}}`,
			"sent", `{"age":{"flag":"y"}}`, `{"ack":"OK","code":"OK","text":"ack OK"}`},
		{"response: set, then map", ``, ``,
			`{"set":{"field":"text","expr":"ack {{ack}}"}},{"map":{"from":"code","to":"ack"}}`,
			"sent", `{"age":{"flag":"y"}}`, `{"ack":"OK","code":"OK","text":"ack "}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			mu.Lock()
			bodies = nil
			mu.Unlock()
			id := strings.NewReplacer(" ", "-", ",", "", ":", "", "'", "").Replace(tt.name)
			dest := `{"name":"ehr","type":"http","url":"` + ehr.URL + `"`
			if tt.dest != "" {
				dest += `,"transform":{"steps":[` + tt.dest + `]}`
			}
			if tt.reply != "" {
				dest += `,"responseTransform":{"steps":[` + tt.reply + `]}`
			}
			flow := `{"id":"` + id + `","responseSelector":"ehr","destinations":[` + dest + `}]`
			if tt.flow != "" {
				flow += `,"transform":{"steps":[` + tt.flow + `]}`
			}
			createFlow(t, c, flow+`}`)
			code, resp, _ := c.do(http.MethodPost, "/api/v1/flows/"+id+"/messages", `{"age":{"flag":"y"}}`, admin)
			var res struct {
				Status   string
				Response json.RawMessage
			}
			if err := json.Unmarshal([]byte(resp), &res); err != nil || code != http.StatusAccepted {
				t.Fatalf("send: %d %s (%v)", code, resp, err)
			}
			mu.Lock()
			delivered := strings.Join(bodies, "|")
			mu.Unlock()
			if res.Status != tt.status || delivered != tt.delivered || string(res.Response) != tt.response {
				t.Errorf("status %s, delivered %q, response %s; want %s, %q, %s", res.Status, delivered, res.Response, tt.status, tt.delivered, tt.response)
			}
		})
	}
}
