package adapters

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// FileSource reads messages from files in a directory matching a glob pattern.
type FileSource struct {
	dir     string
	pattern string
	files   []string
	idx     int
}

// NewFileSource lists files matching pattern (default "*") in dir, sorted.
func NewFileSource(dir, pattern string) (*FileSource, error) {
	if pattern == "" {
		pattern = "*"
	}
	matches, err := filepath.Glob(filepath.Join(dir, pattern))
	if err != nil {
		return nil, err
	}
	var files []string
	for _, m := range matches {
		if info, err := os.Stat(m); err == nil && !info.IsDir() {
			files = append(files, m)
		}
	}
	sort.Strings(files)
	return &FileSource{dir: dir, pattern: pattern, files: files}, nil
}

func (s *FileSource) Name() string { return "file" }

func (s *FileSource) Read(_ context.Context) (Message, error) {
	if s.idx >= len(s.files) {
		return Message{}, io.EOF
	}
	path := s.files[s.idx]
	s.idx++
	b, err := os.ReadFile(path)
	if err != nil {
		return Message{}, err
	}
	return Message{ID: filepath.Base(path), Body: b}, nil
}

func (s *FileSource) Close() error { return nil }

// FileSink writes messages as files in a directory.
type FileSink struct {
	dir string
}

// NewFileSink returns a file sink writing into dir (created if needed).
func NewFileSink(dir string) *FileSink { return &FileSink{dir: dir} }

func (s *FileSink) Name() string { return "file" }

func (s *FileSink) Write(_ context.Context, m Message) error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	name := m.ID
	if name == "" {
		name = "message"
	}
	// Sanitize the id so it cannot escape the directory.
	name = strings.ReplaceAll(name, string(os.PathSeparator), "_")
	return os.WriteFile(filepath.Join(s.dir, name), m.Body, 0o644)
}

func (s *FileSink) Close() error { return nil }
