package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/weavster-dev/weavster/internal/flowlife"
	"github.com/weavster-dev/weavster/internal/gateway"
)

// dispatch runs a single shell command and returns its exit code (§3.2/§3.3).
func dispatch(ctx context.Context, client Client, line string, stdout, stderr io.Writer, debug bool) int {
	fields, err := splitArgs(line)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "Error: %v\n", err)
		return 2
	}
	if len(fields) == 0 {
		return 0
	}
	// Spec §11: deprecated names still run, with a warning naming the
	// replacement.
	if repl, ok := deprecatedCommands[fields[0]]; ok {
		_, _ = fmt.Fprintf(stderr, "Warning: %q is deprecated; use %q\n", fields[0], repl)
		fields[0] = repl
	}
	switch fields[0] {
	case "help":
		printShellHelp(stdout)
		return 0
	case "quit", "exit":
		return 0
	case "version":
		_, _ = fmt.Fprintln(stdout, client.Version(ctx))
		return 0
	case "status":
		return deployedStatus(ctx, client, stdout, stderr, debug)
	case "flow":
		return flowCommand(ctx, client, fields[1:], stdout, stderr, debug)
	case "deploy":
		return deployAll(ctx, client, fields[1:], stdout, stderr, debug)
	case "importmap", "exportmap", "importscripts", "exportscripts": // spec §3.2
		return itemsFileCommand(ctx, client, fields[0], fields[1:], stdout, stderr, debug)
	case "exportmessages": // spec §3.2: exportmessages "path" <flow id|name|*>
		return exportMessages(ctx, client, fields[1:], stdout, stderr, debug)
	case "importmessages": // spec §3.2: importmessages "path" <flow id|name>
		return importMessages(ctx, client, fields[1:], stdout, stderr, debug)
	case "resetstats": // spec §3.2: resetstats [lifetime]
		path := "/api/v1/flows/stats/reset"
		switch {
		case len(fields) == 2 && fields[1] == "lifetime":
			path += "?lifetime=true"
		case len(fields) != 1:
			_, _ = fmt.Fprintln(stderr, "Error: usage: resetstats [lifetime]")
			return 2
		}
		if _, err := client.Call(ctx, http.MethodPost, path, nil); err != nil {
			return shellError(stderr, debug, err)
		}
		_, _ = fmt.Fprintln(stdout, "statistics reset for every flow")
		return 0
	case "import": // spec §3.2: import "path" [force]
		switch {
		case len(fields) == 2:
			return flowCommand(ctx, client, []string{"import", fields[1]}, stdout, stderr, debug)
		case len(fields) == 3 && fields[2] == "force":
			return flowCommand(ctx, client, []string{"import", fields[1], "--overwrite"}, stdout, stderr, debug)
		}
		_, _ = fmt.Fprintln(stderr, `Error: usage: import "path" [force]`)
		return 2
	case "export": // spec §3.2: export id|"name"|* "path"
		if len(fields) != 3 {
			_, _ = fmt.Fprintln(stderr, `Error: usage: export id|"name"|* "path"`)
			return 2
		}
		args := []string{"export", fields[2]}
		if fields[1] != "*" {
			args = append(args, fields[1])
		}
		return flowCommand(ctx, client, args, stdout, stderr, debug)
	case "user":
		return userCommand(ctx, client, fields[1:], stdout, stderr, debug)
	case "snippet": // spec §3.2: snippet [library] list|import|export|remove
		return snippetCommand(ctx, client, fields[1:], stdout, stderr, debug)
	case "config": // config validate "path" (D-04)
		return configCommand(ctx, client, fields[1:], stdout, stderr, debug)
	case "exportcfg": // spec §3.2: exportcfg "path" [overwriteconfigmap]
		return exportConfig(ctx, client, fields[1:], stdout, stderr, debug)
	case "importcfg": // spec §3.2: importcfg "path" [nodeploy] [overwriteconfigmap]
		return importConfig(ctx, client, fields[1:], stdout, stderr, debug)
	case "importalert": // spec §3.2: importalert "path" [force]
		return importAlerts(ctx, client, fields[1:], stdout, stderr, debug)
	case "exportalert": // spec §3.2: exportalert id|"name"|* "path"
		return exportAlerts(ctx, client, fields[1:], stdout, stderr, debug)
	case "clearallmessages": // spec §3.2: removes every message, restarting running flows
		return clearAllMessages(ctx, client, fields[1:], stdout, stderr, debug)
	case "dump": // spec §3.2: dump stats|events "path"
		return dumpCommand(ctx, client, fields[1:], stdout, stderr, debug)
	case "deadletter": // #107 §8: list, show, requeue, remove dead-lettered messages
		return deadLetterCommand(ctx, client, fields[1:], stdout, stderr, debug)
	default:
		_, _ = fmt.Fprintf(stderr, "Error: unknown command %q\n", fields[0])
		return 2
	}
}

