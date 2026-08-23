package migrate

// Mapping is one legacy->YAML mapping-table row (gap #1: the mapper is
// versioned and documented).
type Mapping struct {
	Legacy         string
	New            string
	AutoTranslated bool
}

// MappingVersion is the current legacy->YAML mapper version.
const MappingVersion = "1"

// MappingTable documents how each legacy construct maps to the new config
// (gap #1).
func MappingTable() []Mapping {
	return []Mapping{
		{Legacy: "flow/channel", New: "config.Flow (YAML DSL)", AutoTranslated: true},
		{Legacy: "source connector", New: "config.Source", AutoTranslated: true},
		{Legacy: "destination connector", New: "config.Destination", AutoTranslated: true},
		{Legacy: "declarative filter (from/to)", New: "config.Transform (map step)", AutoTranslated: true},
		{Legacy: "scripted filter", New: "WASI module stub (flagged for review)", AutoTranslated: false},
		{Legacy: "code snippet", New: "config.Snippets", AutoTranslated: true},
		{Legacy: "global script", New: "config.Scripts (flagged for review)", AutoTranslated: false},
		{Legacy: "config map entry", New: "config.Map", AutoTranslated: true},
		{Legacy: "message history", New: "metadata + references (--with-content opt-in)", AutoTranslated: true},
	}
}
