package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/weavster-dev/weavster/internal/flowdef"
	"github.com/weavster-dev/weavster/internal/gateway"
)

// flowUsage lists the flow subcommands (spec §3.2).
const flowUsage = `flow subcommands:
  flow list
  flow get <id>
  flow create <file>
  flow update <id> <file>
  flow update-all <file>
  flow rename <id> <name>
  flow enable|disable <id>
  flow remove <id>
  flow export <file> [<id>...]
  flow import <file> [--overwrite]
  flow deploy|undeploy|start|stop|pause|halt|resume <id>
  flow redeploy-all
  flow start-destination|stop-destination <id> <destination>
  flow connectors
  flow ports`

// flowCommand runs one "flow" subcommand; args excludes "flow". Replies
// from the server are printed as returned. It returns the §3.3 exit code.
func flowCommand(ctx context.Context, client Client, args []string, stdout, stderr io.Writer, debug bool) int {
	sub, rest := "", []string(nil)
	if len(args) > 0 {
		sub, rest = args[0], args[1:]
	}
	usage := func() int {
		_, _ = fmt.Fprintln(stderr, "Error: usage:\n"+flowUsage)
		return 2
	}
	call := func(method, path string, body []byte) int {
		out, err := client.Call(ctx, method, path, body)
		if err != nil {
			return shellError(stderr, debug, err)
		}
		if len(out) > 0 {
			_, _ = fmt.Fprintln(stdout, strings.TrimSpace(string(out)))
		}
		return 0
	}
	flowPath := func(id string, more ...string) string {
		p := "/api/v1/flows/" + url.PathEscape(id)
		for _, m := range more {
			p += "/" + url.PathEscape(m)
		}
		return p
	}
	withFile := func(file string, send func([]byte) int) int {
		data, err := os.ReadFile(file)
		if err != nil {
			return shellError(stderr, debug, err)
		}
		return send(data)
	}

	for _, id := range idArgs(sub, rest) {
		if flowdef.Reserved(id) {
			_, _ = fmt.Fprintf(stderr, "Error: %q is not a flow id (it names an API route)\n", id)
			return 2
		}
	}
	switch {
	case sub == "help":
		_, _ = fmt.Fprintln(stdout, flowUsage)
		return 0
	case sub == "list": // extra words are ignored, as before
		out, err := client.Call(ctx, http.MethodGet, "/api/v1/flows", nil)
		var flows []gateway.Flow
		if err == nil {
			err = json.Unmarshal(out, &flows)
		}
		if err != nil {
			return shellError(stderr, debug, err)
		}
		for _, f := range flows {
			_, _ = fmt.Fprintln(stdout, f.ID+"\t"+f.Status+"\t"+f.Name)
		}
		return 0
	case sub == "get" && len(rest) == 1:
		return call(http.MethodGet, flowPath(rest[0]), nil)
	case sub == "create" && len(rest) == 1:
		return withFile(rest[0], func(b []byte) int { return call(http.MethodPost, "/api/v1/flows", b) })
	case sub == "update" && len(rest) == 2:
		return withFile(rest[1], func(b []byte) int { return call(http.MethodPut, flowPath(rest[0]), b) })
	case sub == "update-all" && len(rest) == 1:
		return withFile(rest[0], func(b []byte) int { return call(http.MethodPut, "/api/v1/flows", b) })
	case sub == "rename" && len(rest) >= 2:
		body, err := renamed(ctx, client, flowPath(rest[0]), strings.Join(rest[1:], " "))
		if err != nil {
			return shellError(stderr, debug, err)
		}
		return call(http.MethodPut, flowPath(rest[0]), body)
	case (sub == "enable" || sub == "disable" || sub == "deploy" || sub == "undeploy" || sub == "start" ||
		sub == "stop" || sub == "pause" || sub == "halt" || sub == "resume") && len(rest) == 1:
		return call(http.MethodPost, flowPath(rest[0], sub), nil)
	case sub == "remove" && len(rest) == 1:
		if code := call(http.MethodDelete, flowPath(rest[0]), nil); code != 0 {
			return code
		}
		_, _ = fmt.Fprintf(stdout, "removed %s\n", rest[0])
		return 0
	case sub == "export" && len(rest) >= 1:
		path := "/api/v1/flows/export"
		if len(rest) > 1 {
			path += "?ids=" + url.QueryEscape(strings.Join(rest[1:], ","))
		}
		out, err := client.Call(ctx, http.MethodGet, path, nil)
		if err == nil {
			err = os.WriteFile(rest[0], out, 0o600)
		}
		if err != nil {
			return shellError(stderr, debug, err)
		}
		_, _ = fmt.Fprintf(stdout, "exported to %s\n", rest[0])
		return 0
	case sub == "import" && (len(rest) == 1 || len(rest) == 2 && rest[1] == "--overwrite"):
		path := "/api/v1/flows/import"
		if len(rest) == 2 {
			path += "?overwrite=true"
		}
		return withFile(rest[0], func(b []byte) int { return call(http.MethodPost, path, b) })
	case sub == "redeploy-all" && len(rest) == 0:
		return call(http.MethodPost, "/api/v1/flows/redeploy-all", nil)
	case (sub == "start-destination" || sub == "stop-destination") && len(rest) == 2:
		action := strings.TrimSuffix(sub, "-destination")
		return call(http.MethodPost, flowPath(rest[0], "destinations", rest[1], action), nil)
	case sub == "connectors" && len(rest) == 0:
		return call(http.MethodGet, "/api/v1/flows/connector-names", nil)
	case sub == "ports" && len(rest) == 0:
		return call(http.MethodGet, "/api/v1/flows/ports-in-use", nil)
	}
	return usage()
}

// idArgs returns the arguments of a subcommand that are flow ids.
func idArgs(sub string, rest []string) []string {
	switch sub {
	case "list", "help", "create", "update-all", "import", "redeploy-all", "connectors", "ports":
		return nil
	case "export":
		if len(rest) > 1 {
			return rest[1:]
		}
		return nil
	}
	if len(rest) > 0 {
		return rest[:1]
	}
	return nil
}

// renamed returns the flow's current definition with a new name, ready for
// PUT: runtime fields (status, stopped destinations) and enabled are left
// out, so the server keeps them.
func renamed(ctx context.Context, client Client, path, name string) ([]byte, error) {
	current, err := client.Call(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var doc map[string]any
	dec := json.NewDecoder(bytes.NewReader(current))
	dec.UseNumber() // keep numbers in the definition exact
	if err := dec.Decode(&doc); err != nil || doc == nil {
		return nil, errors.New("server returned an unreadable flow")
	}
	delete(doc, "status")
	delete(doc, "stoppedDestinations")
	delete(doc, "enabled")
	doc["name"] = name
	return json.Marshal(doc)
}
