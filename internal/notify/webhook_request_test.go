package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestWebhookNotifierRequestContract(t *testing.T) {
	want := Notification{
		Recipients: []string{"ops@example.com", "audit@example.com"},
		Subject:    "Flow failed",
		Body:       "flow:a could not deliver message 42",
	}
	called := false
	notifier := NewWebhookNotifier("https://hooks.example.test/notify")
	notifier.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		called = true
		if req.Method != http.MethodPost {
			t.Errorf("method = %q, want %q", req.Method, http.MethodPost)
		}
		if got := req.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", got)
		}

		var got Notification
		if err := json.NewDecoder(req.Body).Decode(&got); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if got.Subject != want.Subject || got.Body != want.Body {
			t.Errorf("payload = %+v, want %+v", got, want)
		}
		if len(got.Recipients) != len(want.Recipients) {
			t.Fatalf("recipients = %v, want %v", got.Recipients, want.Recipients)
		}
		for i := range want.Recipients {
			if got.Recipients[i] != want.Recipients[i] {
				t.Errorf("recipient[%d] = %q, want %q", i, got.Recipients[i], want.Recipients[i])
			}
		}

		return &http.Response{
			StatusCode: http.StatusNoContent,
			Body:       io.NopCloser(strings.NewReader("")),
			Header:     make(http.Header),
		}, nil
	})}

	if err := notifier.Notify(context.Background(), want); err != nil {
		t.Fatalf("Notify() error = %v", err)
	}
	if !called {
		t.Fatal("webhook transport was not called")
	}
}
