package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/weavster-dev/weavster/internal/flowlife"
	"github.com/weavster-dev/weavster/internal/gateway"
)

// File source timing and limits (#107 D-56).
const (
	// sourceTick is how often the poller checks which flows are due.
	sourceTick = 100 * time.Millisecond
	// flowRefresh is how often the list of flows is read again.
	flowRefresh = time.Second
	// fileSettle is how long a file must be unchanged before it is read,
	// so a file still being written is not taken half-way.
	fileSettle = time.Second
	// defaultPollInterval applies when a source sets no pollIntervalMs.
	defaultPollInterval = time.Second
	// maxFilesPerPoll bounds one flow's turn, so a busy directory cannot
	// hold up the other flows' sources; the rest waits for the next poll.
	maxFilesPerPoll = 100
	// maxSourceDepth bounds how deep a recursive source reads (#107 D-69).
	maxSourceDepth = 32
)

// Ports of the file sources: the flows, message ingest, and the event log.
type (
	flowLister interface {
		List(ctx context.Context) ([]gateway.Flow, error)
		Get(ctx context.Context, id string) (gateway.Flow, error)
	}
	messageIngester interface {
		ingest(ctx context.Context, flowID string, body []byte, metadata map[string]string) (gateway.IngestResult, error)
	}
	eventRecorder interface {
		record(typ, flowID string, data map[string]string)
	}
)

// fileStamp identifies a version of a file.
type fileStamp struct {
	size int64
	mod  time.Time
}

// fileSources polls the directory of every started flow that has a file
// source and sends each file through the flow like a message sent with the
// API (#107 D-56). It runs in one goroutine, so its maps need no lock.
type fileSources struct {
	flows   flowLister
	ingest  messageIngester
	events  eventRecorder
	logger  *slog.Logger
	now     func() time.Time
	listed  time.Time
	cached  []gateway.Flow
	clock   pollClock                       // when each flow polls
	skip    map[string]map[string]fileStamp // flow id -> path -> version not to read again (done or refused)
	lastErr map[string]string               // flow id -> last directory error logged
}

func newFileSources(flows flowLister, ingest messageIngester, events eventRecorder, logger *slog.Logger) *fileSources {
	return &fileSources{flows: flows, ingest: ingest, events: events, logger: logger, now: time.Now,
		clock: newPollClock(), skip: map[string]map[string]fileStamp{}, lastErr: map[string]string{}}
}

// loop polls until ctx is cancelled; a file in progress is finished first.
func (s *fileSources) loop(ctx context.Context) {
	ticker := time.NewTicker(sourceTick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.pass(ctx)
		}
	}
}

// pass polls every started flow with a file source that is due (its
// interval passed, or its schedule's time came; pollDue). The flow list is read at most once per flowRefresh; a flow that
// stopped meanwhile refuses the messages (ErrFlowNotRunning), so no file
// is taken after a stop.
func (s *fileSources) pass(ctx context.Context) {
	now := s.now()
	if now.Sub(s.listed) >= flowRefresh {
		flows, err := s.flows.List(ctx)
		if err != nil {
			if ctx.Err() == nil {
				s.logger.Warn("file sources: listing flows failed", "error", err)
			}
			return
		}
		s.cached, s.listed = flows, now
	}
	for _, f := range s.cached {
		src := f.Source
		if src == nil || src.Type != "file" || flowlife.Normalize(f.Status) != flowlife.Started {
			s.clock.forget(f.ID)
			delete(s.lastErr, f.ID)
			continue
		}
		due, err := s.clock.due(src, f.ID, now, defaultPollInterval)
		if err != nil {
			s.warnOnce(f.ID, "file source: not polling", err)
		}
		if !due {
			continue
		}
		if again := s.poll(ctx, f, now); again >= 0 {
			s.clock.retry(f.ID, now.Add(again))
		}
		if ctx.Err() != nil {
			return
		}
	}
}

// warnOnce logs a flow's source problem once per distinct error.
func (s *fileSources) warnOnce(flowID, msg string, err error) {
	if s.lastErr[flowID] != err.Error() {
		s.lastErr[flowID] = err.Error()
		s.logger.Warn(msg, "flow", flowID, "error", err)
	}
}

