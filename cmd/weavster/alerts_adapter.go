package main

import (
	"context"
	"sync"

	"github.com/weavster-dev/weavster/internal/gateway"
)

// alertKind is the config-item kind alert definitions are stored as.
const alertKind = "alerts"

// alertsAdapter serves gateway.AlertStore from the item store. mu makes
// create-without-overwrite and enable/disable one step each, and orders
// deletes with them so a delete is never undone (one server per database,
// D-41).
type alertsAdapter struct {
	repo itemRepository
	mu   *sync.Mutex
}

func (a alertsAdapter) ListAlerts(ctx context.Context) ([]gateway.Alert, error) {
	return listDocs[gateway.Alert](ctx, a.repo, alertKind)
}

func (a alertsAdapter) GetAlert(ctx context.Context, id string) (gateway.Alert, error) {
	return getDoc[gateway.Alert](ctx, a.repo, alertKind, id, gateway.ErrAlertNotFound)
}

func (a alertsAdapter) SaveAlerts(ctx context.Context, list []gateway.Alert, create bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return saveDocs(ctx, a.repo, alertKind, list, func(al gateway.Alert) string { return al.ID }, create, gateway.ErrAlertExists)
}

func (a alertsAdapter) DeleteAlert(ctx context.Context, id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return deleteDoc(ctx, a.repo, alertKind, id, gateway.ErrAlertNotFound)
}

func (a alertsAdapter) SetAlertEnabled(ctx context.Context, id string, enabled bool) (gateway.Alert, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	al, err := a.GetAlert(ctx, id)
	if err != nil {
		return al, err
	}
	al.Enabled = enabled
	return al, saveDocs(ctx, a.repo, alertKind, []gateway.Alert{al}, func(al gateway.Alert) string { return al.ID }, false, nil)
}
