package main

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/weavster-dev/weavster/internal/adapters"
	"github.com/weavster-dev/weavster/internal/codecs"
	"github.com/weavster-dev/weavster/internal/secrets"
)

// TestEnterpriseStubsDocumented: every Enterprise stub fails with exactly
// the error the support matrix documents for it.
func TestEnterpriseStubsDocumented(t *testing.T) {
	doc, err := os.ReadFile(filepath.Join("..", "..", "docs", "support-matrix.md"))
	if err != nil {
		t.Fatal(err)
	}
	section := string(doc)[strings.Index(string(doc), "## Enterprise-deferred stubs"):]
	section = section[:strings.Index(section[3:], "\n## ")+3]
	documented := map[string]string{}
	for _, m := range regexp.MustCompile("(?m)^\\| ([^|]+?) \\| `([^`]+)` \\|$").FindAllStringSubmatch(section, -1) {
		documented[m[1]] = m[2]
	}
	ctx := context.Background()
	_, dicomErr := codecs.DICOM().Parse(nil)
	_, brokerErr := adapters.BrokerSource{}.Read(ctx)
	_, dicomSourceErr := adapters.DICOMSource{}.Read(ctx)
	for stub, errs := range map[string][]error{
		"Broker queue/topic source and sink": {brokerErr, adapters.BrokerSink{}.Write(ctx, adapters.Message{})},
		"DICOM source and sink":              {dicomSourceErr, adapters.DICOMSink{}.Write(ctx, adapters.Message{})},
		"DICOM codec":                        {dicomErr},
		"KMS/Vault key rotation":             {secrets.EnterpriseKeyManager{}.Rotate(ctx, "k")},
	} {
		want, ok := documented[stub]
		if !ok {
			t.Errorf("the support matrix does not document %q (it has %v)", stub, documented)
			continue
		}
		for _, err := range errs {
			if err == nil || err.Error() != want {
				t.Errorf("%s fails with %v, documented as %q", stub, err, want)
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
		`{"id":"d","inputFormat":"dicom"}`:                         `/inputFormat: value must be one of \"json\", \"hl7v2\", \"xml\", \"delimited\"`,
	} {
		code, resp, _ := c.do(http.MethodPost, "/api/v1/flows", body, admin)
		if code != http.StatusBadRequest || !strings.Contains(resp, want) {
			t.Errorf("%s: %d %s", body, code, resp)
		}
	}
}

// TestDocsMakeNoEnterpriseClaims: a user-facing docs page that names an
// Enterprise feature says on the same line that this edition does not
// have it.
func TestDocsMakeNoEnterpriseClaims(t *testing.T) {
	internal := map[string]bool{"mvp-project-plan.md": true, "agent-onboarding.md": true, "prompt-3-kickoff.md": true, "documentation.md": true}
	term := regexp.MustCompile(`(?i)\b(oidc|saml|sso|single sign-on|opa|cedar|abac|siem|kafka|rabbitmq|nats|redis|vault|kms|dicom|ldap|multi-tenan\w*|multi-factor|mfa)\b`)
	marked := regexp.MustCompile(`(?i)enterprise|\bno\b|\bnot\b|unsupported|ignored|refused`)
	pages, _ := filepath.Glob(filepath.Join("..", "..", "docs", "*.md"))
	pages = append(pages, filepath.Join("..", "..", "README.md"))
	checked := 0
	for _, page := range pages {
		if internal[filepath.Base(page)] {
			continue
		}
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
