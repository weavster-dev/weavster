package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/weavster-dev/weavster/internal/gateway"
)

func TestMoveFile(t *testing.T) {
	src, dest := t.TempDir(), filepath.Join(t.TempDir(), "done")
	write := func(name, body string) string {
		p := filepath.Join(src, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	// Into a directory that does not exist yet.
	if err := moveFile(write("a.json", "1"), dest, "a.json", "m1"); err != nil {
		t.Fatal(err)
	}
	// A name already there gets the suffix, or a timestamp without one.
	if err := moveFile(write("a.json", "2"), dest, "a.json", "m2"); err != nil {
		t.Fatal(err)
	}
	if err := moveFile(write("a.json", "3"), dest, "a.json", ""); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dest)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if len(names) != 3 || names[0] != "a.json" || !strings.HasPrefix(names[1], "a.json.") || names[2] != "a.json.m2" {
		t.Errorf("moved = %v", names)
	}
	if b, _ := os.ReadFile(filepath.Join(dest, "a.json.m2")); string(b) != "2" {
		t.Errorf("a.json.m2 = %q", b)
	}
	if left, _ := os.ReadDir(src); len(left) != 0 {
		t.Errorf("source not emptied: %v", left)
	}
	// Errors: the source is missing, or the destination cannot be created.
	if err := moveFile(filepath.Join(src, "none"), dest, "none", ""); err == nil {
		t.Error("moving a missing file succeeded")
	}
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := moveFile(write("b.json", "x"), filepath.Join(blocker, "sub"), "b.json", ""); err == nil {
		t.Error("moving under a file succeeded")
	}
}

func TestCopyThenRemove(t *testing.T) {
	tests := []struct {
		name                  string
		source                string
		destination           string
		wantErr               bool
		wantSource            string
		wantDestination       string
		wantSourceExists      bool
		wantDestinationExists bool
	}{
		{name: "success", source: "message", wantDestination: "message", wantDestinationExists: true},
		{name: "missing source", wantErr: true},
		{name: "destination exists", source: "new", destination: "existing", wantErr: true, wantSource: "new", wantDestination: "existing", wantSourceExists: true, wantDestinationExists: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			src := filepath.Join(dir, "source")
			dest := filepath.Join(dir, "destination")
			if tt.source != "" {
				if err := os.WriteFile(src, []byte(tt.source), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if tt.destination != "" {
				if err := os.WriteFile(dest, []byte(tt.destination), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			err := copyThenRemove(src, dest)
			if (err != nil) != tt.wantErr {
				t.Fatalf("copyThenRemove() error = %v, wantErr %v", err, tt.wantErr)
			}
			gotSource, sourceErr := os.ReadFile(src)
			if tt.wantSourceExists {
				if sourceErr != nil || string(gotSource) != tt.wantSource {
					t.Errorf("source = %q, %v", gotSource, sourceErr)
				}
			} else if !os.IsNotExist(sourceErr) {
				t.Errorf("source exists: %v", sourceErr)
			}
			gotDestination, destinationErr := os.ReadFile(dest)
			if tt.wantDestinationExists {
				if destinationErr != nil || string(gotDestination) != tt.wantDestination {
					t.Errorf("destination = %q, %v", gotDestination, destinationErr)
				}
			} else if !os.IsNotExist(destinationErr) {
				t.Errorf("destination exists: %v", destinationErr)
			}
		})
	}
}

// fakeIngest records ingested files and answers with err (and id).
type fakeIngest struct {
	files []string
	id    string
	err   error
}

func (f *fakeIngest) ingest(_ context.Context, _ string, _ []byte, md map[string]string) (gateway.IngestResult, error) {
	f.files = append(f.files, md["source.file"])
	return gateway.IngestResult{ID: f.id}, f.err
}

type fakeFlowList struct {
	flows []gateway.Flow
	calls int
}

func (f *fakeFlowList) List(context.Context) ([]gateway.Flow, error) {
	f.calls++
	return f.flows, nil
}

func (f *fakeFlowList) Get(_ context.Context, id string) (gateway.Flow, error) {
	for _, fl := range f.flows {
		if fl.ID == id {
			return fl, nil
		}
	}
	return gateway.Flow{}, gateway.ErrFlowNotFound
}

type fakeEvents struct{ types []string }

func (f *fakeEvents) record(typ, _ string, _ map[string]string) { f.types = append(f.types, typ) }

// newTestSources polls one started flow reading dir; each run advances the
// clock past every interval.
func newTestSources(t *testing.T, src *gateway.FlowSource, ing *fakeIngest) (*fileSources, *fakeFlowList, *bytes.Buffer, func()) {
	t.Helper()
	flows := &fakeFlowList{flows: []gateway.Flow{{ID: "f", Status: "started", Source: src}}}
	var logs bytes.Buffer
	s := newFileSources(flows, ing, &fakeEvents{}, slog.New(slog.NewTextHandler(&logs, nil)))
	clock := time.Now()
	s.now = func() time.Time { return clock }
	run := func() {
		clock = clock.Add(2 * time.Second)
		s.pass(context.Background())
	}
	return s, flows, &logs, run
}

func settled(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Minute)
	if err := os.Chtimes(p, old, old); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestFileSourceEdgeCases(t *testing.T) {
	t.Run("processed but not removable: never sent again", func(t *testing.T) {
		dir := t.TempDir()
		blocker := filepath.Join(t.TempDir(), "file")
		settled(t, filepath.Dir(blocker), "file", "")
		settled(t, dir, "a.json", "{}")
		ing := &fakeIngest{id: "m1"}
		_, _, logs, run := newTestSources(t, &gateway.FlowSource{Type: "file", Dir: dir, MoveTo: filepath.Join(blocker, "sub")}, ing)
		run()
		run()
		if len(ing.files) != 1 || !strings.Contains(logs.String(), "could not be removed") {
			t.Errorf("ingested %v; logs %s", ing.files, logs)
		}
	})
	t.Run("stored then failed: the file is done", func(t *testing.T) {
		dir := t.TempDir()
		p := settled(t, dir, "a.json", "{}")
		ing := &fakeIngest{id: "m1", err: errors.New("store busy")}
		_, _, _, run := newTestSources(t, &gateway.FlowSource{Type: "file", Dir: dir}, ing)
		run()
		if _, err := os.Stat(p); !os.IsNotExist(err) || len(ing.files) != 1 {
			t.Errorf("file still there (%v) or ingested %v", err, ing.files)
		}
	})
	t.Run("nothing stored: kept and tried again", func(t *testing.T) {
		dir := t.TempDir()
		p := settled(t, dir, "a.json", "{}")
		ing := &fakeIngest{err: errors.New("store down")}
		_, _, _, run := newTestSources(t, &gateway.FlowSource{Type: "file", Dir: dir}, ing)
		run()
		run()
		if _, err := os.Stat(p); err != nil || len(ing.files) != 2 {
			t.Errorf("file gone (%v) or ingested %v", err, ing.files)
		}
	})
	t.Run("unreadable file skipped until it changes", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root reads unreadable files")
		}
		dir := t.TempDir()
		p := settled(t, dir, "a.json", "{}")
		if err := os.Chmod(p, 0); err != nil {
			t.Fatal(err)
		}
		ing := &fakeIngest{}
		_, _, logs, run := newTestSources(t, &gateway.FlowSource{Type: "file", Dir: dir}, ing)
		run()
		run()
		if len(ing.files) != 0 || strings.Count(logs.String(), "cannot read a file") != 1 {
			t.Errorf("ingested %v; logs %s", ing.files, logs)
		}
	})
	t.Run("dotfiles only when the pattern asks", func(t *testing.T) {
		dir := t.TempDir()
		settled(t, dir, ".tmp.json", "{}")
		settled(t, dir, "a.json", "{}")
		ing := &fakeIngest{id: "m"}
		_, _, _, run := newTestSources(t, &gateway.FlowSource{Type: "file", Dir: dir}, ing)
		run()
		if strings.Join(ing.files, ",") != "a.json" {
			t.Errorf("default pattern read %v", ing.files)
		}
		ing2 := &fakeIngest{id: "m"}
		_, _, _, run2 := newTestSources(t, &gateway.FlowSource{Type: "file", Dir: dir, Pattern: ".*"}, ing2)
		run2()
		if strings.Join(ing2.files, ",") != ".tmp.json" {
			t.Errorf(".* read %v", ing2.files)
		}
	})
	t.Run("a poll reads at most maxFilesPerPoll files", func(t *testing.T) {
		dir := t.TempDir()
		for i := 0; i < maxFilesPerPoll+20; i++ {
			settled(t, dir, fmt.Sprintf("m%04d.json", i), "{}")
		}
		ing := &fakeIngest{id: "m"}
		_, _, _, run := newTestSources(t, &gateway.FlowSource{Type: "file", Dir: dir}, ing)
		run()
		if len(ing.files) != maxFilesPerPoll {
			t.Fatalf("first poll read %d", len(ing.files))
		}
		run()
		if len(ing.files) != maxFilesPerPoll+20 {
			t.Errorf("after two polls: %d", len(ing.files))
		}
	})
	t.Run("flows are listed at most once per refresh", func(t *testing.T) {
		ing := &fakeIngest{}
		s, flows, _, _ := newTestSources(t, &gateway.FlowSource{Type: "file", Dir: t.TempDir()}, ing)
		for i := 0; i < 5; i++ {
			s.pass(context.Background()) // same instant
		}
		if flows.calls != 1 {
			t.Errorf("listed %d times", flows.calls)
		}
	})
	t.Run("a missing directory is logged once; stopped flows are not read", func(t *testing.T) {
		ing := &fakeIngest{}
		s, flows, logs, run := newTestSources(t, &gateway.FlowSource{Type: "file", Dir: filepath.Join(t.TempDir(), "none")}, ing)
		run()
		run()
		if strings.Count(logs.String(), "cannot read the directory") != 1 {
			t.Errorf("logs %s", logs)
		}
		dir := t.TempDir()
		settled(t, dir, "a.json", "{}")
		flows.flows = []gateway.Flow{{ID: "f", Status: "stopped", Source: &gateway.FlowSource{Type: "file", Dir: dir}}}
		run()
		if len(ing.files) != 0 || len(s.lastErr) != 0 {
			t.Errorf("stopped flow read %v; lastErr %v", ing.files, s.lastErr)
		}
	})
}

func TestMoveFileRefusesItsOwnDirectory(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a")
	if err := os.WriteFile(p, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := moveFile(p, dir+"/", "a", ""); err == nil || !strings.Contains(err.Error(), "already in") {
		t.Errorf("err = %v", err)
	}
}

func TestReadAtMost(t *testing.T) {
	dir := t.TempDir()
	for _, tt := range []struct {
		body string
		err  error
	}{
		{"1234", nil},
		{"12345", errTooLarge},
	} {
		p := filepath.Join(dir, "f")
		if err := os.WriteFile(p, []byte(tt.body), 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := readAtMost(p, 4)
		if !errors.Is(err, tt.err) || (err == nil && string(got) != tt.body) {
			t.Errorf("%q: %q %v", tt.body, got, err)
		}
	}
	if _, err := readAtMost(filepath.Join(dir, "none"), 4); err == nil {
		t.Error("missing file read")
	}
}

// TestFileSourceUsesCurrentMoveTo: cleanup follows the flow's current
// moveTo, not the one cached when the poll began.
func TestFileSourceUsesCurrentMoveTo(t *testing.T) {
	dir, oldTo, newTo := t.TempDir(), t.TempDir(), t.TempDir()
	settled(t, dir, "a.json", "{}")
	ing := &fakeIngest{id: "m1"}
	s, flows, _, run := newTestSources(t, &gateway.FlowSource{Type: "file", Dir: dir, MoveTo: oldTo}, ing)
	run() // lists the flow with oldTo, and processes a.json
	if _, err := os.Stat(filepath.Join(oldTo, "a.json")); err != nil {
		t.Fatalf("not moved to the cached moveTo: %v", err)
	}
	settled(t, dir, "b.json", "{}")
	flows.flows[0].Source = &gateway.FlowSource{Type: "file", Dir: dir, MoveTo: newTo} // updated; the cache is older
	s.listed = s.now()                                                                 // keep the cached list for this pass
	s.clock.last["f"] = time.Time{}
	s.pass(context.Background())
	if _, err := os.Stat(filepath.Join(newTo, "b.json")); err != nil {
		t.Errorf("not moved to the current moveTo: %v", err)
	}
}

// TestListFilesRecursive: recursion reads files up to maxSourceDepth
// levels of subdirectories and no deeper, filters by pattern, reports an
// unreadable subdirectory, follows a root that is a symbolic link, and a
// missing root is an error.
func TestListFilesRecursive(t *testing.T) {
	root := t.TempDir()
	at := func(depth int) string {
		p := root
		for i := 0; i < depth; i++ {
			p = filepath.Join(p, "d")
		}
		return p
	}
	for _, p := range []string{filepath.Join(root, "top.json"), filepath.Join(root, "top.txt"),
		filepath.Join(at(maxSourceDepth), "deepest.json"), filepath.Join(at(maxSourceDepth+1), "too-deep.json"),
		filepath.Join(root, "locked", "x.json")} {
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(filepath.Join(root, "locked"), 0); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(filepath.Join(root, "locked"), 0o750) }()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".cache"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".cache", ".x.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if files, _, _ := listFiles(root, true, ".*"); len(files) != 0 {
		t.Errorf("a dot pattern entered a hidden directory: %v", files)
	}
	deepest := strings.Repeat("d/", maxSourceDepth) + "deepest.json"
	for _, dir := range []string{root, link} {
		files, skipped, err := listFiles(dir, true, "*.json")
		if err != nil {
			t.Fatal(err)
		}
		var rels []string
		for _, f := range files {
			rels = append(rels, f.rel)
		}
		want, wantSkipped := deepest+",top.json", "locked"
		if os.Geteuid() == 0 { // root reads the locked directory anyway
			want, wantSkipped = deepest+",locked/x.json,top.json", ""
		}
		if strings.Join(rels, ",") != want || strings.Join(skipped, ",") != wantSkipped {
			t.Errorf("%s: listed %v, skipped %v; want %s, %s", dir, rels, skipped, want, wantSkipped)
		}
	}
	if _, _, err := listFiles(filepath.Join(root, "missing"), true, "*"); err == nil {
		t.Error("a missing root listed without error")
	}
}
