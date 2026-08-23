package adapters

import (
	"bytes"
	"context"
	"net/http"
)

// WebServiceSink POSTs messages with a SOAPAction header (SOAP-style web
// service adapter; spec §8).
type WebServiceSink struct {
	url        string
	soapAction string
	client     *http.Client
}

// NewWebServiceSink returns a web-service sink.
func NewWebServiceSink(url, soapAction string) *WebServiceSink {
	return &WebServiceSink{url: url, soapAction: soapAction, client: http.DefaultClient}
}

func (s *WebServiceSink) Name() string { return "web-service" }

func (s *WebServiceSink) Write(ctx context.Context, m Message) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url, bytes.NewReader(m.Body))
	if err != nil {
		return err
	}
	if s.soapAction != "" {
		req.Header.Set("SOAPAction", s.soapAction)
	}
	req.Header.Set("Content-Type", "text/xml; charset=utf-8")
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return &httpStatusError{code: resp.StatusCode}
	}
	return nil
}

func (s *WebServiceSink) Close() error { return nil }
