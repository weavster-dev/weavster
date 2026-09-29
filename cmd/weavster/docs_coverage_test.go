package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/weavster-dev/weavster/internal/compiler"
	"github.com/weavster-dev/weavster/internal/flowdef"
	"github.com/weavster-dev/weavster/internal/gateway"
	"github.com/weavster-dev/weavster/internal/serverconfig"
)

// The user documentation covers what the code offers (#107 §21): each
// check reads the list from the code, so a new key, route, command, or
// step without documentation fails here.

func docsPage(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "docs", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// allDocs is every published docs page, joined.
func allDocs(t *testing.T) string {
	t.Helper()
	pages, _ := filepath.Glob(filepath.Join("..", "..", "docs", "*.md"))
	var b strings.Builder
	for _, p := range pages {
		switch filepath.Base(p) {
		case "mvp-project-plan.md", "agent-onboarding.md", "prompt-3-kickoff.md":
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(data)
	}
	return b.String()
}

// yamlKeys lists the yaml key paths of a struct type, leaves only.
func yamlKeys(t reflect.Type, prefix string) []string {
	var out []string
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		name := strings.Split(f.Tag.Get("yaml"), ",")[0]
		if name == "" || name == "-" {
			continue
		}
		path := strings.TrimPrefix(prefix+"."+name, ".")
		if f.Type.Kind() == reflect.Struct {
			out = append(out, yamlKeys(f.Type, path)...)
			continue
		}
		out = append(out, path)
	}
	return out
}

// TestDocsServerConfig: every server configuration key is in its section
// of server-config.md (by its path under the section, or its last name).
func TestDocsServerConfig(t *testing.T) {
	page := docsPage(t, "server-config.md")
	sections := map[string]string{}
	for _, part := range regexp.MustCompile("(?m)^### `").Split(page, -1)[1:] {
		name := part[:strings.Index(part, "`")]
		sections[name] = part
	}
	for _, key := range yamlKeys(reflect.TypeOf(serverconfig.Config{}), "") {
		top, rest, _ := strings.Cut(key, ".")
		section, ok := sections[top]
		if !ok {
			t.Errorf("server-config.md has no `%s` section (for %s)", top, key)
			continue
		}
		last := rest[strings.LastIndex(rest, ".")+1:]
		if !strings.Contains(section, "`"+rest+"`") && !strings.Contains(section, "`"+last+"`") {
			t.Errorf("server-config.md does not document %s", key)
		}
	}
}

// TestDocsAPI: every API path in the OpenAPI contract appears in the docs
// (path parameters may be named differently).
func TestDocsAPI(t *testing.T) {
	var spec struct {
		Paths map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal([]byte(gateway.OpenAPISpec()), &spec); err != nil {
		t.Fatal(err)
	}
	param := regexp.MustCompile(`\{[^}]*\}`)
	docs := param.ReplaceAllString(expandSets(allDocs(t)), "{}")
	var missing []string
	for path := range spec.Paths {
		if !strings.Contains(docs, param.ReplaceAllString(path, "{}")) {
			missing = append(missing, path)
		}
	}
	sort.Strings(missing)
	for _, p := range missing {
		t.Errorf("no docs page mentions %s", p)
	}
}

// expandSets adds to text every expansion of the route shorthand the docs
// use for a set of paths: /flows/{deploy,undeploy}-all stands for
// /flows/deploy-all and /flows/undeploy-all.
func expandSets(text string) string {
	set := regexp.MustCompile(`\{([A-Za-z-]+(?:,[A-Za-z-]+)+)\}`)
	var b strings.Builder
	b.WriteString(text)
	for _, token := range regexp.MustCompile("`[^`]*`").FindAllString(text, -1) {
		if m := set.FindStringSubmatchIndex(token); m != nil {
			for _, alt := range strings.Split(token[m[2]:m[3]], ",") {
				b.WriteString("\n" + expandSets(token[:m[0]]+alt+token[m[1]:]))
			}
		}
	}
	return b.String()
}

// TestDocsCLI: every shell command and command-line flag is in cli.md.
func TestDocsCLI(t *testing.T) {
	page := docsPage(t, "cli.md")
	var help strings.Builder
	printShellHelp(&help)
	for _, item := range strings.Split(strings.TrimPrefix(strings.TrimSpace(help.String()), "commands: "), ", ") {
		for _, name := range strings.Split(strings.Fields(item)[0], "|") {
			if !strings.Contains(page, "`"+name) {
				t.Errorf("cli.md does not document the %s command", name)
			}
		}
	}
	for _, flag := range []string{"-a", "-u", "-p", "-s", "-v", "-c", "-ca", "-h", "-d"} {
		if !strings.Contains(page, "`"+flag+" ") && !strings.Contains(page, "`"+flag+"`") {
			t.Errorf("cli.md does not document the %s flag", flag)
		}
	}
	for _, sub := range []string{"weavster server", "weavster test", "weavster config validate", "weavster version"} {
		if !strings.Contains(page, sub) {
			t.Errorf("cli.md does not document %s", sub)
		}
	}
}

// TestDocsDSL: every transform step and its fields are in
// processing-messages.md.
func TestDocsDSL(t *testing.T) {
	page := docsPage(t, "processing-messages.md")
	step := reflect.TypeOf(compiler.Step{})
	for i := 0; i < step.NumField(); i++ {
		f := step.Field(i)
		kind := strings.Split(f.Tag.Get("json"), ",")[0]
		if !strings.Contains(page, "`"+kind+"`") {
			t.Errorf("processing-messages.md does not document the %s step", kind)
		}
		fields := f.Type.Elem()
		for j := 0; j < fields.NumField(); j++ {
			name := strings.Split(fields.Field(j).Tag.Get("json"), ",")[0]
			if name != "" && name != "-" && !strings.Contains(page, "`"+name+"`") {
				t.Errorf("processing-messages.md does not document %s.%s", kind, name)
			}
		}
	}
}

// TestDocsFlowDefinition: every property of a flow, its source, and its
// destinations (the flow schema) is in the docs.
func TestDocsFlowDefinition(t *testing.T) {
	docs := allDocs(t)
	var schema map[string]any
	if err := json.Unmarshal(flowdef.Schema, &schema); err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if props, ok := x["properties"].(map[string]any); ok {
				for name := range props {
					names[name] = true
				}
			}
			for _, child := range x {
				walk(child)
			}
		case []any:
			for _, child := range x {
				walk(child)
			}
		}
	}
	walk(schema)
	var missing []string
	for name := range names {
		if !strings.Contains(docs, "`"+name+"`") && !strings.Contains(docs, "`"+name+":") && !strings.Contains(docs, `"`+name+`"`) {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	for _, n := range missing {
		t.Errorf("no docs page documents the flow property %s", n)
	}
}

// TestDocsConfigAsCode: every section of a config-as-code document is in
// config-as-code.md.
func TestDocsConfigAsCode(t *testing.T) {
	page := docsPage(t, "config-as-code.md")
	for _, section := range configSections {
		if !strings.Contains(page, "`"+section+"`") && !strings.Contains(page, section+":") {
			t.Errorf("config-as-code.md does not document the %s section", section)
		}
	}
}

// TestLLMsTxt: agent-docs/llms.txt lists every published JSON Schema and
// the OpenAPI contract, and every local file it links to exists.
func TestLLMsTxt(t *testing.T) {
	dir := filepath.Join("..", "..", "agent-docs")
	index, err := os.ReadFile(filepath.Join(dir, "llms.txt"))
	if err != nil {
		t.Fatal(err)
	}
	schemas, _ := filepath.Glob(filepath.Join(dir, "schemas", "*.json"))
	for _, s := range append(schemas, filepath.Join(dir, "openapi.yaml")) {
		rel, _ := filepath.Rel(dir, s)
		if !strings.Contains(string(index), "]("+filepath.ToSlash(rel)+")") {
			t.Errorf("llms.txt does not list %s", rel)
		}
	}
	for _, m := range regexp.MustCompile(`\]\(([^)#]+)\)`).FindAllStringSubmatch(string(index), -1) {
		if strings.Contains(m[1], "://") {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, m[1])); err != nil {
			t.Errorf("llms.txt links to %s, which does not exist", m[1])
		}
	}
}
