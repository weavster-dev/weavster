package main

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/weavster-dev/weavster/internal/gateway"
	"github.com/weavster-dev/weavster/internal/state"
)

// itemRepository is the store side of the config map, scripts, and
// settings. Every state backend implements it.
type itemRepository interface {
	ListItems(ctx context.Context, kind string) (map[string]json.RawMessage, error)
	GetItem(ctx context.Context, kind, name string) (json.RawMessage, error)
	PutItem(ctx context.Context, kind, name string, value json.RawMessage) error
	DeleteItem(ctx context.Context, kind, name string) error
	ReplaceItems(ctx context.Context, kind string, items map[string]json.RawMessage) error
}

// itemsAdapter serves gateway.ItemStore from the store.
type itemsAdapter struct{ repo itemRepository }

func itemErr(err error) error {
	if errors.Is(err, state.ErrItemNotFound) {
		return gateway.ErrItemNotFound
	}
	return err
}

func (a itemsAdapter) ListItems(ctx context.Context, kind string) (map[string]json.RawMessage, error) {
	return a.repo.ListItems(ctx, kind)
}

func (a itemsAdapter) GetItem(ctx context.Context, kind, name string) (json.RawMessage, error) {
	v, err := a.repo.GetItem(ctx, kind, name)
	return v, itemErr(err)
}

func (a itemsAdapter) PutItem(ctx context.Context, kind, name string, value json.RawMessage) error {
	return a.repo.PutItem(ctx, kind, name, value)
}

func (a itemsAdapter) DeleteItem(ctx context.Context, kind, name string) error {
	return itemErr(a.repo.DeleteItem(ctx, kind, name))
}

func (a itemsAdapter) ReplaceItems(ctx context.Context, kind string, items map[string]json.RawMessage) error {
	return a.repo.ReplaceItems(ctx, kind, items)
}
