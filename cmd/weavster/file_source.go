package main

import (
	"context"
	"errors"
	"fmt"
	"io"
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
)

// Ports of the file sources: the flows, message ingest, and the event log.
type (
	flowLister interface {
		List(ctx context.Context) ([]gateway.Flow, error)
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
	last    map[string]time.Time // flow id -> last poll
	skip    map[string]fileStamp // path -> version not to read again (done or refused)
	lastErr map[string]string    // flow id -> last directory error logged
}

func newFileSources(flows flowLister, ingest messageIngester, events eventRecorder, logger *slog.Logger) *fileSources {
	return &fileSources{flows: flows, ingest: ingest, events: events, logger: logger, now: time.Now,
		last: map[string]time.Time{}, skip: map[string]fileStamp{}, lastErr: map[string]string{}}
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

// pass polls every started flow with a file source whose interval has
// passed. The flow list is read at most once per flowRefresh; a flow that
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
			delete(s.last, f.ID)
			delete(s.lastErr, f.ID)
			continue
		}
		interval := defaultPollInterval
		if src.PollIntervalMs > 0 {
			interval = time.Duration(src.PollIntervalMs) * time.Millisecond
		}
		if now.Sub(s.last[f.ID]) < interval {
			continue
		}
		s.last[f.ID] = now
		s.poll(ctx, f, now)
		if ctx.Err() != nil {
			return
		}
	}
}

// poll reads up to maxFilesPerPoll files of one flow's directory, in name
// order.
func (s *fileSources) poll(ctx context.Context, f gateway.Flow, now time.Time) {
	src := f.Source
	pattern := src.Pattern
	if pattern == "" {
		pattern = "*"
	}
	entries, err := os.ReadDir(src.Dir)
	if err != nil {
		if msg := err.Error(); s.lastErr[f.ID] != msg { // once per distinct error
			s.lastErr[f.ID] = msg
			s.logger.Warn("file source: cannot read the directory", "flow", f.ID, "error", err)
		}
		return
	}
	delete(s.lastErr, f.ID)
	present := map[string]bool{}
	read := 0
	for _, e := range entries { // sorted by name; symlinks are not followed
		path := filepath.Join(src.Dir, e.Name())
		present[path] = true
		if ctx.Err() != nil || read == maxFilesPerPoll {
			continue
		}
		if !e.Type().IsRegular() || hidden(e.Name(), pattern) {
			continue // directories, symlinks, devices; dotfiles unless asked for
		}
		if ok, _ := filepath.Match(pattern, e.Name()); !ok {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue // removed meanwhile
		}
		stamp := fileStamp{size: info.Size(), mod: info.ModTime()}
		if now.Sub(stamp.mod) < fileSettle || s.skip[path] == stamp {
			continue // still being written, or unchanged since it was done or refused
		}
		delete(s.skip, path)
		read++
		if !s.readFile(ctx, f, e.Name(), path, stamp) {
			break
		}
	}
	for path := range s.skip { // forget files that are gone
		if filepath.Dir(path) == filepath.Clean(src.Dir) && !present[path] {
			delete(s.skip, path)
		}
	}
}

// hidden reports whether name is a dotfile a pattern that does not start
// with a dot leaves alone (temporary files of rsync and editors,
// .DS_Store).
func hidden(name, pattern string) bool {
	return strings.HasPrefix(name, ".") && !strings.HasPrefix(pattern, ".")
}

// readFile sends one file through the flow and then removes it (moves it
// to moveTo when set). It returns false when the flow stopped accepting
// messages or the store failed, so the rest waits for the next poll.
func (s *fileSources) readFile(ctx context.Context, f gateway.Flow, name, path string, stamp fileStamp) bool {
	if stamp.size > gateway.MaxMessageBytes {
		s.reject(f, name, path, stamp, fmt.Sprintf("larger than %d MiB", gateway.MaxMessageBytes>>20))
		return true
	}
	body, err := os.ReadFile(path)
	if err != nil {
		s.skip[path] = stamp // not again until it changes
		s.logger.Warn("file source: cannot read a file; skipped until it changes", "flow", f.ID, "file", name, "error", err)
		return true
	}
	res, err := s.ingest.ingest(ctx, f.ID, body, map[string]string{"source.file": name})
	switch {
	case errors.Is(err, gateway.ErrFlowNotRunning), errors.Is(err, gateway.ErrFlowNotFound):
		return false
	case errors.Is(err, gateway.ErrInvalidMessage):
		s.reject(f, name, path, stamp, err.Error())
		return true
	case err != nil && res.ID == "": // nothing stored: try again later
		s.logger.Warn("file source: processing failed; the file is kept", "flow", f.ID, "file", name, "error", err)
		return false
	case err != nil: // stored, then failed: the message exists, so the file is done
		s.logger.Warn("file source: the message was stored but processing failed", "flow", f.ID, "file", name, "message", res.ID, "error", err)
	}
	if f.Source.MoveTo == "" {
		err = os.Remove(path)
	} else {
		err = moveFile(path, f.Source.MoveTo, name, res.ID)
	}
	if err != nil {
		// The message is stored: never send this version of the file again.
		s.skip[path] = stamp
		s.logger.Error("file source: the file was processed but could not be removed; it is skipped until it changes", "flow", f.ID, "file", name, "message", res.ID, "error", err)
	}
	return true
}

// reject moves a file the flow refuses to moveTo/rejected, or leaves it
// and skips it until it changes, and records a source.file.rejected event.
func (s *fileSources) reject(f gateway.Flow, name, path string, stamp fileStamp, reason string) {
	moved := false
	if f.Source.MoveTo != "" {
		if err := moveFile(path, filepath.Join(f.Source.MoveTo, "rejected"), name, ""); err == nil {
			moved = true
		} else {
			s.logger.Warn("file source: cannot move a rejected file", "flow", f.ID, "file", name, "error", err)
		}
	}
	if !moved {
		s.skip[path] = stamp
	}
	s.events.record("source.file.rejected", f.ID, map[string]string{"file": name, "reason": reason})
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
