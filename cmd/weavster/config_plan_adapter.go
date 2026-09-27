package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/weavster-dev/weavster/internal/artifact"
	"github.com/weavster-dev/weavster/internal/config"
	"github.com/weavster-dev/weavster/internal/flowdef"
	"github.com/weavster-dev/weavster/internal/gateway"
)

// configPlanner serves gateway.ConfigPlanner: it rebuilds the live
// configuration from the server's stores and plans the document against it.
type configPlanner struct {
	transfer gateway.FlowTransfer
	alerts   gateway.AlertStore
	snippets gateway.SnippetStore
	items    gateway.ItemStore
}

func (p configPlanner) PlanConfig(ctx context.Context, doc []byte) (gateway.ConfigPlan, error) {
	desired, err := config.ParseValid(doc)
	if err != nil {
		return gateway.ConfigPlan{}, fmt.Errorf("%w: %w", gateway.ErrInvalidConfig, err)
	}
	live, err := p.live(ctx)
	if err != nil {
		return gateway.ConfigPlan{}, err
	}
	plan := config.LivePlan(desired, live)
	out := gateway.ConfigPlan{
		Fingerprint: config.Fingerprint(live), Added: nonNil(plan.Added), Updated: nonNil(plan.Updated), Removed: nonNil(plan.Removed),
		Unchanged: plan.Unchanged, Changes: []gateway.ConfigChange{}, Text: plan.Text(),
	}
	for _, c := range plan.Changes {
		gc := gateway.ConfigChange{Key: c.Key, Action: c.Action, Before: c.Before, After: c.After}
		for _, f := range c.Fields {
			gc.Fields = append(gc.Fields, gateway.ConfigFieldChange{Path: f.Path, Before: f.Before, After: f.After})
		}
		out.Changes = append(out.Changes, gc)
	}
	return out, nil
}

// live rebuilds the server's configuration as a config-as-code document:
// flows without runtime state, and every other artifact as stored.
func (p configPlanner) live(ctx context.Context) (*config.Config, error) {
	c := &config.Config{
		Flows: map[string]flowdef.Flow{}, Alerts: map[string]artifact.Alert{}, Snippets: map[string]artifact.Snippet{},
		SnippetLibraries: map[string]artifact.SnippetLibrary{}, Scripts: map[string]string{}, ConfigMap: map[string]string{},
		Settings: map[string]any{},
	}
	flows, err := p.transfer.Export(ctx, nil)
	if err != nil {
		return nil, err
	}
	for _, f := range flows {
		c.Flows[f.ID] = f
	}
	alerts, err := p.alerts.ListAlerts(ctx)
	if err != nil {
		return nil, err
	}
	for _, a := range alerts {
		c.Alerts[a.ID] = a
	}
	snippets, err := p.snippets.ListSnippets(ctx)
	if err != nil {
		return nil, err
	}
	for _, s := range snippets {
		c.Snippets[s.Name] = s
	}
	libs, err := p.snippets.ListLibraries(ctx)
	if err != nil {
		return nil, err
	}
	for _, l := range libs {
		c.SnippetLibraries[l.Name] = l
	}
	for kind, dst := range map[string]map[string]string{"scripts": c.Scripts, "configmap": c.ConfigMap} {
		items, err := p.items.ListItems(ctx, kind)
		if err != nil {
			return nil, err
		}
		for name, raw := range items {
			var v string
			if err := json.Unmarshal(raw, &v); err != nil {
				return nil, fmt.Errorf("%s %s: %w", kind, name, err)
			}
			dst[name] = v
		}
	}
	settings, err := p.items.ListItems(ctx, "settings")
	if err != nil {
		return nil, err
	}
	for name, raw := range settings {
		var v any
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber() // keep large integers exact
		if err := dec.Decode(&v); err != nil {
			return nil, fmt.Errorf("settings %s: %w", name, err)
		}
		c.Settings[name] = v
	}
	return c, nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
