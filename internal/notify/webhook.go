package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
)

// WebhookNotifier delivers notifications as JSON to a webhook URL (arch §3.1).
type WebhookNotifier struct {
	url    string
	client *http.Client
}

// NewWebhookNotifier returns a webhook notifier.
func NewWebhookNotifier(url string) *WebhookNotifier {
	return &WebhookNotifier{url: url, client: http.DefaultClient}
}

func (w *WebhookNotifier) Notify(ctx context.Context, n Notification) error {
	body, err := json.Marshal(n)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := w.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return &httpStatusError{code: resp.StatusCode}
	}
	return nil
}

type httpStatusError struct{ code int }

func (e *httpStatusError) Error() string { return http.StatusText(e.code) }

var _ Notifier = (*WebhookNotifier)(nil)
