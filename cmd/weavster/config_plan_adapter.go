package main

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/weavster-dev/weavster/internal/artifact"
	"github.com/weavster-dev/weavster/internal/config"
	"github.com/weavster-dev/weavster/internal/flowdef"
	"github.com/weavster-dev/weavster/internal/gateway"
)

// configPlanner serves gateway.ConfigPlanner with config.LivePlan.
type configPlanner struct{}

func (configPlanner) PlanConfig(doc []byte, bundle gateway.ConfigBundle) (gateway.ConfigPlan, error) {
	desired, err := config.ParseValid(doc)
	if err != nil {
		return gateway.ConfigPlan{}, fmt.Errorf("%w: %w", gateway.ErrInvalidConfig, err)
	}
	live, err := liveConfigOf(bundle)
	if err != nil {
		return gateway.ConfigPlan{}, err
	}
	plan := config.LivePlan(desired, live)
	out := gateway.ConfigPlan{
		Fingerprint: plan.Fingerprint, Added: nonNil(plan.Added), Updated: nonNil(plan.Updated), Removed: nonNil(plan.Removed),
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

// liveConfigOf turns the live bundle into the config-as-code model.
func liveConfigOf(b gateway.ConfigBundle) (*config.Config, error) {
	c := &config.Config{
		Flows: map[string]flowdef.Flow{}, Alerts: map[string]artifact.Alert{}, Snippets: map[string]artifact.Snippet{},
		SnippetLibraries: map[string]artifact.SnippetLibrary{}, Scripts: map[string]string{}, ConfigMap: map[string]string{},
		Settings: map[string]any{},
	}
	for _, f := range b.Flows {
		c.Flows[f.ID] = f
	}
	for _, a := range b.Alerts {
		c.Alerts[a.ID] = a
	}
	for _, s := range b.Snippets {
		c.Snippets[s.Name] = s
	}
	for _, l := range b.SnippetLibraries {
		c.SnippetLibraries[l.Name] = l
	}
	texts := map[string]map[string]json.RawMessage{"scripts": b.Scripts}
	if b.ConfigMap != nil {
		texts["configmap"] = *b.ConfigMap
	}
	for kind, items := range texts {
		dst := map[string]map[string]string{"scripts": c.Scripts, "configmap": c.ConfigMap}[kind]
		for name, raw := range items {
			var v string
			if err := json.Unmarshal(raw, &v); err != nil {
				return nil, fmt.Errorf("%s %s: %w", kind, name, err)
			}
			dst[name] = v
		}
	}
	for name, raw := range b.Settings {
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
