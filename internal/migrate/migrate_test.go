package migrate

import (
	"context"
	"strings"
	"testing"

	"github.com/weavster-dev/weavster/internal/config"
)

const legacyXML = `<?xml version="1.0"?>
<weavster-export>
  <flows>
    <flow name="admit" enabled="true">
      <source type="file" path="/incoming/patients"/>
      <destination name="his-mllp" type="tcp"/>
      <filter from="PID.5.1" to="patient.lastName"/>
      <filter script="legacyInlinedScript()"/>
    </flow>
  </flows>
  <snippets>
    <snippet name="normalize">reusable()</snippet>
  </snippets>
  <scripts>
    <script name="global-init">init()</script>
  </scripts>
  <users>
    <user name="admin" email="admin@example.com" org="ops"/>
  </users>
  <configmap>
    <entry key="region" value="us-east-1"/>
  </configmap>
  <messages>
    <message id="m1" flow="admit" at="2026-08-23T10:00:00Z" status="sent"/>
  </messages>
</weavster-export>`

func TestDryRun(t *testing.T) {
	rep, err := DryRun([]byte(legacyXML), Options{})
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if rep.Flows != 1 || rep.Snippets != 1 || rep.Scripts != 1 || rep.Users != 1 || rep.ConfigMapEntry != 1 || rep.Messages != 1 {
		t.Errorf("counts = %+v", rep)
	}
	if len(rep.ReviewRequired) != 4 {
		t.Errorf("review list = %v, want 4 (source path, tcp destination, script filter, global script)", rep.ReviewRequired)
	}
	if rep.Config.Flows["admit"].SourceType != "file" {
		t.Errorf("transformed flow = %+v", rep.Config.Flows["admit"])
	}
}

func TestRunLoadsConfig(t *testing.T) {
	store := config.NewMemStore()
	rep, err := Run(context.Background(), []byte(legacyXML), Options{}, store, false)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if rep == nil {
		t.Fatal("nil report")
	}
	arts, err := store.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := arts["flow/admit"]; !ok {
		t.Errorf("flow artifact missing: %v", arts)
	}
	if _, ok := arts["snippet/normalize"]; !ok {
		t.Errorf("snippet artifact missing: %v", arts)
	}
}

func TestExtractRejectsMalformedXML(t *testing.T) {
	legacy, err := Extract([]byte("<weavster-export><flows></weavster-export>"))
	if err == nil {
		t.Fatal("Extract() error = nil, want malformed XML error")
	}
	if legacy != nil {
		t.Errorf("Extract() legacy = %+v, want nil on malformed XML", legacy)
	}
}

func TestTransformDeclarativeFilter(t *testing.T) {
	le, err := Extract([]byte(legacyXML))
	if err != nil {
		t.Fatal(err)
	}
	cfg, _, err := Transform(le, MappingVersion)
	if err != nil {
		t.Fatal(err)
	}
	flow := cfg.Flows["admit"]
	if string(flow.Transform) != `{"steps":[{"map":{"from":"PID.5.1","to":"patient.lastName"}}]}` {
		t.Errorf("transform = %s", flow.Transform)
	}
	if len(flow.Destinations) != 0 {
		t.Errorf("destinations = %+v; the tcp destination must be flagged, not kept", flow.Destinations)
	}
}

func TestMappingTableVersioned(t *testing.T) {
	table := MappingTable()
	if len(table) == 0 {
		t.Fatal("empty mapping table")
	}
	if _, _, err := Transform(&LegacyExport{}, "bogus"); err == nil {
		t.Error("unknown mapping version must be rejected")
	}
}

func TestTransformNamesAndEnabled(t *testing.T) {
	le := &LegacyExport{Flows: []LegacyFlow{
		{Name: "ADT Inbound", Enabled: true, Destinations: []LegacyDestination{{Name: "HIS main", Type: "http"}, {Name: "HIS-main", Type: "file"}}},
		{Name: "import"},
		{Name: "ADT/Inbound"},
		{Name: ""},
	}}
	cfg, review, err := Transform(le, MappingVersion)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"ADT-Inbound", "import-flow", "ADT-Inbound-2", "unnamed"} {
		if cfg.Flows[id].ID != id {
			t.Errorf("flow %s missing: %v", id, cfg.Flows)
		}
	}
	if f := cfg.Flows["ADT-Inbound"]; !f.Enabled || f.Name != "ADT Inbound" || f.Destinations[0].Name != "HIS-main" {
		t.Errorf("ADT-Inbound = %+v", f)
	}
	if f := cfg.Flows["ADT-Inbound"]; len(f.Destinations) != 2 || f.Destinations[1].Name != "HIS-main-2" {
		t.Errorf("colliding destination names = %+v", f.Destinations)
	}
	// four flow renames, two destination renames, two destinations needing a url/dir
	if len(review) != 8 {
		t.Errorf("review = %v", review)
	}
	store := config.NewMemStore()
	if err := Load(context.Background(), store, cfg); err != nil {
		t.Errorf("Load of renamed flows: %v", err)
	}
	if got := validName(strings.Repeat("a", 200)); len(got) != 128 {
		t.Errorf("validName length = %d", len(got))
	}
}

func TestUniqueStaysWithinLimit(t *testing.T) {
	long := strings.Repeat("a", 128)
	got := unique(long, func(c string) bool { return c == long })
	if len(got) != 128 || !strings.HasSuffix(got, "-2") {
		t.Errorf("unique = %q (%d chars)", got, len(got))
	}
}
