package migrate

import (
	"encoding/json"
	"fmt"

	"github.com/weavster-dev/weavster/internal/artifact"
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
		Version:          "1",
		Flows:            make(map[string]flowdef.Flow),
		Alerts:           make(map[string]artifact.Alert),
		Snippets:         make(map[string]artifact.Snippet),
		SnippetLibraries: make(map[string]artifact.SnippetLibrary),
		Scripts:          make(map[string]string),
		ConfigMap:        make(map[string]string),
		Settings:         make(map[string]any),
	}

	var review []string

	for _, lf := range le.Flows {
		id := validName(lf.Name)
		if flowdef.Reserved(id) {
			id += "-flow"
		}
		id = unique(id, func(c string) bool { return cfg.Flows[c].ID != "" })
		if id != lf.Name {
			review = append(review, "flow:"+lf.Name+":renamed:"+id)
		}
		f := flowdef.Flow{ID: id, Name: lf.Name, SourceType: lf.Source.Type, Enabled: lf.Enabled}
		if lf.Source.Path != "" {
			// Flow definitions carry no source settings yet (gap #1).
			review = append(review, "flow:"+lf.Name+":source-path")
		}
		destNames := map[string]bool{}
		for _, d := range lf.Destinations {
			if d.Type != "http" && d.Type != "file" {
				// Only http and file destinations exist; flag the rest.
				review = append(review, "flow:"+lf.Name+":destination:"+d.Name+":"+d.Type)
				continue
			}
			name := unique(validName(d.Name), func(c string) bool { return destNames[c] })
			destNames[name] = true
			if name != d.Name {
				review = append(review, "flow:"+lf.Name+":destination-renamed:"+d.Name+":"+name)
			}
			// Legacy destinations carry no URL or directory; the flow runs
			// only once they are set.
			setting := "url"
			if d.Type == "file" {
				setting = "dir"
			}
			review = append(review, "flow:"+lf.Name+":destination:"+name+":set-"+setting)
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

	// Snippet, script, and config-map names follow the same name rule as
	// flow ids; a changed name is flagged for review.
	rename := func(kind, name string, taken func(string) bool) string {
		n := unique(validName(name), taken)
		if n != name {
			review = append(review, kind+":"+name+":renamed:"+n)
		}
		return n
	}
	for _, s := range le.Snippets {
		name := rename("snippet", s.Name, func(c string) bool { _, ok := cfg.Snippets[c]; return ok })
		cfg.Snippets[name] = artifact.Snippet{Name: name, Code: s.Body}
	}
	for _, s := range le.Scripts {
		name := rename("script", s.Name, func(c string) bool { _, ok := cfg.Scripts[c]; return ok })
		cfg.Scripts[name] = s.Body
		review = append(review, "script:"+name) // scripts are not auto-translated
	}
	for _, e := range le.ConfigMap {
		name := rename("configmap", e.Key, func(c string) bool { _, ok := cfg.ConfigMap[c]; return ok })
		cfg.ConfigMap[name] = e.Value
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

// unique returns name, or name with the smallest "-N" suffix that is not
// taken, shortened so the result stays within 128 characters.
func unique(name string, taken func(string) bool) string {
	if !taken(name) {
		return name
	}
	for n := 2; ; n++ {
		suffix := fmt.Sprintf("-%d", n)
		base := name
		if len(base)+len(suffix) > 128 {
			base = base[:128-len(suffix)]
		}
		if c := base + suffix; !taken(c) {
			return c
		}
	}
}
