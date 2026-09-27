package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/weavster-dev/weavster/internal/gateway"
)

const deadLetterUsage = "Error: usage: deadletter list [flow] | deadletter show <id> | deadletter requeue <id>|all [flow] | deadletter remove <id>"

// deadLetterCommand lists, shows, requeues, and removes dead-lettered
// messages (#107 §8).
func deadLetterCommand(ctx context.Context, client Client, args []string, stdout, stderr io.Writer, debug bool) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, deadLetterUsage)
		return 2
	}
	var err error
	switch {
	case args[0] == "list" && len(args) <= 2:
		err = listDeadLetters(ctx, client, args[1:], stdout)
	case args[0] == "show" && len(args) == 2:
		var reply []byte
		if reply, err = client.Call(ctx, http.MethodGet, "/api/v1/messages/"+url.PathEscape(args[1]), nil); err == nil {
			err = printIndented(stdout, reply)
		}
	case args[0] == "requeue" && len(args) >= 2 && args[1] == "all" && len(args) <= 3:
		err = requeueAll(ctx, client, args[2:], stdout)
	case args[0] == "requeue" && len(args) == 2:
		err = requeueOne(ctx, client, args[1], stdout)
	case args[0] == "remove" && len(args) == 2:
		err = removeDeadLetter(ctx, client, args[1], stdout)
	default:
		_, _ = fmt.Fprintln(stderr, deadLetterUsage)
		return 2
	}
	if err != nil {
		return shellError(stderr, debug, err)
	}
	return 0
}

func listDeadLetters(ctx context.Context, client Client, flow []string, stdout io.Writer) error {
	q := url.Values{"status": {"dead-lettered"}, "limit": {"1000"}, "sort": {"-receivedAt"}}
	if len(flow) == 1 {
		q.Set("flowId", flow[0])
	}
	reply, err := client.Call(ctx, http.MethodGet, "/api/v1/messages?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	var msgs []gateway.Message
	if err := json.Unmarshal(reply, &msgs); err != nil {
		return err
	}
	for _, m := range msgs {
		_, _ = fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", m.ID, m.FlowID, m.ReceivedAt.Format("2006-01-02T15:04:05Z"), attemptsText(m.Attempts, m.Metadata["error"]))
	}
	_, _ = fmt.Fprintf(stdout, "%d dead-lettered messages\n", len(msgs))
	return nil
}

// attemptsText summarizes per-destination attempts, or the processing error.
func attemptsText(attempts map[string]gateway.MessageAttempt, processingError string) string {
	dests := make([]string, 0, len(attempts))
	for d := range attempts {
		dests = append(dests, d)
	}
	sort.Strings(dests)
	parts := make([]string, 0, len(dests)+1)
	for _, d := range dests {
		a := attempts[d]
		if a.LastError == "" {
			parts = append(parts, fmt.Sprintf("%s: delivered", d))
		} else {
			parts = append(parts, fmt.Sprintf("%s: %d attempts, %s", d, a.Attempts, a.LastError))
		}
	}
	if processingError != "" {
		parts = append(parts, processingError)
	}
	return strings.Join(parts, "; ")
}

func requeueOne(ctx context.Context, client Client, id string, stdout io.Writer) error {
	reply, err := client.Call(ctx, http.MethodPost, "/api/v1/messages/"+url.PathEscape(id)+"/requeue", nil)
	if err != nil {
		return err
	}
	var res gateway.RequeueResult
	if err := json.Unmarshal(reply, &res); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "requeued %s (before: %s)\n", id, attemptsText(res.Previous, ""))
	return nil
}

func requeueAll(ctx context.Context, client Client, flow []string, stdout io.Writer) error {
	path := "/api/v1/messages/requeue"
	if len(flow) == 1 {
		path += "?flowId=" + url.QueryEscape(flow[0])
	}
	reply, err := client.Call(ctx, http.MethodPost, path, nil)
	if err != nil {
		return err
	}
	var res gateway.RequeueAllResult
	if err := json.Unmarshal(reply, &res); err != nil {
		return err
	}
	for _, s := range res.Skipped {
		_, _ = fmt.Fprintf(stdout, "skipped %s: %s\n", s.ID, s.Reason)
	}
	_, _ = fmt.Fprintf(stdout, "requeued %d messages, skipped %d\n", len(res.Requeued), len(res.Skipped))
	return nil
}

// removeDeadLetter deletes a message only if it is dead-lettered, so a
// mistyped id cannot remove a message that is still being delivered.
func removeDeadLetter(ctx context.Context, client Client, id string, stdout io.Writer) error {
	path := "/api/v1/messages/" + url.PathEscape(id)
	reply, err := client.Call(ctx, http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	var m gateway.Message
	if err := json.Unmarshal(reply, &m); err != nil {
		return err
	}
	if m.Status != "dead-lettered" {
		return fmt.Errorf("message %s is not dead-lettered (status %s); use the API to delete other messages", id, m.Status)
	}
	if _, err := client.Call(ctx, http.MethodDelete, path, nil); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "removed %s\n", id)
	return nil
}

// printIndented writes a JSON reply indented.
func printIndented(w io.Writer, reply []byte) error {
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, reply, "", "  "); err != nil {
		return err
	}
	_, _ = fmt.Fprintln(w, pretty.String())
	return nil
}