// deprecatedCommands maps legacy command names to their replacements
// (spec §3 renamings: channel → flow, code template → snippet).
var deprecatedCommands = map[string]string{"channel": "flow", "codetemplate": "snippet"}

// shellError prints err; in debug mode it adds each wrapped cause with its
// type. It returns exit code 2.
func shellError(stderr io.Writer, debug bool, err error) int {
	_, _ = fmt.Fprintf(stderr, "Error: %v\n", err)
	var unknown x509.UnknownAuthorityError
	var invalid x509.CertificateInvalidError
	var hostname x509.HostnameError
	switch {
	case errors.As(err, &unknown):
		_, _ = fmt.Fprintln(stderr, "  The server's certificate is not signed by a CA this machine trusts: pass that CA with -ca FILE (or ca: in the connection file).")
	case errors.As(err, &hostname):
		_, _ = fmt.Fprintln(stderr, "  The server's certificate was issued for another name: connect with a name it lists, or reissue it.")
	case errors.As(err, &invalid) && invalid.Reason == x509.Expired:
		_, _ = fmt.Fprintln(stderr, "  The server's certificate has expired or is not valid yet: renew it (or check this machine's clock).")
	case errors.As(err, &invalid):
		_, _ = fmt.Fprintln(stderr, "  The server's certificate was refused (the error says why, for example a missing server-auth key usage): reissue it.")
	}
	if debug {
		for cause := errors.Unwrap(err); cause != nil; cause = errors.Unwrap(cause) {
			_, _ = fmt.Fprintf(stderr, "  caused by %T: %v\n", cause, cause)
		}
	}
	return 2
}

func printShellHelp(w io.Writer) {
	_, _ = fmt.Fprintln(w, `commands: help, status, version, deploy [timeout], resetstats [lifetime], exportmessages "path" <flow|*>, importmessages "path" <flow>, import "path" [force], export id|"name"|* "path", flow <subcommand> (flow help), user list|add|remove|changepw, snippet [library] list|import "path"|export "path"|remove <name>, config validate|diff|plan "path", config apply "path" [--dry-run] [reason], exportcfg "path" [overwriteconfigmap], importcfg "path" [nodeploy] [overwriteconfigmap] [force], importalert "path" [force], exportalert id|"name"|* "path", clearallmessages, dump stats|events "path", deadletter list [flow]|show <id>|requeue <id>|requeue all [flow]|remove <id>, importmap|exportmap|importscripts|exportscripts "path", quit`)
}

// splitArgs splits a command line into words. Double quotes group words
// ("ADT Inbound"); inside them \" and \\ escape a quote and a backslash.
func splitArgs(line string) ([]string, error) {
	var args []string
	var cur strings.Builder
	inWord, quoted := false, false
	runes := []rune(line)
	for i := 0; i < len(runes); i++ {
		c := runes[i]
		switch {
		case quoted && c == '\\' && i+1 < len(runes) && (runes[i+1] == '"' || runes[i+1] == '\\'):
			i++
			cur.WriteRune(runes[i])
		case c == '"':
			quoted = !quoted
			inWord = true
		case !quoted && unicode.IsSpace(c):
			if inWord {
				args = append(args, cur.String())
				cur.Reset()
				inWord = false
			}
		default:
			cur.WriteRune(c)
			inWord = true
		}
	}
	if quoted {
		return nil, errors.New("unterminated quote")
	}
	if inWord {
		args = append(args, cur.String())
	}
	return args, nil
}

