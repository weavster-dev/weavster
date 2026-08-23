package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/smtp"
	"testing"
)

func TestSMTPNotifier(t *testing.T) {
	var captured []byte
	n := NewSMTPNotifier("localhost:25", "from@example.com")
	n.send = func(_ string, _ smtp.Auth, _ string, _ []string, msg []byte) error {
		captured = msg
		return nil
	}
	if err := n.Notify(context.Background(), Notification{
		Recipients: []string{"ops@example.com"},
		Subject:    "Alert",
		Body:       "flow failed",
	}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(captured, []byte("Subject: Alert")) || !bytes.Contains(captured, []byte("flow failed")) {
		t.Errorf("captured = %q", captured)
	}
}

func TestWebhookNotifier(t *testing.T) {
	var got Notification
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := NewWebhookNotifier(srv.URL)
	if err := n.Notify(context.Background(), Notification{Recipients: []string{"x"}, Subject: "s", Body: "b"}); err != nil {
		t.Fatal(err)
	}
	if got.Subject != "s" || got.Body != "b" {
		t.Errorf("got = %+v", got)
	}
}
