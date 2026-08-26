package notify

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

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
