// Package flowdef is the single flow-definition model: the JSON document
// the flow API accepts and returns, and the flows of a config-as-code
// document, with its JSON Schema (flow.schema.json) and validation.
package flowdef

import "encoding/json"

// Flow is a flow definition. Status and StoppedDestinations are runtime
// state: the API reports them, but definitions (create, update, import,
// config-as-code) must not set them.
type Flow struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	SourceType string `json:"sourceType"`
	Status     string `json:"status,omitempty"`
	Enabled    bool   `json:"enabled"`
	// InitialState is the status automatic deployment at server start
	// gives the flow: started (when empty), paused, or stopped.
	InitialState string `json:"initialState,omitempty"`
	// DependsOn lists flows this flow requires (ids); kept acyclic.
	DependsOn []string `json:"dependsOn,omitempty"`
	// StoppedDestinations is runtime state (like Status): destinations held
	// by POST .../destinations/{name}/stop.
	StoppedDestinations []string `json:"stoppedDestinations,omitempty"`
	// Transform is the flow's YAML DSL transform as a JSON object (the
	// transform.schema.json shape), kept unparsed.
	Transform    json.RawMessage `json:"transform,omitempty"`
	Destinations []Destination   `json:"destinations,omitempty"`
	// ResponseSelector names the destination whose reply is returned to
	// the sender of a message.
	ResponseSelector string `json:"responseSelector,omitempty"`
}

// Destination is one delivery target of a flow.
type Destination struct {
	Name string `json:"name"`
	Type string `json:"type"`
	URL  string `json:"url,omitempty"`
	Dir  string `json:"dir,omitempty"`
	// Transform is this destination's own DSL transform (filter steps
	// included), applied to the flow's output; kept unparsed.
	Transform json.RawMessage `json:"transform,omitempty"`
	// ResponseTransform is applied to this destination's reply.
	ResponseTransform json.RawMessage `json:"responseTransform,omitempty"`
}
