package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunHelpAndVersion(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"-h"}, strings.NewReader(""), &out, &errb); code != 0 {
		t.Errorf("help exit = %d", code)
	}
	if !strings.Contains(out.String(), "Usage:") {
		t.Errorf("help output = %q", out.String())
	}

	out.Reset()
	if code := run([]string{"-v"}, strings.NewReader(""), &out, &errb); code != 0 {
		t.Errorf("version exit = %d", code)
	}
	if !strings.Contains(out.String(), version) {
		t.Errorf("version output = %q", out.String())
	}
}

func TestRunUsageError(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"-nonexistent"}, strings.NewReader(""), &out, &errb); code != 2 {
		t.Errorf("unknown flag exit = %d, want 2", code)
	}
}

func TestRunScriptReadError(t *testing.T) {
	var out, errb bytes.Buffer
	missing := filepath.Join(t.TempDir(), "missing-script")

	if code := run([]string{"-s", missing}, strings.NewReader(""), &out, &errb); code != 2 {
		t.Errorf("script read error exit = %d, want 2", code)
	}
	if !strings.Contains(errb.String(), "Error:") || !strings.Contains(errb.String(), missing) {
		t.Errorf("script read error = %q", errb.String())
	}
}

func TestRunTestCommand(t *testing.T) {
	dir := t.TempDir()
	var out, errb bytes.Buffer
	code := run([]string{"test", "--format", "junit", "--output", dir}, strings.NewReader(""), &out, &errb)
	if code != 0 {
		t.Fatalf("test exit = %d, stderr = %q", code, errb.String())
	}
	// junit file written to output dir
	b, err := os.ReadFile(filepath.Join(dir, "results.xml"))
	if err != nil {
		t.Fatalf("read junit: %v", err)
	}
	if !strings.Contains(string(b), "<testsuite") {
		t.Errorf("junit output = %q", b)
	}
}

func TestRunTestJSONToStdout(t *testing.T) {
	var out, errb bytes.Buffer
	code := run([]string{"test", "--format", "json", "--filter", "json"}, strings.NewReader(""), &out, &errb)
	if code != 0 {
		t.Fatalf("exit = %d stderr = %q", code, errb.String())
	}
	if !strings.Contains(out.String(), `"name": "identity/json"`) {
		t.Errorf("json output = %q", out.String())
	}
	if !strings.Contains(out.String(), `"passed": true`) {
		t.Errorf("json output missing passed field: %q", out.String())
	}
}

func TestShellBatchMode(t *testing.T) {
	client := &fakeClient{}
	var out, errb bytes.Buffer
	code := runScript([]byte("# comment\nhelp\nversion\nflow list\nnonsense\n"), client, &out, &errb, false)
	if code != 2 {
		t.Errorf("exit = %d, want 2 (unknown command)", code)
	}
	if !strings.Contains(out.String(), "commands:") || !strings.Contains(out.String(), version) {
		t.Errorf("output = %q", out.String())
	}
	if !strings.Contains(out.String(), "Patient Admit") {
		t.Errorf("flow list output missing: %q", out.String())
	}
}

func TestDispatchExitCodes(t *testing.T) {
	client := &fakeClient{}
	var out, errb bytes.Buffer
	if code := dispatch(context.Background(), client, "help", &out, &errb, false); code != 0 {
		t.Errorf("help exit = %d", code)
	}
	if code := dispatch(context.Background(), client, "bogus", &out, &errb, false); code != 2 {
		t.Errorf("bogus exit = %d, want 2", code)
	}
	if code := dispatch(context.Background(), client, "quit", &out, &errb, false); code != 0 {
		t.Errorf("quit exit = %d", code)
	}
}

