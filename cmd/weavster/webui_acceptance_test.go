package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/weavster-dev/weavster/internal/gateway"
	"github.com/weavster-dev/weavster/internal/topology"
	"github.com/weavster-dev/weavster/internal/webui"
)

// TestWebUI: the binary serves the read-only UI without credentials, with
// its CSP; / leads to it; the calls the page makes (sign in, read the
// topology through ../api/v1 relative to the page) work, and need
// flows:view; the same holds under listen.contextPath.
func TestWebUI(t *testing.T) {
	for _, prefix := range []string{"", "/weavster"} {
		t.Run("prefix="+prefix, func(t *testing.T) {
			addr := freeAddr(t)
			listen := "listen: {address: \"" + addr + "\"}\n"
			if prefix != "" {
				listen = "listen: {address: \"" + addr + "\", contextPath: \"" + prefix + "\"}\n"
			}
			cfg := writeConfig(t, listen+storeConfig(t))
			base := "http://" + addr + prefix
			stop := startCLI(t, []string{"server", "--config", cfg}, base+"/api/openapi.yaml")
			defer stop()
			noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
			get := func(path string) (*http.Response, string) {
				t.Helper()
				res, err := noRedirect.Get(base + path)
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = res.Body.Close() }()
				b, _ := io.ReadAll(res.Body)
				return res, string(b)
			}

			if res, _ := get("/"); res.StatusCode != http.StatusFound || res.Header.Get("Location") != prefix+"/ui/" {
				t.Errorf("GET / = %d %q", res.StatusCode, res.Header.Get("Location"))
			}
			res, page := get("/ui/")
			if res.StatusCode != http.StatusOK || !strings.Contains(page, `<script src="app.js" defer></script>`) ||
				res.Header.Get("Content-Security-Policy") != webui.CSP || res.Header.Get("X-Frame-Options") != "DENY" {
				t.Fatalf("GET /ui/ = %d %v %.80s", res.StatusCode, res.Header, page)
			}
			for _, asset := range []string{"app.js", "app.css"} {
				if res, _ := get("/ui/" + asset); res.StatusCode != http.StatusOK {
					t.Errorf("GET /ui/%s = %d", asset, res.StatusCode)
				}
			}

			// The page's own calls: its API root is ../api/v1/ from /ui/.
			api, _ := url.Parse(base + "/ui/")
			api = api.ResolveReference(&url.URL{Path: "../api/v1/"})
			c := apiClient{t: t, base: base}
			admin := basic(bootstrapAdmin, testAdminPassword)
			if code, body, _ := c.do(http.MethodPost, "/api/v1/users", `{"username":"viewer","password":"View-Pass-1","permissions":["flows:view"],"mustChangePassword":false}`, admin); code != http.StatusCreated {
				t.Fatalf("create viewer: %d %s", code, body)
			}
			if code, body, _ := c.do(http.MethodPost, "/api/v1/users", `{"username":"nobody","password":"None-Pass-1","permissions":[],"mustChangePassword":false}`, admin); code != http.StatusCreated {
				t.Fatalf("create nobody: %d %s", code, body)
			}
			createFlow(t, c, `{"id":"adt","destinations":[{"name":"out","type":"file","dir":"`+t.TempDir()+`"}]}`)
			pageCall := func(method, path, token, body string) (int, string) {
				t.Helper()
				req, _ := http.NewRequest(method, api.String()+path, strings.NewReader(body))
				req.Header.Set(gateway.MarkerHeader, gateway.MarkerValue)
				if token != "" {
					req.Header.Set("Authorization", "Bearer "+token)
				}
				res, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = res.Body.Close() }()
				b, _ := io.ReadAll(res.Body)
				return res.StatusCode, string(b)
			}
			login := func(user, pass string) string {
				t.Helper()
				code, body := pageCall(http.MethodPost, "auth/login", "", `{"username":"`+user+`","password":"`+pass+`"}`)
				var out struct{ Token string }
				if code != http.StatusOK || json.Unmarshal([]byte(body), &out) != nil || out.Token == "" {
					t.Fatalf("login %s: %d %s", user, code, body)
				}
				return out.Token
			}
			viewer := login("viewer", "View-Pass-1")
			for _, path := range []string{"topology", "topology/flows/adt"} {
				code, body := pageCall(http.MethodGet, path, viewer, "")
				if code != http.StatusOK || topology.Validate([]byte(body)) != nil || !strings.Contains(body, `"flow:adt"`) {
					t.Errorf("GET %s as viewer = %d %s", path, code, body)
				}
			}
			if code, _ := pageCall(http.MethodGet, "topology", login("nobody", "None-Pass-1"), ""); code != http.StatusForbidden {
				t.Errorf("GET topology without flows:view = %d", code)
			}
			if code, _ := pageCall(http.MethodGet, "topology", "", ""); code != http.StatusUnauthorized {
				t.Errorf("GET topology signed out = %d", code)
			}
			if code, _ := pageCall(http.MethodPost, "auth/logout", viewer, ""); code != http.StatusNoContent {
				t.Errorf("sign out = %d", code)
			}
			if code, _ := pageCall(http.MethodGet, "topology", viewer, ""); code != http.StatusUnauthorized {
				t.Errorf("GET topology after signing out = %d", code)
			}
		})
	}
}
