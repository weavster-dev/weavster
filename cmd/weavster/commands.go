package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
	_, _ = fmt.Fprintln(w, `commands: help, status, version, deploy [timeout], resetstats [lifetime], import "path" [force], export id|"name"|* "path", flow <subcommand> (flow help), user list, quit`)
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
