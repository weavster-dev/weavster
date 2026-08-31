package adapters

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestFileSinkWriteUsesSafeFilename(t *testing.T) {
	dir := t.TempDir()
	sink := NewFileSink(dir)

	if err := sink.Write(context.Background(), Message{Body: []byte("default")}); err != nil {
		t.Fatal(err)
	}
	if err := sink.Write(context.Background(), Message{ID: "nested" + string(os.PathSeparator) + "message", Body: []byte("sanitized")}); err != nil {
		t.Fatal(err)
	}

	for name, want := range map[string]string{
		"message":        "default",
		"nested_message": "sanitized",
	} {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %q: %v", name, err)
		}
		if string(got) != want {
			t.Errorf("%s contents = %q, want %q", name, got, want)
		}
	}

	if _, err := os.Stat(filepath.Join(dir, "nested", "message")); !os.IsNotExist(err) {
		t.Errorf("unsanitized path exists or could not be checked: %v", err)
	}
}
