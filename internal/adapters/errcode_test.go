package adapters

import (
	"context"
	"crypto/x509"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// TestErrorCode: failures carry their protocol's code; network and TLS
// failures are classified by kind; others have none.
func TestErrorCode(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closed := ln.Addr().String()
	_ = ln.Close()
	_, refused := net.Dial("tcp", closed)
	if refused == nil {
		t.Skip("the freed port was taken again")
	}
	expired, cancel := context.WithTimeout(context.Background(), 0)
	defer cancel()
	<-expired.Done()
	pg, _ := NewSQLSink(nil, SQLSinkOptions{Dialect: DialectPostgres, Table: "t", Columns: []SQLColumn{{"a", "a"}}})
	for _, tt := range []struct {
		err  error
		want string
	}{
		{nil, ""},
		{errors.New("transform failed"), ""},
		{WithCode("flow:not-running", errors.New("x")), "flow:not-running"},
		{fmt.Errorf("wrapped: %w", &httpStatusError{code: 503}), "http:503"},
		{refused, "net:refused"},
		{fmt.Errorf("x: %w", syscall.ECONNRESET), "net:reset"},
		{expired.Err(), "net:timeout"},
		{&net.DNSError{Err: "no such host", Name: "nope.invalid"}, "net:dns"},
		{fmt.Errorf("x: %w", x509.UnknownAuthorityError{}), "tls:certificate"},
		{pg.dbError(context.Background(), &pgconn.PgError{Code: "42P01"}), "sqlstate:42P01"},
		{pg.dbError(expired, errors.New("x")), "net:timeout"},
		{pg.dbError(context.Background(), sql.ErrConnDone), "net:reset"},
		{pg.dbError(context.Background(), fmt.Errorf("read: %w", syscall.ECONNRESET)), "net:reset"},
		{pg.dbError(context.Background(), errors.New("encode")), ""},
	} {
		if got := ErrorCode(tt.err); got != tt.want {
			t.Errorf("%v: %q, want %q", tt.err, got, tt.want)
		}
	}
	if got := Classify(refused); ErrorCode(got) != "net:refused" || !errors.Is(got, syscall.ECONNREFUSED) {
		t.Errorf("Classify(refused) = %v", got)
	}
	coded := WithCode("http:503", errors.New("x"))
	if Classify(coded) != coded || Classify(nil) != nil || Classify(errors.New("x")).Error() != "x" {
		t.Error("Classify changed an error it should keep")
	}

	// An HTTP sink's status and an MLLP sink's ACK code.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }))
	defer srv.Close()
	err = NewHTTPSinkWith(srv.URL, HTTPSinkOptions{Timeout: 5 * time.Second}).Write(context.Background(), Message{Body: []byte("{}")})
	if ErrorCode(err) != "http:503" {
		t.Errorf("HTTP 503: %v (%q)", err, ErrorCode(err))
	}
	msg := "MSH|^~\\&|A|B|C|D|1||ADT^A01|C1|P|2.5\r"
	for reply, want := range map[string]string{
		"MSH|^~\\&|C|D|A|B|2||ACK^A01|X|P|2.5\rMSA|AE|C1\r": "mllp:AE",
		"MSH|^~\\&|C|D|A|B|2||ACK^A01|X|P|2.5\rMSA|AR|C1\r": "mllp:AR",
		"MSH|^~\\&|C|D|A|B|2||ACK^A01|X|P|2.5\rMSA|AA|C9\r": "mllp:wrong-message",
		"hello":                            "mllp:not-an-ack",
		strings.Repeat("x", maxACKBytes+1): "mllp:ack-too-large",
	} {
		err := NewMLLPSinkWith(mllpPeer(t, reply), 5*time.Second).Write(context.Background(), Message{Body: []byte(msg)})
		if got := ErrorCode(err); got != want {
			t.Errorf("reply %q: %v (%q), want %q", reply, err, got, want)
		}
	}
	err = NewMLLPSinkWith(mllpPeer(t, ""), 100*time.Millisecond).Write(context.Background(), Message{Body: []byte(msg)})
	if got := ErrorCode(err); got != "net:timeout" {
		t.Errorf("no ACK in time: %v (%q)", err, got)
	}
}