// poll reads up to maxFilesPerPoll files of one flow's directory, in name
// order. It returns when to poll again before the next regular time (-1
// for no sooner): at once when files are left over, after fileSettle when
// a file was still being written, and, for a scheduled source, after
// defaultPollInterval when the directory could not be read or a file could
// not be stored.
func (s *fileSources) poll(ctx context.Context, f gateway.Flow, now time.Time) time.Duration {
	src := f.Source
	pattern := src.Pattern
	if pattern == "" {
		pattern = "*"
	}
	files, skipped, err := listFiles(src.Dir, src.Recursive, pattern)
	if err != nil {
		s.warnOnce(f.ID, "file source: cannot read the directory", err)
		if src.Schedule != "" {
			return defaultPollInterval // a scheduled poll is not skipped for a passing error
		}
		return -1
	}
	delete(s.lastErr, f.ID)
	if msg := strings.Join(skipped, ", "); msg != s.lastErr[f.ID+"/subdirs"] { // once per distinct set
		s.lastErr[f.ID+"/subdirs"] = msg
		if msg != "" {
			s.logger.Warn("file source: cannot read subdirectories; their files are not read", "flow", f.ID, "subdirectories", msg)
		}
	}
	if s.skip[f.ID] == nil {
		s.skip[f.ID] = map[string]fileStamp{}
	}
	skip := s.skip[f.ID]
	present := map[string]bool{}
	read := 0
	again := time.Duration(-1)
	for _, e := range files { // matching regular files, in path order
		present[e.path] = true
		if ctx.Err() != nil {
			continue
		}
		info, err := e.d.Info()
		if err != nil {
			continue // removed meanwhile
		}
		stamp := fileStamp{size: info.Size(), mod: info.ModTime()}
		switch {
		case skip[e.path] == stamp:
			continue // unchanged since it was done or refused
		case now.Sub(stamp.mod) < fileSettle:
			if again < 0 {
				again = fileSettle // still being written: look again once it settles
			}
			continue
		case read == maxFilesPerPoll:
			again = 0 // more files: the next poll at once
			continue
		}
		delete(skip, e.path)
		read++
		if res := s.readFile(ctx, f, e.rel, e.path, stamp); res != readDone {
			switch {
			case res == readBusy && (again < 0 || again > defaultPollInterval):
				again = defaultPollInterval // throttled: the rest after a moment, also for a schedule
			case res == readFailed && src.Schedule != "" && (again < 0 || again > defaultPollInterval):
				again = defaultPollInterval // a scheduled poll is not skipped for a passing error
			}
			break
		}
	}
	for path := range skip { // forget this flow's files that are gone
		if !present[path] {
			delete(skip, path)
		}
	}
	return again
}

// listed is a file a source can read: its path, its path relative to the
// source's dir ("/"-separated), and its directory entry.
type listed struct {
	path, rel string
	d         fs.DirEntry
}

// listFiles lists, in path order, the regular files in dir whose names
// match pattern (hidden ones only when the pattern starts with a dot);
// recursive also lists subdirectories up to maxSourceDepth levels deep,
// skipping hidden ones. Symbolic links are never followed, except dir
// itself. Subdirectories that cannot be read are skipped and returned;
// dir itself not being readable is an error.
func listFiles(dir string, recursive bool, pattern string) (files []listed, skipped []string, err error) {
	root := filepath.Clean(dir)
	match := func(d fs.DirEntry) bool {
		ok, _ := filepath.Match(pattern, d.Name())
		return ok && d.Type().IsRegular() && !hidden(d.Name(), pattern)
	}
	if !recursive {
		entries, err := os.ReadDir(root) // sorted by name; follows dir if it is a link
		for _, e := range entries {
			if match(e) {
				files = append(files, listed{path: filepath.Join(root, e.Name()), rel: e.Name(), d: e})
			}
		}
		return files, nil, err
	}
	// A trailing separator makes WalkDir enter dir when it is a link.
	start := root
	if start != string(filepath.Separator) {
		start += string(filepath.Separator)
	}
	err = filepath.WalkDir(start, func(path string, d fs.DirEntry, err error) error {
		rel, _ := filepath.Rel(root, path) // path is under root
		if rel == "." {
			return err // the source's own dir must be readable
		}
		if err != nil {
			skipped = append(skipped, filepath.ToSlash(rel))
			return nil // an unreadable subdirectory
		}
		if d.IsDir() {
			// rel of a directory n levels down has n-1 separators.
			// Hidden directories are always skipped (a pattern starting
			// with a dot opts into hidden files only).
			if strings.HasPrefix(d.Name(), ".") || strings.Count(rel, string(filepath.Separator))+1 > maxSourceDepth {
				return filepath.SkipDir
			}
			return nil
		}
		if match(d) {
			files = append(files, listed{path: filepath.Join(root, rel), rel: filepath.ToSlash(rel), d: d})
		}
		return nil
	})
	return files, skipped, err
}

// markSkip records a version of a flow's file not to read again.
func (s *fileSources) markSkip(flowID, path string, stamp fileStamp) {
	if s.skip[flowID] == nil {
		s.skip[flowID] = map[string]fileStamp{}
	}
	s.skip[flowID][path] = stamp
}

// hidden reports whether name is a dotfile a pattern that does not start
// with a dot leaves alone (temporary files of rsync and editors,
// .DS_Store).
func hidden(name, pattern string) bool {
	return strings.HasPrefix(name, ".") && !strings.HasPrefix(pattern, ".")
}

// readResult is what reading one file means for the rest of the poll.
type readResult int

const (
	readDone    readResult = iota // go on with the next file
	readStopped                   // the flow stopped accepting messages: stop
	readFailed                    // nothing was stored (the store failed): stop, try again later
	readBusy                      // the server is at its processing limit: stop, try again soon
)