// deployAll deploys every enabled, undeployed flow (spec §3.2 deploy
// [timeout]); disabled flows are skipped, as at server start. The optional
// timeout in seconds stops starting new deploys once it has passed; a
// deploy already sent is never cut off.
func deployAll(ctx context.Context, client Client, args []string, stdout, stderr io.Writer, debug bool) int {
	if len(args) > 1 {
		_, _ = fmt.Fprintln(stderr, "Error: usage: deploy [timeout]")
		return 2
	}
	var deadline time.Time
	if len(args) == 1 {
		secs, err := strconv.Atoi(args[0])
		if err != nil || secs <= 0 {
			_, _ = fmt.Fprintln(stderr, "Error: timeout must be a positive number of seconds")
			return 2
		}
		deadline = time.Now().Add(time.Duration(secs) * time.Second)
	}
	flows, err := listFlows(ctx, client)
	if err != nil {
		return shellError(stderr, debug, err)
	}
	code, n := 0, 0
	for _, f := range flows {
		if f.Status != flowlife.Undeployed || !f.Enabled {
			continue
		}
		path := "/api/v1/flows/" + url.PathEscape(f.ID)
		// An earlier deploy may have deployed this flow as a dependency.
		var cur gateway.Flow
		if out, err := client.Call(ctx, http.MethodGet, path, nil); err == nil && json.Unmarshal(out, &cur) == nil && cur.Status != flowlife.Undeployed {
			continue
		}
		if !deadline.IsZero() && time.Now().After(deadline) {
			_, _ = fmt.Fprintf(stderr, "Error: timeout: deploy stopped before %s\n", f.ID)
			code = 2
			break
		}
		if _, err := client.Call(ctx, http.MethodPost, path+"/deploy", nil); err != nil {
			code = shellError(stderr, debug, fmt.Errorf("flow %s: %w", f.ID, err))
			continue
		}
		_, _ = fmt.Fprintf(stdout, "deployed %s\n", f.ID)
		n++
	}
	_, _ = fmt.Fprintf(stdout, "deployed %d flows\n", n)
	return code
}

// maxExportMessages is the most messages one exportmessages writes (the
// API's limit for one archive).
const maxExportMessages = 10000

// exportMessages writes an archive of a flow's messages (every flow's for
// "*") to a file.
func exportMessages(ctx context.Context, client Client, args []string, stdout, stderr io.Writer, debug bool) int {
	if len(args) != 2 {
		_, _ = fmt.Fprintln(stderr, `Error: usage: exportmessages "path" <flow id|name|*>`)
		return 2
	}
	var archive []byte
	var err error
	if args[1] == "*" {
		archive, err = client.Call(ctx, http.MethodGet, "/api/v1/messages/export", nil)
	} else {
		archive, err = exportFlowMessages(ctx, client, args[1])
	}
	if err == nil {
		err = os.WriteFile(args[0], archive, 0o600)
	}
	if err != nil {
		return shellError(stderr, debug, err)
	}
	n := archiveCount(archive)
	_, _ = fmt.Fprintf(stdout, "exported %d messages to %s\n", n, args[0])
	if n == maxExportMessages {
		_, _ = fmt.Fprintf(stderr, "Warning: the archive holds the %d newest messages; export older ones with the API (offset)\n", maxExportMessages)
	}
	return 0
}

// exportFlowMessages exports one flow's messages. arg is used as the flow
// id; only an empty archive leads to a lookup by name (which needs
// flows:view), so an id never needs that permission.
func exportFlowMessages(ctx context.Context, client Client, arg string) ([]byte, error) {
	get := func(id string) ([]byte, error) {
		return client.Call(ctx, http.MethodGet, "/api/v1/messages/export?flowId="+url.QueryEscape(id), nil)
	}
	archive, err := get(arg)
	if err != nil || archiveCount(archive) > 0 {
		return archive, err
	}
	flows, err := listFlows(ctx, client)
	var se *serverError
	if errors.As(err, &se) && se.Code == http.StatusForbidden {
		return archive, nil // no flows:view to check names: arg was an id with no messages
	}
	if err != nil {
		return nil, err
	}
	id, err := flowByName(flows, arg)
	if err != nil || id == arg {
		return archive, err
	}
	return get(id)
}

