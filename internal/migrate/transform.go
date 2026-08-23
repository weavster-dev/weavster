package migrate

import (
	"fmt"

	"github.com/weavster-dev/weavster/internal/config"
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
		Flows:    make(map[string]config.Flow),
		Alerts:   make(map[string]config.Alert),
		Snippets: make(map[string]string),
		Scripts:  make(map[string]string),
		Map:      make(map[string]string),
		Settings: make(map[string]any),
	}

	var review []string

	for _, lf := range le.Flows {
		f := config.Flow{
			Name:   lf.Name,
			Source: config.Source{Type: lf.Source.Type, Config: map[string]any{"path": lf.Source.Path}},
		}
		for _, d := range lf.Destinations {
			f.Destinations = append(f.Destinations, config.Destination{Name: d.Name, Type: d.Type})
		}
		for _, filt := range lf.Filters {
			if filt.Script != "" {
				// Inexpressible legacy script -> flagged for review (gap #1).
				review = append(review, "flow:"+lf.Name+":script-filter")
				continue
			}
			f.Transforms = append(f.Transforms, config.Transform{
				Kind: "map",
				Spec: map[string]any{"from": filt.From, "to": filt.To},
			})
		}
		cfg.Flows[lf.Name] = f
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