// readFile sends one file through the flow and then removes it (moves it
// to moveTo when set). When it returns anything but readDone, the rest of
// the files wait for a later poll.
func (s *fileSources) readFile(ctx context.Context, f gateway.Flow, name, path string, stamp fileStamp) readResult {
	tooLarge := fmt.Sprintf("larger than %d MiB", gateway.MaxMessageBytes>>20)
	if stamp.size > gateway.MaxMessageBytes {
		s.reject(f, name, path, stamp, tooLarge)
		return readDone
	}
	body, err := readAtMost(path, gateway.MaxMessageBytes)
	if errors.Is(err, errTooLarge) { // it grew after the listing
		s.reject(f, name, path, stamp, tooLarge)
		return readDone
	}
	if err != nil {
		s.markSkip(f.ID, path, stamp) // not again until it changes
		s.logger.Warn("file source: cannot read a file; skipped until it changes", "flow", f.ID, "file", name, "error", err)
		return readDone
	}
	res, err := s.ingest.ingest(ctx, f.ID, body, map[string]string{"source.file": name})
	switch {
	case errors.Is(err, gateway.ErrFlowNotRunning), errors.Is(err, gateway.ErrFlowNotFound):
		return readStopped
	case errors.Is(err, gateway.ErrInvalidMessage):
		s.reject(f, name, path, stamp, err.Error())
		return readDone
	case errors.Is(err, gateway.ErrBusy): // throttled, not failed: the file waits for a later poll
		return readBusy
	case err != nil && res.ID == "": // nothing stored: try again later
		s.logger.Warn("file source: processing failed; the file is kept", "flow", f.ID, "file", name, "error", err)
		return readFailed
	case err != nil: // stored, then failed: the message exists, so the file is done
		s.logger.Warn("file source: the message was stored but processing failed", "flow", f.ID, "file", name, "message", res.ID, "error", err)
	}
	// Clean up with the flow's current source: it may have been updated
	// while the file was processed.
	moveTo := f.Source.MoveTo
	if cur, gerr := s.flows.Get(ctx, f.ID); gerr == nil && cur.Source != nil {
		moveTo = cur.Source.MoveTo
	}
	if moveTo == "" {
		err = os.Remove(path)
	} else {
		err = moveFile(path, filepath.Join(moveTo, filepath.Dir(name)), filepath.Base(name), res.ID)
	}
	if err != nil {
		// The message is stored: never send this version of the file again.
		s.markSkip(f.ID, path, stamp)
		s.logger.Error("file source: the file was processed but could not be removed; it is skipped until it changes", "flow", f.ID, "file", name, "message", res.ID, "error", err)
	}
	return readDone
}

// reject moves a file the flow refuses to moveTo/rejected, or leaves it
// and skips it until it changes, and records a source.file.rejected event.
func (s *fileSources) reject(f gateway.Flow, name, path string, stamp fileStamp, reason string) {
	moved := false
	if f.Source.MoveTo != "" {
		if err := moveFile(path, filepath.Join(f.Source.MoveTo, "rejected", filepath.Dir(name)), filepath.Base(name), ""); err == nil {
			moved = true
		} else {
			s.logger.Warn("file source: cannot move a rejected file", "flow", f.ID, "file", name, "error", err)
		}
	}
	if !moved {
		s.markSkip(f.ID, path, stamp)
	}
	s.events.record("source.file.rejected", f.ID, map[string]string{"file": name, "reason": reason})
}

// errTooLarge: a file is larger than the limit readAtMost was given.
var errTooLarge = errors.New("file too large")

// readAtMost reads a file of at most limit bytes (errTooLarge otherwise),
// never holding more than limit+1 bytes.
func readAtMost(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	body, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err == nil && int64(len(body)) > limit {
		return nil, errTooLarge
	}
	return body, err
}

// moveFile moves path into dir (created if missing) under name, or, when a
// file of that name is already there, under name.<suffix>. Only a move to
// another filesystem copies; any other rename failure is returned.
func moveFile(path, dir, name, suffix string) error {
	if filepath.Clean(filepath.Dir(path)) == filepath.Clean(dir) {
		return fmt.Errorf("%s is already in %s", name, dir)
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	dest := filepath.Join(dir, name)
	if _, err := os.Lstat(dest); err == nil {
		if suffix == "" {
			suffix = time.Now().UTC().Format("20060102T150405.000000000")
		}
		dest += "." + suffix
	}
	err := os.Rename(path, dest)
	if err == nil || !errors.Is(err, syscall.EXDEV) {
		return err
	}
	return copyThenRemove(path, dest)
}

// copyThenRemove moves a file across filesystems; if the original cannot
// be removed, the copy is removed, so the file is never in both places.
func copyThenRemove(path, dest string) error {
	in, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Remove(path)
	}
	if err != nil {
		_ = os.Remove(dest)
	}
	return err
}
