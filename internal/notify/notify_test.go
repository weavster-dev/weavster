package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/smtp"
	"slices"
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

func TestSMTPNotifierMultipleRecipientsAndSendError(t *testing.T) {
	wantErr := errors.New("smtp unavailable")
	wantRecipients := []string{"primary@example.com", "backup@example.com"}
	var captured []byte
	n := NewSMTPNotifier("smtp.example.com:2525", "alerts@example.com")
	n.send = func(addr string, auth smtp.Auth, from string, to []string, msg []byte) error {
		if addr != "smtp.example.com:2525" {
			t.Errorf("send address = %q", addr)
		}
		if auth != nil {
			t.Errorf("send auth = %v, want nil", auth)
		}
		if from != "alerts@example.com" {
			t.Errorf("envelope sender = %q", from)
		}
		if !slices.Equal(to, wantRecipients) {
			t.Errorf("envelope recipients = %v, want %v", to, wantRecipients)
		}
		captured = append([]byte(nil), msg...)
		return wantErr
	}

	err := n.Notify(context.Background(), Notification{
		Recipients: wantRecipients,
		Subject:    "Delivery failure",
		Body:       "flow stopped",
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Notify error = %v, want %v", err, wantErr)
	}
	if !bytes.Contains(captured, []byte("To: primary@example.com, backup@example.com\r\n")) {
		t.Errorf("message missing multi-recipient To header: %q", captured)
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

func TestWebhookNotifierErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	n := NewWebhookNotifier(srv.URL)
	err := n.Notify(context.Background(), Notification{Recipients: []string{"x"}, Subject: "s", Body: "b"})
	if err == nil {
		t.Error("expected error for non-2xx status")
	}
}
