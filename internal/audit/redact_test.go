package audit

import (
	"context"
	"io"
	"log/slog"
	"testing"
)

func TestRedactSensitive(t *testing.T) {
	in := map[string]string{
		"password": "a", "newPassword": "b", "X-Token": "c", "AUTHORIZATION": "d",
		"client_secret": "e", "SSN": "f", "status": "sent", "query.flowId": "lab",
		"className": "g", "graphId": "h", "query.apiToken": "i", "notpassword": "j", "patient_ssn": "k", "phiFlag": "l",
	}
	out := RedactSensitive(in)
	for k, want := range map[string]string{
		"password": "[redacted]", "newPassword": "[redacted]", "X-Token": "[redacted]", "AUTHORIZATION": "[redacted]",
		"client_secret": "[redacted]", "SSN": "[redacted]", "status": "sent", "query.flowId": "lab",
		"className": "g", "graphId": "h", "query.apiToken": "[redacted]", "notpassword": "[redacted]",
		"patient_ssn": "[redacted]", "phiFlag": "[redacted]",
	} {
		if out[k] != want {
			t.Errorf("%s = %q, want %q", k, out[k], want)
		}
	}
	if in["password"] != "a" {
		t.Error("RedactSensitive modified its input")
	}
}

func TestLocalSinkRedactsAndBounds(t *testing.T) {
	s := NewLocalSink(slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := s.Record(context.Background(), Entry{Actor: "a", Detail: map[string]string{"password": "p"}}); err != nil {
		t.Fatal(err)
	}
	if got := s.Entries()[0].Detail["password"]; got != "[redacted]" {
		t.Errorf("sink stored %q, want [redacted]", got)
	}
	for i := 0; i < 2*maxEntries+5; i++ {
		_ = s.Record(context.Background(), Entry{Actor: "a"})
	}
	entries := s.Entries()
	if len(entries) != maxEntries || len(s.entries) != maxEntries ||
		entries[0].ID != int64(maxEntries+7) || entries[len(entries)-1].ID != int64(2*maxEntries+6) {
		t.Errorf("stored %d, returned %d entries (ids %d..%d); want exactly the newest %d in order",
			len(s.entries), len(entries), entries[0].ID, entries[len(entries)-1].ID, maxEntries)
	}
}
