package migrate

import "encoding/xml"

// LegacyExport is the legacy XML/archive export format consumed by the ETL
// (gap #1). The exact legacy format is pinned by this schema.
type LegacyExport struct {
	XMLName   xml.Name        `xml:"weavster-export"`
	Flows     []LegacyFlow    `xml:"flows>flow"`
	Snippets  []LegacySnippet `xml:"snippets>snippet"`
	Scripts   []LegacyScript  `xml:"scripts>script"`
	Users     []LegacyUser    `xml:"users>user"`
	ConfigMap []LegacyEntry   `xml:"configmap>entry"`
	Messages  []LegacyMessage `xml:"messages>message"`
}

// LegacyFlow is a legacy flow (channel).
type LegacyFlow struct {
	Name         string              `xml:"name,attr"`
	Enabled      bool                `xml:"enabled,attr"`
	Source       LegacySource        `xml:"source"`
	Destinations []LegacyDestination `xml:"destination"`
	Filters      []LegacyFilter      `xml:"filter"`
}

// LegacySource is a legacy source connector.
type LegacySource struct {
	Type string `xml:"type,attr"`
	Path string `xml:"path,attr"`
}

// LegacyDestination is a legacy destination connector.
type LegacyDestination struct {
	Name string `xml:"name,attr"`
	Type string `xml:"type,attr"`
}

// LegacyFilter is a legacy filter/transform step.
type LegacyFilter struct {
	From   string `xml:"from,attr"`
	To     string `xml:"to,attr"`
	Script string `xml:"script,attr"`
}

// LegacySnippet is a legacy code snippet.
type LegacySnippet struct {
	Name string `xml:"name,attr"`
	Body string `xml:",chardata"`
}

// LegacyScript is a legacy global script.
type LegacyScript struct {
	Name string `xml:"name,attr"`
	Body string `xml:",chardata"`
}

// LegacyUser is a legacy user account.
type LegacyUser struct {
	Name  string `xml:"name,attr"`
	Email string `xml:"email,attr"`
	Org   string `xml:"org,attr"`
}

// LegacyEntry is a legacy config-map entry.
type LegacyEntry struct {
	Key   string `xml:"key,attr"`
	Value string `xml:"value,attr"`
}

// LegacyMessage is legacy message-history metadata (content is opt-in).
type LegacyMessage struct {
	ID     string `xml:"id,attr"`
	Flow   string `xml:"flow,attr"`
	At     string `xml:"at,attr"`
	Status string `xml:"status,attr"`
}
