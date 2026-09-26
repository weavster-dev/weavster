package adapters

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPSinkWriteResponse(t *testing.T) {
	tests := []struct {
		name    string
		code    int
		reply   string
		wantLen int
		wantErr bool
	}{
		{"reply", http.StatusOK, `{"ok":true}`, len(`{"ok":true}`), false},
		{"at the limit", http.StatusOK, strings.Repeat("x", MaxResponseBytes), MaxResponseBytes, false},
		{"too large", http.StatusOK, strings.Repeat("x", MaxResponseBytes+1), 0, false},
		{"error status", http.StatusServiceUnavailable, "busy", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.code)
				_, _ = w.Write([]byte(tt.reply))
			}))
			defer srv.Close()
			reply, err := NewHTTPSink(srv.URL).WriteResponse(context.Background(), Message{ID: "m", Body: []byte("x")})
			n := 0
			if reply != nil {
				n = len(reply.Body)
			}
			if (err != nil) != tt.wantErr || n != tt.wantLen {
				t.Errorf("WriteResponse = %d bytes, %v; want %d bytes, error %v", n, err, tt.wantLen, tt.wantErr)
			}
		})
	}
}

func TestHTTPSinkReplyContentTypeAndTruncation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		if r.URL.Path == "/cut" {
			w.Header().Set("Content-Length", "100")
			_, _ = w.Write([]byte("partial"))
			return // the connection closes before the declared length
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	reply, err := NewHTTPSink(srv.URL).WriteResponse(context.Background(), Message{ID: "m"})
	if err != nil || reply == nil || reply.ContentType != "text/plain" || string(reply.Body) != "ok" {
		t.Errorf("reply = %+v, %v", reply, err)
	}
	reply, err = NewHTTPSink(srv.URL+"/cut").WriteResponse(context.Background(), Message{ID: "m"})
	if err != nil || reply != nil {
		t.Errorf("cut-off reply = %+v, %v; want delivered with no reply", reply, err)
	}
	if err := NewHTTPSink(srv.URL).Write(context.Background(), Message{ID: "m"}); err != nil {
		t.Errorf("Write = %v", err)
	}
}
