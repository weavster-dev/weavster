package main

import (
	"bytes"
	"compress/gzip"
	"context"
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
		if len(fields) >= 2 && fields[1] == "list" {
			users, err := client.UserList(ctx)
			if err != nil {
				return shellError(stderr, debug, err)
			}
			for _, u := range users {
				_, _ = fmt.Fprintln(stdout, u)
			}
			return 0
		}
		_, _ = fmt.Fprintln(stderr, "Error: unknown user subcommand")
		return 2
	default:
		_, _ = fmt.Fprintf(stderr, "Error: unknown command %q\n", fields[0])
		return 2
	}
}

// shellError prints err; in debug mode it adds each wrapped cause with its
// type. It returns exit code 2.
func shellError(stderr io.Writer, debug bool, err error) int {
	_, _ = fmt.Fprintf(stderr, "Error: %v\n", err)
	if debug {
		for cause := errors.Unwrap(err); cause != nil; cause = errors.Unwrap(cause) {
			_, _ = fmt.Fprintf(stderr, "  caused by %T: %v\n", cause, cause)
		}
	}
	return 2
}

func printShellHelp(w io.Writer) {
	_, _ = fmt.Fprintln(w, `commands: help, status, version, deploy [timeout], resetstats [lifetime], exportmessages "path" <flow|*>, importmessages "path" <flow>, import "path" [force], export id|"name"|* "path", flow <subcommand> (flow help), user list, quit`)
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
