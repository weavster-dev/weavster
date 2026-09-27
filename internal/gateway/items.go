package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
)

// ItemStore keeps named JSON values per kind (the config map, global
// scripts, settings).
type ItemStore interface {
	ListItems(ctx context.Context, kind string) (map[string]json.RawMessage, error)
	GetItem(ctx context.Context, kind, name string) (json.RawMessage, error) // ErrItemNotFound
	PutItem(ctx context.Context, kind, name string, value json.RawMessage) error
	DeleteItem(ctx context.Context, kind, name string) error // ErrItemNotFound
	ReplaceItems(ctx context.Context, kind string, items map[string]json.RawMessage) error
}

// ErrItemNotFound: no item has that name.
var ErrItemNotFound = errors.New("item not found")

// itemKind describes one item resource.
type itemKind struct {
	kind     string // storage kind and URL segment
	resource string // permission resource: <resource>:edit
	label    string // for messages
	strings  bool   // values must be JSON strings
}

// Item resources (spec §5): the config map, global scripts, and settings.
var itemKinds = []itemKind{
	{kind: "configmap", resource: "configmap", label: "config map entry", strings: true},
	{kind: "scripts", resource: "scripts", label: "script", strings: true},
	{kind: "settings", resource: "settings", label: "setting"},
}

// validItemName: 1–128 of A-Z a-z 0-9 . _ -.
var validItemName = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

// maxItemsBody caps an item request body.
const maxItemsBody = 10 << 20

// checkName validates an item name; the error is safe to show.
func checkName(name string) error {
	if !validItemName.MatchString(name) {
		return fmt.Errorf("name %q must be 1-128 characters from A-Z a-z 0-9 . _ -", name)
	}
	return nil
}

// checkItem validates a name and value for kind k; the error is safe to show.
func (k itemKind) checkItem(name string, value json.RawMessage) error {
	if err := checkName(name); err != nil {
		return err
	}
	if string(value) == "null" {
		return fmt.Errorf("value of %s must not be null; delete it instead", name)
	}
	if k.strings {
		var str string
		if json.Unmarshal(value, &str) != nil {
			return fmt.Errorf("value of %s must be a string", name)
		}
	}
	return nil
}

// readItemsBody reads a JSON document of at most maxItemsBody into v,
// rejecting unknown fields.
func readItemsBody(w http.ResponseWriter, r *http.Request, v any) bool {
	return readStrictJSON(w, r, maxItemsBody, v)
}

// readStrictJSON reads one JSON document of at most limit bytes into v,
// rejecting unknown fields and trailing data; it answers 413 or 400 and
// returns false on failure.
func readStrictJSON(w http.ResponseWriter, r *http.Request, limit int64, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	dec.DisallowUnknownFields()
	err := dec.Decode(v)
	if err == nil {
		if _, tokErr := dec.Token(); tokErr != io.EOF {
			err = errors.New("trailing data after the JSON document")
		}
	}
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeStatusError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("body larger than %d MiB", limit>>20))
			return false
		}
		writeStatusError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return false
	}
	return true
}

func (s *Server) itemsAvailable(w http.ResponseWriter) bool {
	if s.cfg.Items == nil {
		writeStatusError(w, http.StatusServiceUnavailable, "configuration items unavailable")
		return false
	}
	return true
}

func writeItemError(w http.ResponseWriter, k itemKind, err error) {
	if errors.Is(err, ErrItemNotFound) {
		writeStatusError(w, http.StatusNotFound, k.label+" not found")
		return
	}
	writeBackendError(w, err)
}

func (s *Server) handleItemsList(k itemKind) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.itemsAvailable(w) {
			return
		}
		items, err := s.cfg.Items.ListItems(r.Context(), k.kind)
		if err != nil {
			writeItemError(w, k, err)
			return
		}
		writeJSON(w, http.StatusOK, items)
	}
}

func (s *Server) handleItemsReplace(k itemKind) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.itemsAvailable(w) {
			return
		}
		var items map[string]json.RawMessage
		if !readItemsBody(w, r, &items) {
			return
		}
		if items == nil {
			writeStatusError(w, http.StatusBadRequest, "body must be a JSON object of name: value")
			return
		}
		for name, v := range items {
			if err := k.checkItem(name, v); err != nil {
				writeStatusError(w, http.StatusBadRequest, err.Error())
				return
			}
		}
		if err := s.cfg.Items.ReplaceItems(r.Context(), k.kind, items); err != nil {
			writeItemError(w, k, err)
			return
		}
		writeJSON(w, http.StatusOK, items)
	}
}

func (s *Server) handleItemGet(k itemKind) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.itemsAvailable(w) {
			return
		}
		name, ok := pathName(w, r)
		if !ok {
			return
		}
		v, err := s.cfg.Items.GetItem(r.Context(), k.kind, name)
		if err != nil {
			writeItemError(w, k, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"name": name, "value": v})
	}
}

func (s *Server) handleItemPut(k itemKind) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.itemsAvailable(w) {
			return
		}
		var body struct {
			Value json.RawMessage `json:"value"`
		}
		if !readItemsBody(w, r, &body) {
			return
		}
		name := r.PathValue("name")
		if body.Value == nil {
			writeStatusError(w, http.StatusBadRequest, `body must be {"value": ...}`)
			return
		}
		if err := k.checkItem(name, body.Value); err != nil {
			writeStatusError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := s.cfg.Items.PutItem(r.Context(), k.kind, name, body.Value); err != nil {
			writeItemError(w, k, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"name": name, "value": body.Value})
	}
}

func (s *Server) handleItemDelete(k itemKind) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.itemsAvailable(w) {
			return
		}
		name, ok := pathName(w, r)
		if !ok {
			return
		}
		if err := s.cfg.Items.DeleteItem(r.Context(), k.kind, name); err != nil {
			writeItemError(w, k, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