// importMessages restores an archive file into a flow. arg is used as the
// flow id; only when the server knows no such flow is it looked up by name.
func importMessages(ctx context.Context, client Client, args []string, stdout, stderr io.Writer, debug bool) int {
	if len(args) != 2 {
		_, _ = fmt.Fprintln(stderr, `Error: usage: importmessages "path" <flow id|name>`)
		return 2
	}
	archive, err := os.ReadFile(args[0])
	if err != nil {
		return shellError(stderr, debug, err)
	}
	post := func(id string) ([]byte, error) {
		return client.Call(ctx, http.MethodPost, "/api/v1/messages/import?flowId="+url.QueryEscape(id), archive)
	}
	out, err := post(args[1])
	var se *serverError
	if errors.As(err, &se) && se.Code == http.StatusNotFound {
		if flows, lerr := listFlows(ctx, client); lerr == nil {
			if id, nerr := flowByName(flows, args[1]); nerr == nil && id != args[1] {
				out, err = post(id)
			} else if nerr != nil {
				err = nerr
			}
		}
	}
	if err != nil {
		return shellError(stderr, debug, err)
	}
	_, _ = fmt.Fprintln(stdout, strings.TrimSpace(string(out)))
	return 0
}

// archiveCount counts the messages in an export archive (0 if unreadable).
func archiveCount(archive []byte) int {
	zr, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return 0
	}
	var doc struct {
		Items []json.RawMessage `json:"items"`
	}
	if json.NewDecoder(zr).Decode(&doc) != nil {
		return 0
	}
	return len(doc.Items)
}

// userCommand runs a spec §3.2 user administration command.
func userCommand(ctx context.Context, client Client, args []string, stdout, stderr io.Writer, debug bool) int {
	usage := func() int {
		_, _ = fmt.Fprintln(stderr, "Error: usage: user list | user add <name> <password> [permission...] | user remove <name> | user changepw <name> <password>")
		return 2
	}
	if len(args) == 0 {
		return usage()
	}
	userPath := func(name string, more ...string) string {
		p := "/api/v1/users/" + url.PathEscape(name)
		for _, m := range more {
			p += "/" + m
		}
		return p
	}
	var err error
	switch {
	case args[0] == "list" && len(args) == 1:
		var out []byte
		out, err = client.Call(ctx, http.MethodGet, "/api/v1/users", nil)
		var users []gateway.UserInfo
		if err == nil {
			err = json.Unmarshal(out, &users)
		}
		if err == nil {
			for _, u := range users {
				line := u.Username + "\t" + strings.Join(u.Permissions, ",")
				if u.MustChangePassword {
					line += "\tmust change password"
				}
				if u.Locked {
					line += "\tlocked"
				}
				_, _ = fmt.Fprintln(stdout, line)
			}
		}
	case args[0] == "add" && len(args) >= 3:
		body, _ := json.Marshal(gateway.NewUser{Username: args[1], Password: args[2], Permissions: append([]string{}, args[3:]...)})
		if _, err = client.Call(ctx, http.MethodPost, "/api/v1/users", body); err == nil {
			_, _ = fmt.Fprintf(stdout, "added %s (must change the password at first login)\n", args[1])
		}
	case args[0] == "remove" && len(args) == 2:
		if _, err = client.Call(ctx, http.MethodDelete, userPath(args[1]), nil); err == nil {
			_, _ = fmt.Fprintf(stdout, "removed %s\n", args[1])
		}
	case args[0] == "changepw" && len(args) == 3:
		body, _ := json.Marshal(map[string]string{"password": args[2]})
		if _, err = client.Call(ctx, http.MethodPost, userPath(args[1], "password"), body); err == nil {
			_, _ = fmt.Fprintf(stdout, "password set for %s (must change it at next login)\n", args[1])
		}
	default:
		return usage()
	}
	if err != nil {
		return shellError(stderr, debug, err)
	}
	return 0
}

