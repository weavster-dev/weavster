package adapters

import (
	"context"
	"os"
	"path/filepath"
	"strings"
)

// DocumentSink renders messages into documents via a template (spec §8
// document writer). Placeholders: {{id}}, {{body}}.
type DocumentSink struct {
	dir      string
	template string
}

// NewDocumentSink returns a document writer into dir using template.
func NewDocumentSink(dir, template string) *DocumentSink {
	return &DocumentSink{dir: dir, template: template}
}

func (s *DocumentSink) Name() string { return "document" }

func (s *DocumentSink) Write(_ context.Context, m Message) error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	content := s.template
	content = strings.ReplaceAll(content, "{{id}}", m.ID)
	content = strings.ReplaceAll(content, "{{body}}", string(m.Body))

	name := m.ID
	if name == "" {
		name = "document"
	}
	name = strings.ReplaceAll(name, string(os.PathSeparator), "_")
	return os.WriteFile(filepath.Join(s.dir, name+".txt"), []byte(content), 0o644)
}

func (s *DocumentSink) Close() error { return nil }
