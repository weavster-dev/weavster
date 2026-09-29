package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/weavster-dev/weavster/internal/adapters"
	"github.com/weavster-dev/weavster/internal/codecs"
	"github.com/weavster-dev/weavster/internal/enterprise"
	"github.com/weavster-dev/weavster/internal/secrets"
	"gopkg.in/yaml.v3"
)

// TestEnterpriseStubsDocumented: every Enterprise stub fails with exactly
// the error the support matrix documents for it.
func TestEnterpriseStubsDocumented(t *testing.T) {
	doc, err := os.ReadFile(filepath.Join("..", "..", "docs", "support-matrix.md"))
	if err != nil {
		t.Fatal(err)
	}
	start := strings.Index(string(doc), "## Enterprise-deferred stubs")
	if start < 0 {
		t.Fatal(`support-matrix.md has no "## Enterprise-deferred stubs" section`)
	}
	section := string(doc)[start:]
	if end := strings.Index(section[3:], "\n## "); end >= 0 {
		section = section[:end+3]
	}
	documented := map[string]string{}
	for _, m := range regexp.MustCompile("(?m)^\\| ([^|]+?) \\| `([^`]+)` \\|$").FindAllStringSubmatch(section, -1) {
		documented[m[1]] = m[2]
	}
	ctx := context.Background()
	_, dicomParse := codecs.DICOM().Parse(nil)
	_, dicomSerialize := codecs.DICOM().Serialize(nil)
	_, dicomAck := codecs.DICOM().Acknowledge(nil)
	_, brokerErr := adapters.BrokerSource{}.Read(ctx)
	_, dicomSourceErr := adapters.DICOMSource{}.Read(ctx)
	for stub, errs := range map[string][]error{
		"Broker queue/topic source and sink": {brokerErr, adapters.BrokerSink{}.Write(ctx, adapters.Message{})},
		"DICOM source and sink":              {dicomSourceErr, adapters.DICOMSink{}.Write(ctx, adapters.Message{})},
		"DICOM codec":                        {dicomParse, dicomSerialize, dicomAck},
		"KMS/Vault key rotation":             {secrets.EnterpriseKeyManager{}.Rotate(ctx, "k")},
	} {
		want, ok := documented[stub]
		if !ok {
			t.Errorf("the support matrix does not document %q (it has %v)", stub, documented)
			continue
		}
		for _, err := range errs {
			if err == nil || err.Error() != want || !errors.Is(err, enterprise.ErrNotImplemented) {
				t.Errorf("%s fails with %v, documented as %q (and must be enterprise.ErrNotImplemented)", stub, err, want)
			}
		}
	}
	if len(documented) != 4 {
		t.Errorf("documented stubs = %v", documented)
	}
}

// TestEnterpriseAdaptersRefused: the running server refuses flows that
// would select an Enterprise adapter or codec, with a 400 naming what this
// edition supports.
func TestEnterpriseAdaptersRefused(t *testing.T) {
	addr := freeAddr(t)
	cfg := writeConfig(t, "listen: {address: \""+addr+"\"}\n"+storeConfig(t))
	stop := startCLI(t, []string{"server", "--config", cfg}, "http://"+addr+"/api/openapi.yaml")
	defer stop()
	c := apiClient{t: t, base: "http://" + addr}
	admin := basic(bootstrapAdmin, testAdminPassword)
	for body, want := range map[string]string{
		`{"id":"a","destinations":[{"name":"q","type":"broker"}]}`: `/destinations/0/type: value must be one of \"http\", \"file\", \"mllp\", \"flow\", \"database\"`,
		`{"id":"b","destinations":[{"name":"q","type":"dicom"}]}`:  `/destinations/0/type: value must be one of`,
		`{"id":"c","source":{"type":"broker"}}`:                    `/source/type: value must be`,
		`{"id":"d","inputFormat":"dicom"}`:                         `/inputFormat: value must be one of \"json\", \"hl7v2\", \"xml\", \"delimited\", null"`,
	} {
		code, resp, _ := c.do(http.MethodPost, "/api/v1/flows", body, admin)
		if code != http.StatusBadRequest || !strings.Contains(resp, want) {
			t.Errorf("%s: %d %s", body, code, resp)
		}
	}
}

// TestDocsMakeNoEnterpriseClaims: every page the docs site publishes (and
// the README) that names an Enterprise feature says on the same line, in
// so many words, that this edition does not have it.
func TestDocsMakeNoEnterpriseClaims(t *testing.T) {
	root := filepath.Join("..", "..")
	mk, err := os.ReadFile(filepath.Join(root, "mkdocs.yml"))
	if err != nil {
		t.Fatal(err)
	}
	excluded := map[string]bool{}
	var cfg struct {
		ExcludeDocs string `yaml:"exclude_docs"`
	}
	_ = yaml.Unmarshal(mk, &cfg) // mkdocs.yml also holds !!python tags; the key still decodes
	for _, f := range strings.Fields(cfg.ExcludeDocs) {
		excluded[f] = true
	}
	if !excluded["mvp-project-plan.md"] {
		t.Fatalf("exclude_docs not read from mkdocs.yml: %v", excluded)
	}
	term := regexp.MustCompile(`(?i)\b(oidc|saml|sso|single sign-on|opa|cedar|abac|siem|kafka|rabbitmq|nats|redis|vault|kms|dicom|ldap|multi-tenan\w*|multi-factor|mfa|kubernetes operator|object storage)\b`)
	marked := regexp.MustCompile(`(?i)enterprise|this edition|not available|unsupported|ignored|refused`)
	pages := []string{filepath.Join(root, "README.md")}
	_ = filepath.WalkDir(filepath.Join(root, "docs"), func(path string, d os.DirEntry, err error) error {
		rel, _ := filepath.Rel(filepath.Join(root, "docs"), path)
		if err == nil && !d.IsDir() && strings.HasSuffix(path, ".md") && !excluded[filepath.ToSlash(rel)] {
			pages = append(pages, path)
		}
		return nil
	})
	checked := 0
	for _, page := range pages {
		b, err := os.ReadFile(page)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(b), "\n") {
			if term.MatchString(line) {
				checked++
				if !marked.MatchString(line) {
					t.Errorf("%s:%d names an Enterprise feature without saying this edition lacks it: %s", filepath.Base(page), i+1, line)
				}
			}
		}
	}
	if checked == 0 {
		t.Error("no Enterprise terms found: the check is not running")
	}
}