// itemsFileCommand imports or exports the config map or the global scripts
// as a JSON file of name: value (spec §3.2 importmap, exportmap,
// importscripts, exportscripts). An import replaces the whole set.
func itemsFileCommand(ctx context.Context, client Client, cmd string, args []string, stdout, stderr io.Writer, debug bool) int {
	if len(args) != 1 {
		_, _ = fmt.Fprintf(stderr, "Error: usage: %s \"path\"\n", cmd)
		return 2
	}
	kind, noun := "configmap", "config map entries"
	if strings.HasSuffix(cmd, "scripts") {
		kind, noun = "scripts", "scripts"
	}
	count := func(doc []byte) int {
		var m map[string]json.RawMessage
		_ = json.Unmarshal(doc, &m)
		return len(m)
	}
	var err error
	if strings.HasPrefix(cmd, "export") {
		var out []byte
		if out, err = client.Call(ctx, http.MethodGet, "/api/v1/"+kind, nil); err == nil {
			err = os.WriteFile(args[0], out, 0o600)
		}
		if err == nil {
			_, _ = fmt.Fprintf(stdout, "exported %d %s to %s\n", count(out), noun, args[0])
		}
	} else {
		var doc []byte
		if doc, err = os.ReadFile(args[0]); err == nil {
			_, err = client.Call(ctx, http.MethodPut, "/api/v1/"+kind, doc)
		}
		if err == nil {
			_, _ = fmt.Fprintf(stdout, "imported %d %s from %s (they replace the previous set)\n", count(doc), noun, args[0])
		}
	}
	if err != nil {
		return shellError(stderr, debug, err)
	}
	return 0
}

// snippetCommand manages code snippets, or with a leading "library" the
// snippet libraries. import creates or replaces the file's entries and keeps
// the others.
func snippetCommand(ctx context.Context, client Client, args []string, stdout, stderr io.Writer, debug bool) int {
	path, noun := "/api/v1/snippets", "snippets"
	if len(args) > 0 && args[0] == "library" {
		path, noun, args = "/api/v1/snippet-libraries", "snippet libraries", args[1:]
	}
	want := 2
	if len(args) > 0 && args[0] == "list" {
		want = 1
	}
	if len(args) != want {
		_, _ = fmt.Fprintln(stderr, "Error: usage: snippet [library] list | import \"path\" | export \"path\" | remove <name>")
		return 2
	}
	var err error
	switch args[0] {
	case "list":
		var rows [][]string
		if noun == "snippets" {
			var list []gateway.Snippet
			err = callJSON(ctx, client, path+"?summary=true", &list)
			for _, sn := range list {
				rows = append(rows, []string{sn.Name, sn.Library, sn.Description})
			}
		} else {
			var list []gateway.SnippetLibrary
			err = callJSON(ctx, client, path, &list)
			for _, l := range list {
				rows = append(rows, []string{l.Name, l.Description})
			}
		}
		for _, cols := range rows {
			_, _ = fmt.Fprintln(stdout, strings.TrimRight(strings.Join(cols, "\t"), "\t"))
		}
	case "import":
		var doc []byte
		if doc, err = os.ReadFile(args[1]); err == nil {
			doc, err = client.Call(ctx, http.MethodPut, path, doc)
		}
		if err == nil {
			_, _ = fmt.Fprintf(stdout, "imported %d %s from %s\n", jsonArrayLen(doc), noun, args[1])
		}
	case "export":
		var out []byte
		if out, err = client.Call(ctx, http.MethodGet, path, nil); err == nil {
			err = os.WriteFile(args[1], out, 0o600)
		}
		if err == nil {
			_, _ = fmt.Fprintf(stdout, "exported %d %s to %s\n", jsonArrayLen(out), noun, args[1])
		}
	case "remove":
		if _, err = client.Call(ctx, http.MethodDelete, path+"/"+url.PathEscape(args[1]), nil); err == nil {
			_, _ = fmt.Fprintf(stdout, "removed %s\n", args[1])
		}
	default:
		_, _ = fmt.Fprintf(stderr, "Error: unknown snippet subcommand %q\n", args[0])
		return 2
	}
	if err != nil {
		return shellError(stderr, debug, err)
	}
	return 0
}

// pickAlert returns the alert with id sel, or else every alert named sel.
func pickAlert(all []gateway.Alert, sel string) []gateway.Alert {
	var named []gateway.Alert
	for _, a := range all {
		if a.ID == sel {
			return []gateway.Alert{a}
		}
		if a.Name == sel {
			named = append(named, a)
		}
	}
	return named
}

