package main

import (
	"bytes"
	"net"
	"net/http"
	"os"
	"syscall"
	"testing"
	"time"
)

// TestIsPrivileged exercises the untested isPrivileged() one-liner against
// the real process euid, guarding against a typo inverting the comparison.
func TestIsPrivileged(t *testing.T) {
	want := os.Geteuid() == 0
	if got := isPrivileged(); got != want {
		t.Errorf("isPrivileged() = %v, want %v", got, want)
	}
}

// TestRunServerBindFailure covers runServer's error path when the requested
// address is already in use: ListenAndServe fails, and runServer must
// surface the error on stderr and return exit code 1 rather than hang.
func TestRunServerBindFailure(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	addr := ln.Addr().String()

	var stderr bytes.Buffer
	code := runServer([]string{addr}, &stderr)
	if code != 1 {
		t.Fatalf("exit = %d, want 1 (address in use), stderr = %q", code, stderr.String())
	}
	if stderr.Len() == 0 {
		t.Error("expected an error message on stderr")
	}
}

// TestRunServerShutsDownOnSignal covers the full runServer lifecycle: it
// binds a real listener, serves requests, and then cleanly shuts down and
// returns 0 when it receives SIGTERM -- the path exercised in production by
// the container runtime's stop signal.
func TestRunServerShutsDownOnSignal(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close() // free the port for runServer to bind synchronously

	done := make(chan int, 1)
	var stderr bytes.Buffer
	go func() {
		done <- runServer([]string{addr}, &stderr)
	}()

	// Poll until the server is actually accepting connections.
	deadline := time.Now().Add(5 * time.Second)
	var ready bool
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://" + addr + "/api/v1/system")
		if err == nil {
			_ = resp.Body.Close()
			ready = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !ready {
		t.Fatalf("server never became ready: %s", stderr.String())
	}

	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("kill: %v", err)
	}

	select {
	case code := <-done:
		if code != 0 {
			t.Errorf("exit = %d, want 0 after graceful shutdown, stderr = %q", code, stderr.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runServer did not shut down after SIGTERM")
	}
}
