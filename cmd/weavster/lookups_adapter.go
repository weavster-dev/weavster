package main

import (
	"context"
	"errors"

	"github.com/weavster-dev/weavster/internal/gateway"
	"github.com/weavster-dev/weavster/internal/state"
)

// lookupRepository is the store side of dynamic lookups. Every state
// backend implements it.
type lookupRepository interface {
	LookupGroups(ctx context.Context) (map[string]int, error)
	LookupEntries(ctx context.Context, group, prefix string, limit int) (map[string]string, error)
	LookupGet(ctx context.Context, group string, keys []string) (map[string]string, error)
	LookupPut(ctx context.Context, group string, entries map[string]string, replace bool) error
	LookupDelete(ctx context.Context, group, key string) error
	LookupDeleteGroup(ctx context.Context, group string) error
}

// lookupsAdapter serves gateway.LookupStore from the store.
type lookupsAdapter struct{ lookupRepository }

func lookupErr(err error) error {
	if errors.Is(err, state.ErrLookupNotFound) {
		return gateway.ErrLookupNotFound
	}
	return err
}

func (a lookupsAdapter) LookupDelete(ctx context.Context, group, key string) error {
	return lookupErr(a.lookupRepository.LookupDelete(ctx, group, key))
}

func (a lookupsAdapter) LookupDeleteGroup(ctx context.Context, group string) error {
	return lookupErr(a.lookupRepository.LookupDeleteGroup(ctx, group))
}