// callJSON GETs path and decodes the reply into v.
func callJSON(ctx context.Context, client Client, path string, v any) error {
	out, err := client.Call(ctx, http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	return json.Unmarshal(out, v)
}

// jsonArrayLen counts the elements of a JSON array (0 if it is not one).
func jsonArrayLen(doc []byte) int {
	var list []json.RawMessage
	_ = json.Unmarshal(doc, &list)
	return len(list)
}

// clearAllMessages removes every message; running flows are stopped for it
// and started again.
func clearAllMessages(ctx context.Context, client Client, args []string, stdout, stderr io.Writer, debug bool) int {
	if len(args) != 0 {
		_, _ = fmt.Fprintln(stderr, "Error: usage: clearallmessages")
		return 2
	}
	out, err := client.Call(ctx, http.MethodDelete, "/api/v1/messages?all=true&restart=true", nil)
	var res gateway.MessagesDeleted
	if err == nil {
		err = json.Unmarshal(out, &res)
	}
	if err != nil {
		return shellError(stderr, debug, err)
	}
	line := fmt.Sprintf("removed %d messages", res.Deleted)
	if len(res.Restarted) > 0 {
		line += "; restarted " + strings.Join(res.Restarted, ", ")
	}
	if res.Busy > 0 {
		line += fmt.Sprintf("; %d being processed were kept", res.Busy)
	}
	_, _ = fmt.Fprintln(stdout, line)
	return 0
}

// dumpCommand writes flow statistics or the event log to a JSON file.
func dumpCommand(ctx context.Context, client Client, args []string, stdout, stderr io.Writer, debug bool) int {
	paths := map[string]string{"stats": "/api/v1/flows/stats", "events": "/api/v1/events?limit=" + strconv.Itoa(gateway.MaxEventLimit)}
	if len(args) != 2 || paths[args[0]] == "" {
		_, _ = fmt.Fprintln(stderr, "Error: usage: dump stats|events \"path\"")
		return 2
	}
	out, err := client.Call(ctx, http.MethodGet, paths[args[0]], nil)
	if err == nil {
		err = os.WriteFile(args[1], out, 0o600)
	}
	if err != nil {
		return shellError(stderr, debug, err)
	}
	_, _ = fmt.Fprintf(stdout, "wrote %s to %s\n", args[0], args[1])
	return 0
}

// importAlerts saves the alerts in a JSON file (an array, as exportalert
// writes); with force, alerts with the same id are replaced.
func importAlerts(ctx context.Context, client Client, args []string, stdout, stderr io.Writer, debug bool) int {
	if len(args) == 0 || len(args) > 2 || (len(args) == 2 && args[1] != "force") {
		_, _ = fmt.Fprintln(stderr, "Error: usage: importalert \"path\" [force]")
		return 2
	}
	path := "/api/v1/alerts/import"
	if len(args) == 2 {
		path += "?force=true"
	}
	doc, err := os.ReadFile(args[0])
	if err == nil {
		doc, err = client.Call(ctx, http.MethodPost, path, doc)
	}
	if err != nil {
		return shellError(stderr, debug, err)
	}
	_, _ = fmt.Fprintf(stdout, "imported %d alerts from %s\n", jsonArrayLen(doc), args[0])
	return 0
}

// exportAlerts writes one alert (by id or name) or every alert (*) to a
// JSON file, as an array importalert reads.
func exportAlerts(ctx context.Context, client Client, args []string, stdout, stderr io.Writer, debug bool) int {
	if len(args) != 2 {
		_, _ = fmt.Fprintln(stderr, "Error: usage: exportalert id|\"name\"|* \"path\"")
		return 2
	}
	var all, picked []gateway.Alert
	err := callJSON(ctx, client, "/api/v1/alerts", &all)
	if args[0] == "*" {
		picked = all
	} else {
		picked = pickAlert(all, args[0])
	}
	if err == nil && len(picked) == 0 && args[0] != "*" {
		err = fmt.Errorf("no alert has the id or name %q", args[0])
	}
	if err == nil && len(picked) > 1 && args[0] != "*" {
		err = fmt.Errorf("%d alerts are named %q; export one by its id", len(picked), args[0])
	}
	var out []byte
	if err == nil {
		out, err = json.MarshalIndent(append([]gateway.Alert{}, picked...), "", "  ")
	}
	if err == nil {
		err = os.WriteFile(args[1], out, 0o600)
	}
	if err != nil {
		return shellError(stderr, debug, err)
	}
	_, _ = fmt.Fprintf(stdout, "exported %d alerts to %s\n", len(picked), args[1])
	return 0
}

// exportConfig writes the full configuration to a file; with
// overwriteconfigmap it includes the config map, so importcfg with
// overwriteconfigmap replaces the target server's.
func exportConfig(ctx context.Context, client Client, args []string, stdout, stderr io.Writer, debug bool) int {
	if len(args) == 0 || len(args) > 2 || (len(args) == 2 && args[1] != "overwriteconfigmap") {
		_, _ = fmt.Fprintln(stderr, "Error: usage: exportcfg \"path\" [overwriteconfigmap]")
		return 2
	}
	path := "/api/v1/config/export"
	if len(args) == 2 {
		path += "?includeConfigMap=true"
	}
	out, err := client.Call(ctx, http.MethodGet, path, nil)
	var b gateway.ConfigBundle
	if err == nil {
		err = json.Unmarshal(out, &b)
	}
	if err == nil {
		err = os.WriteFile(args[0], out, 0o600)
	}
	if err != nil {
		return shellError(stderr, debug, err)
	}
	_, _ = fmt.Fprintf(stdout, "exported %d flows, %d alerts, %d snippets, %d snippet libraries, %d scripts, %d settings", len(b.Flows), len(b.Alerts), len(b.Snippets), len(b.SnippetLibraries), len(b.Scripts), len(b.Settings))
	if b.ConfigMap != nil {
		_, _ = fmt.Fprintf(stdout, ", %d config map entries", len(*b.ConfigMap))
	}
	_, _ = fmt.Fprintf(stdout, " to %s\n", args[0])
	return 0
}

// importConfig restores a file written by exportcfg. nodeploy leaves the
// imported flows undeployed; overwriteconfigmap replaces the config map with
// the file's; force replaces flows, alerts, and snippets that exist.
func importConfig(ctx context.Context, client Client, args []string, stdout, stderr io.Writer, debug bool) int {
	params := map[string]string{"nodeploy": "nodeploy", "overwriteconfigmap": "overwriteConfigMap", "force": "force"}
	query := url.Values{}
	for _, a := range args[min(1, len(args)):] {
		if params[a] == "" {
			args = nil
			break
		}
		query.Set(params[a], "true")
	}
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, "Error: usage: importcfg \"path\" [nodeploy] [overwriteconfigmap] [force]")
		return 2
	}
	doc, err := os.ReadFile(args[0])
	if err == nil {
		path := "/api/v1/config/import"
		if len(query) > 0 {
			path += "?" + query.Encode()
		}
		doc, err = client.Call(ctx, http.MethodPost, path, doc)
	}
	var res gateway.ConfigImportResult
	if err == nil {
		err = json.Unmarshal(doc, &res)
	}
	if err != nil {
		return shellError(stderr, debug, err)
	}
	_, _ = fmt.Fprintf(stdout, "imported %d flows (%d new, %d replaced), %d alerts, %d snippets, %d snippet libraries, %d scripts, %d settings from %s\n",
		len(res.Flows.Created)+len(res.Flows.Updated), len(res.Flows.Created), len(res.Flows.Updated), res.Alerts, res.Snippets, res.SnippetLibraries, res.Scripts, res.Settings, args[0])
	if res.ConfigMap {
		_, _ = fmt.Fprintln(stdout, "replaced the config map")
	}
	if len(res.Deployed) > 0 {
		_, _ = fmt.Fprintf(stdout, "deployed %s\n", strings.Join(res.Deployed, ", "))
	}
	return 0
}

