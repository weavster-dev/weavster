package main

import (
	"github.com/weavster-dev/weavster/internal/config"
	"github.com/weavster-dev/weavster/internal/gateway"
)

// configValidator serves gateway.ConfigValidator with the config-as-code
// rules (internal/config).
type configValidator struct{}

func (configValidator) ValidateConfig(doc []byte) (gateway.ConfigSummary, error) {
	if err := config.Validate(doc); err != nil {
		return gateway.ConfigSummary{}, err
	}
	c, err := config.Parse(doc)
	if err != nil {
		return gateway.ConfigSummary{}, err
	}
	return gateway.ConfigSummary{
		Flows: len(c.Flows), Alerts: len(c.Alerts), Snippets: len(c.Snippets), SnippetLibraries: len(c.SnippetLibraries),
		Scripts: len(c.Scripts), ConfigMap: len(c.ConfigMap), Settings: len(c.Settings),
	}, nil
}
