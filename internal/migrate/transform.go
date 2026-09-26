package migrate

import (
	"encoding/json"
	"fmt"

	"github.com/weavster-dev/weavster/internal/config"
	"github.com/weavster-dev/weavster/internal/flowdef"
)

// Transform maps a legacy export to the new YAML DSL config (gap #1).
// Constructs that cannot be auto-translated (scripted filters/scripts) are
// flagged for review rather than silently dropped.
func Transform(le *LegacyExport, mappingVersion string) (*config.Config, []string, error) {
	if mappingVersion != MappingVersion {
		return nil, nil, fmt.Errorf("migrate: unknown mapping version %q", mappingVersion)
	}

	cfg := &config.Config{
		Version:  "1",
		Flows:    make(map[string]flowdef.Flow),
		Alerts:   make(map[string]config.Alert),
		Snippets: make(map[string]string),
		Scripts:  make(map[string]string),
		Map:      make(map[string]string),
		Settings: make(map[string]any),
	}

	var review []string

	for _, lf := range le.Flows {
		id := validName(lf.Name)
		if flowdef.Reserved(id) {
			id += "-flow"
		}
		for base, n := id, 2; cfg.Flows[id].ID != ""; n++ {
			id = fmt.Sprintf("%s-%d", base, n)
		}
		if id != lf.Name {
			review = append(review, "flow:"+lf.Name+":renamed:"+id)
		}
		f := flowdef.Flow{ID: id, Name: lf.Name, SourceType: lf.Source.Type, Enabled: lf.Enabled}
		if lf.Source.Path != "" {
			// Flow definitions carry no source settings yet (gap #1).
			review = append(review, "flow:"+lf.Name+":source-path")
		}
		for _, d := range lf.Destinations {
			if d.Type != "http" && d.Type != "file" {
				// Only http and file destinations exist; flag the rest.
				review = append(review, "flow:"+lf.Name+":destination:"+d.Name+":"+d.Type)
				continue
			}
			name := validName(d.Name)
			if name != d.Name {
				review = append(review, "flow:"+lf.Name+":destination-renamed:"+d.Name+":"+name)
			}
			f.Destinations = append(f.Destinations, flowdef.Destination{Name: name, Type: d.Type})
		}
		var steps []map[string]any
		for _, filt := range lf.Filters {
			if filt.Script != "" {
				// Inexpressible legacy script -> flagged for review (gap #1).
				review = append(review, "flow:"+lf.Name+":script-filter")
				continue
			}
			steps = append(steps, map[string]any{"map": map[string]string{"from": filt.From, "to": filt.To}})
		}
		if len(steps) > 0 {
			f.Transform, _ = json.Marshal(map[string]any{"steps": steps}) // plain maps always encode
		}
		cfg.Flows[id] = f
	}

	for _, s := range le.Snippets {
		cfg.Snippets[s.Name] = s.Body
	}
	for _, s := range le.Scripts {
		cfg.Scripts[s.Name] = s.Body
		review = append(review, "script:"+s.Name) // scripts are not auto-translated
	}
	for _, e := range le.ConfigMap {
		cfg.Map[e.Key] = e.Value
	}
	return cfg, review, nil
}

// validName turns a legacy name into a valid flow id or destination name
// (flow.schema.json: 1-128 of A-Z a-z 0-9 . _ -): other characters become
// "-".
func validName(name string) string {
	b := []byte(name)
	for i, c := range b {
		valid := c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-'
		if !valid {
			b[i] = '-'
		}
	}
	switch {
	case len(b) == 0:
		return "unnamed"
	case len(b) > 128:
		b = b[:128]
	}
	return string(b)
}