// configCommand runs config-as-code commands on a YAML or JSON document,
// none of which change the server: validate checks it locally (no server),
// diff shows what applying it would change, and plan prints that as JSON.
func configCommand(ctx context.Context, client Client, args []string, stdout, stderr io.Writer, debug bool) int {
	paths := map[string]string{"diff": "/api/v1/config/plan", "plan": "/api/v1/config/plan"}
	if len(args) >= 2 && args[0] == "apply" {
		return configApply(ctx, client, args[1], args[2:], stdout, stderr, debug)
	}
	if len(args) == 2 && args[0] == "validate" {
		doc, err := readDocument(args[1])
		var out string
		if err == nil {
			out, err = checkDocument(args[1], doc)
		}
		if err != nil {
			return shellError(stderr, debug, err)
		}
		_, _ = fmt.Fprint(stdout, out)
		return 0
	}
	if len(args) != 2 || paths[args[0]] == "" {
		_, _ = fmt.Fprintln(stderr, "Error: usage: config validate|diff|plan \"path\" | config apply \"path\" [--dry-run] [reason...]")
		return 2
	}
	doc, err := os.ReadFile(args[1])
	if err == nil {
		doc, err = client.Call(ctx, http.MethodPost, paths[args[0]], doc)
	}
	var out string
	if err == nil {
		out, err = configOutput(args[0], args[1], doc)
	}
	if err != nil {
		return shellError(stderr, debug, err)
	}
	_, _ = fmt.Fprint(stdout, out)
	return 0
}