func TestDispatchStatusFlowUser(t *testing.T) {
	client := &fakeClient{}
	var out, errb bytes.Buffer

	if code := dispatch(context.Background(), client, "status", &out, &errb, false); code != 0 {
		t.Errorf("status exit = %d, stderr = %q", code, errb.String())
	}
	if !strings.Contains(out.String(), "started") {
		t.Errorf("status output = %q", out.String())
	}

	out.Reset()
	if code := dispatch(context.Background(), client, "user list", &out, &errb, false); code != 0 {
		t.Errorf("user list exit = %d, stderr = %q", code, errb.String())
	}
	if !strings.Contains(out.String(), "admin") {
		t.Errorf("user list output = %q", out.String())
	}

	out.Reset()
	if code := dispatch(context.Background(), client, "user bogus", &out, &errb, false); code != 2 {
		t.Errorf("user bogus exit = %d, want 2", code)
	}
	if !strings.Contains(errb.String(), "unknown user subcommand") {
		t.Errorf("stderr = %q", errb.String())
	}
}

func TestDispatchClientErrors(t *testing.T) {
	client := &erroringClient{}
	var out, errb bytes.Buffer

	if code := dispatch(context.Background(), client, "status", &out, &errb, false); code != 2 {
		t.Errorf("status error exit = %d, want 2", code)
	}

	errb.Reset()
	if code := dispatch(context.Background(), client, "flow list", &out, &errb, false); code != 2 {
		t.Errorf("flow list error exit = %d, want 2", code)
	}

	errb.Reset()
	if code := dispatch(context.Background(), client, "user list", &out, &errb, false); code != 2 {
		t.Errorf("user list error exit = %d, want 2", code)
	}
}

type erroringClient struct{}

func (erroringClient) Status(context.Context) (string, error) {
	return "", fmt.Errorf("status unavailable")
}
func (erroringClient) FlowList(context.Context) ([]string, error) {
	return nil, fmt.Errorf("flow list unavailable")
}
func (erroringClient) UserList(context.Context) ([]string, error) {
	return nil, fmt.Errorf("user list unavailable")
}
func (erroringClient) Version(context.Context) string { return version }

func TestPrivilegedGuard(t *testing.T) {
	if err := checkPrivileged(false, func() bool { return true }); err == nil {
		t.Error("expected refusal when privileged")
	}
	if err := checkPrivileged(true, func() bool { return true }); err != nil {
		t.Errorf("allowRoot should bypass: %v", err)
	}
	if err := checkPrivileged(false, func() bool { return false }); err != nil {
		t.Errorf("non-privileged should pass: %v", err)
	}
}

func TestBuildServerServesSystem(t *testing.T) {
	t.Setenv("WEAVSTER_ADMIN_USER", "admin")
	t.Setenv("WEAVSTER_ADMIN_PASSWORD", "Admin123!")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler, err := buildServer(logger)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/system", nil)
	req.Header.Set("X-Weavster-CSRF", "1")
	req.SetBasicAuth("admin", "Admin123!")
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("system status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "weavster") {
		t.Errorf("system body = %q", rec.Body.String())
	}
}

func TestBuildServerMissingCredentials(t *testing.T) {
	os.Unsetenv("WEAVSTER_ADMIN_USER")
	os.Unsetenv("WEAVSTER_ADMIN_PASSWORD")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	_, err := buildServer(logger)
	if err == nil {
		t.Fatal("expected error when credentials are absent")
	}
}

type fakeClient struct{}

func (fakeClient) Status(context.Context) (string, error) { return "started", nil }
func (fakeClient) FlowList(context.Context) ([]string, error) {
	return []string{"Patient Admit"}, nil
}
func (fakeClient) UserList(context.Context) ([]string, error) { return []string{"admin"}, nil }
func (fakeClient) Version(context.Context) string             { return version }

func TestShellError(t *testing.T) {
	var errb bytes.Buffer
	code := shellError(&errb, false, fmt.Errorf("oops"))
	if code != 2 {
		t.Errorf("shellError code = %d, want 2", code)
	}
	if !strings.Contains(errb.String(), "oops") {
		t.Errorf("output = %q", errb.String())
	}

	errb.Reset()
	code = shellError(&errb, true, fmt.Errorf("debug-err"))
	if code != 2 || !strings.Contains(errb.String(), "debug-err") {
		t.Errorf("debug mode: code=%d output=%q", code, errb.String())
	}
}
