package gateway

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

type bodyLimitAuth struct{ calls int }

func (a *bodyLimitAuth) Authenticate(context.Context, string, string, string) (Identity, error) {
	a.calls++
	return Identity{}, errors.New("invalid credentials")
}

type bodyLimitPasswords struct{ calls int }

func (p *bodyLimitPasswords) ChangePassword(context.Context, string, string, string) error {
	p.calls++
	return nil
}

type countedAuthBody struct {
	io.Reader
	read int
}

func (b *countedAuthBody) Read(p []byte) (int, error) {
	n, err := b.Reader.Read(p)
	b.read += n
	return n, err
}

func TestAuthRequestBodyLimit(t *testing.T) {
	const limit = 1 << 20
	for _, endpoint := range []string{"login", "password"} {
		for _, knownLength := range []bool{true, false} {
			for _, size := range []int{limit, limit + 1, 2 * limit} {
				t.Run(endpoint+"/"+map[bool]string{true: "known", false: "streamed"}[knownLength]+"/"+strconv.Itoa(size), func(t *testing.T) {
					auth := &bodyLimitAuth{}
					passwords := &bodyLimitPasswords{}
					s := New(Config{Auth: auth, Passwords: passwords, RequireCSRF: true})
					prefix := `{"username":"missing","password":"`
					if endpoint == "password" {
						prefix = `{"oldPassword":"old","newPassword":"`
					}
					body := &countedAuthBody{Reader: strings.NewReader(prefix + strings.Repeat("x", size-len(prefix)-2) + `"}`)}
					r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/"+endpoint, body)
					if knownLength {
						r.ContentLength = int64(size)
					} else {
						r.ContentLength = -1
						r.TransferEncoding = []string{"chunked"}
					}
					r.Header.Set(MarkerHeader, MarkerValue)
					if endpoint == "password" {
						token, err := s.sessions.create(Identity{Username: "viewer"}, time.Now())
						if err != nil {
							t.Fatal(err)
						}
						r.Header.Set("Authorization", "Bearer "+token)
					}
					w := httptest.NewRecorder()
					s.Router().ServeHTTP(w, r)
					wantStatus, wantCalls := http.StatusRequestEntityTooLarge, 0
					if size == limit {
						wantStatus, wantCalls = http.StatusUnauthorized, 1
						if endpoint == "password" {
							wantStatus = http.StatusNoContent
						}
					}
					if w.Code != wantStatus {
						t.Errorf("status = %d, want %d", w.Code, wantStatus)
					}
					if calls := auth.calls + passwords.calls; calls != wantCalls {
						t.Errorf("provider calls = %d, want %d", calls, wantCalls)
					}
					if body.read > limit+1 {
						t.Errorf("read %d bytes, want at most %d", body.read, limit+1)
					}
				})
			}
		}
	}
}