// maxDocumentBytes is the largest config-as-code document, as on the server.
const maxDocumentBytes = 50 << 20

// readDocument reads a config-as-code document of at most maxDocumentBytes.
func readDocument(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	doc, err := io.ReadAll(io.LimitReader(f, maxDocumentBytes+1))
	if err == nil && len(doc) > maxDocumentBytes {
		err = fmt.Errorf("%s: larger than 50 MiB", path)
	}
	return doc, err
}

// checkDocument checks a config-as-code document on this machine, with the
// rules of this client's version; it needs no server and no database
// (#107 D-55).
func checkDocument(path string, doc []byte) (string, error) {
	n, err := configValidator{}.ValidateConfig(doc)
	if err != nil {
		return "", fmt.Errorf("%s: %w", path, err)
	}
	return fmt.Sprintf("%s is valid: %d flows, %d alerts, %d snippets, %d snippet libraries, %d scripts, %d config map entries, %d settings\n",
		path, n.Flows, n.Alerts, n.Snippets, n.SnippetLibraries, n.Scripts, n.ConfigMap, n.Settings), nil
}

// configOutput renders a config command's reply.
func configOutput(cmd, path string, reply []byte) (string, error) {
	switch cmd {
	case "diff":
		var plan gateway.ConfigPlan
		if err := json.Unmarshal(reply, &plan); err != nil {
			return "", err
		}
		return plan.Text, nil
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, reply, "", "  "); err != nil {
		return "", err
	}
	return pretty.String(), nil
}

// configApply plans the document, prints the plan, and applies it with the
// plan's fingerprint, so what is applied is exactly what was shown. With
// --dry-run it stops after checking the plan is current; any other words
// are the reason recorded in the audit log.
func configApply(ctx context.Context, client Client, path string, rest []string, stdout, stderr io.Writer, debug bool) int {
	query := url.Values{}
	var reason []string
	for _, a := range rest {
		switch {
		case a == "--dry-run":
			query.Set("dryRun", "true")
		case strings.HasPrefix(a, "-"): // a mistyped --dry-run must not apply
			_, _ = fmt.Fprintf(stderr, "Error: unknown option %q; use --dry-run (reason words cannot start with -)\n", a)
			return 2
		default:
			reason = append(reason, a)
		}
	}
	if len(reason) > 0 {
		query.Set("reason", strings.Join(reason, " "))
	}
	doc, err := os.ReadFile(path)
	var plan gateway.ConfigPlan
	if err == nil {
		var out []byte
		if out, err = client.Call(ctx, http.MethodPost, "/api/v1/config/plan", doc); err == nil {
			err = json.Unmarshal(out, &plan)
		}
	}
	if err != nil {
		return shellError(stderr, debug, err)
	}
	_, _ = fmt.Fprint(stdout, plan.Text)
	if len(plan.Changes) == 0 && query.Get("dryRun") == "" {
		return 0
	}
	query.Set("fingerprint", plan.Fingerprint)
	if _, err := client.Call(ctx, http.MethodPost, "/api/v1/config/apply?"+query.Encode(), doc); err != nil {
		return shellError(stderr, debug, err)
	}
	if query.Get("dryRun") != "" {
		_, _ = fmt.Fprintln(stdout, "dry run: the plan is current; nothing was changed")
		return 0
	}
	_, _ = fmt.Fprintf(stdout, "applied %d changes\n", len(plan.Changes))
	return 0
}
