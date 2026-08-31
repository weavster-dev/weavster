package notify

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type failingRoundTripper struct{ err error }

func (t failingRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, t.err
}

// TestWebhookNotifierErrorResponse covers WebhookNotifier.Notify's handling
// of non-2xx responses and the underlying httpStatusError.Error(), which
// previously had 0% coverage. Without this, a webhook endpoint returning an
// error status could silently succeed if the >=300 check regressed.
func TestWebhookNotifierErrorResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	n := NewWebhookNotifier(srv.URL)
	err := n.Notify(context.Background(), Notification{Subject: "s", Body: "b"})
	if err == nil {
		t.Fatal("Notify() error = nil, want non-nil for 503 response")
	}
	want := http.StatusText(http.StatusServiceUnavailable)
	if err.Error() != want {
		t.Errorf("Notify() error = %q, want %q", err.Error(), want)
	}
}

func TestWebhookNotifierPropagatesRequestAndTransportErrors(t *testing.T) {
	t.Run("invalid URL", func(t *testing.T) {
		err := NewWebhookNotifier("://invalid").Notify(context.Background(), Notification{})
		if err == nil {
			t.Fatal("Notify() error = nil, want invalid URL error")
		}
	})

	t.Run("transport failure", func(t *testing.T) {
		want := errors.New("network unavailable")
		notifier := NewWebhookNotifier("https://example.invalid")
		notifier.client = &http.Client{Transport: failingRoundTripper{err: want}}

		err := notifier.Notify(context.Background(), Notification{})
		if !errors.Is(err, want) {
			t.Errorf("Notify() error = %v, want %v", err, want)
		}
	})
}
