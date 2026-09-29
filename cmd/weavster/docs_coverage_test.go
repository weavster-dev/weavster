package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/weavster-dev/weavster/internal/config"
	"github.com/weavster-dev/weavster/internal/flowdef"
	"github.com/weavster-dev/weavster/internal/gateway"
	"github.com/weavster-dev/weavster/internal/serverconfig"
)

// The user documentation covers what the code offers (#107 §21): each
// check reads its list from the code (help output, types, schemas, the
// OpenAPI contract), so a new key, route, command, flag, or step without
// documentation fails here.

// publishedPages are the docs pages the site publishes (every .md under
// docs/ but those mkdocs.yml excludes), by their path under docs/.
func publishedPages(t *testing.T) map[string]string {
	t.Helper()
	root := filepath.Join("..", "..")
	mk, err := os.ReadFile(filepath.Join(root, "mkdocs.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		ExcludeDocs string `yaml:"exclude_docs"`
	}
	_ = yaml.Unmarshal(mk, &cfg) // mkdocs.yml may hold !!python tags; this key still decodes
	excluded := map[string]bool{}
	for _, f := range strings.Fields(cfg.ExcludeDocs) {
		excluded[f] = true
	}
	if len(excluded) == 0 {
		t.Fatal("exclude_docs not read from mkdocs.yml")
	}
	pages := map[string]string{}
	docs := filepath.Join(root, "docs")
	err = filepath.WalkDir(docs, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".md") {
			return err
		}
		rel, _ := filepath.Rel(docs, path)
		rel = filepath.ToSlash(rel)
		if excluded[rel] {
			return nil
		}
		b, err := os.ReadFile(path)
		pages[rel] = string(b)
		return err
	})
	if err != nil || len(pages) == 0 {
		t.Fatalf("docs pages: %v (%d)", err, len(pages))
	}
	return pages
}

func docsPage(t *testing.T, name string) string {
	t.Helper()
	page, ok := publishedPages(t)[name]
	if !ok {
		t.Fatalf("docs/%s is not a published page", name)
	}
	return page
}

// fields walks a type's fields by tag (json or yaml), through pointers,
// slices, maps, and nested structs, calling visit with each field's path.
func fields(typ reflect.Type, tag, prefix string, visit func(path string, leaf bool)) {
	for typ.Kind() == reflect.Pointer || typ.Kind() == reflect.Slice || typ.Kind() == reflect.Map {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		return
	}
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		name := strings.Split(f.Tag.Get(tag), ",")[0]
		if name == "-" || !f.IsExported() {
			continue
		}
		if name == "" { // inline or untagged: its fields are this struct's
			fields(f.Type, tag, prefix, visit)
			continue
		}
		path := strings.TrimPrefix(prefix+"."+name, ".")
		elem := f.Type
		for elem.Kind() == reflect.Pointer || elem.Kind() == reflect.Slice || elem.Kind() == reflect.Map {
			elem = elem.Elem()
		}
		leaf := elem.Kind() != reflect.Struct || elem.PkgPath() == "time"
		visit(path, leaf)
		if !leaf {
			fields(f.Type, tag, path, visit)
		}
	}
}

// TestDocsServerConfig: every server configuration key is in its
// section of server-config.md, by its path under the section.
func TestDocsServerConfig(t *testing.T) {
	page := docsPage(t, "server-config.md")
	sections := map[string]string{}
	for _, part := range regexp.MustCompile("(?m)^### `").Split(page, -1)[1:] {
		sections[part[:strings.Index(part, "`")]] = part
	}
	fields(reflect.TypeOf(serverconfig.Config{}), "yaml", "", func(key string, leaf bool) {
		if !leaf {
			return
		}
		top, rest, _ := strings.Cut(key, ".")
		section, ok := sections[top]
		switch {
		case !ok:
			t.Errorf("server-config.md has no `%s` section (for %s)", top, key)
		case !strings.Contains(section, "`"+rest+"`") && !strings.Contains(section, "`"+rest+"."):
			t.Errorf("server-config.md does not document %s (as `%s` in its `%s` section)", key, rest, top)
		}
	})
}

// TestDocsAPI: every operation in the OpenAPI contract (method and exact
// path; path parameters may be named differently) is in the docs, the
// method right before the path on one line (or the line says the path takes
// all methods).
func TestDocsAPI(t *testing.T) {
	var spec struct {
		Paths map[string]map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal([]byte(gateway.OpenAPISpec()), &spec); err != nil {
		t.Fatal(err)
	}
	param := regexp.MustCompile(`\{[^}]*\}`)
	var docs strings.Builder
	for _, page := range publishedPages(t) {
		docs.WriteString(expandSets(page) + "\n")
	}
	lines := strings.Split(param.ReplaceAllString(docs.String(), "{}"), "\n")
	var missing []string
	for path, ops := range spec.Paths {
		norm := param.ReplaceAllString(path, "{}")
		// The exact path: not followed by more path.
		exact := regexp.QuoteMeta(norm) + `(?:[^A-Za-z0-9/{}_.-]|$)`
		allMethods := regexp.MustCompile(exact + ".*all methods")
		for method := range ops {
			m := strings.ToUpper(method)
			if m == "PARAMETERS" {
				continue
			}
			// The method names this route: it comes right before the path,
			// perhaps in a list of methods ("`GET`, `PUT`, and `DELETE
			// /api/v1/x`") or before a URL ("-X POST http://host/api/v1/x").
			named := regexp.MustCompile(`\b` + m + `\b(?:[\s,` + "`" + `]|\band\b|\bor\b|\b(?:GET|POST|PUT|PATCH|DELETE)\b)*(?:https?://[^/\s]+)?` + exact)
			found := false
			for _, line := range lines {
				if named.MatchString(line) || allMethods.MatchString(line) {
					found = true
					break
				}
			}
			if !found {
				missing = append(missing, m+" "+path)
			}
		}
	}
	sort.Strings(missing)
	for _, op := range missing {
		t.Errorf("no docs line names %s", op)
	}
}

// expandSets adds to text every expansion of the route shorthand the docs
// use for a set of paths: /flows/{deploy,undeploy}-all stands for
// /flows/deploy-all and /flows/undeploy-all. The expanded line keeps its
// other words (the methods, "all methods").
func expandSets(text string) string {
	set := regexp.MustCompile(`\{([A-Za-z-]+(?:,[A-Za-z-]+)+)\}`)
	var b strings.Builder
	b.WriteString(text)
	for _, line := range strings.Split(text, "\n") {
		for _, token := range regexp.MustCompile("`[^`]*`").FindAllString(line, -1) {
			if m := set.FindStringSubmatchIndex(token); m != nil {
				for _, alt := range strings.Split(token[m[2]:m[3]], ",") {
					b.WriteString("\n" + expandSets(strings.Replace(line, token, token[:m[0]]+alt+token[m[1]:], 1)))
				}
			}
		}
	}
	return b.String()
}

// usageFlags are the flags a usage text lists (from PrintDefaults, or a
// usage line's [-x] or [--x]).
func usageFlags(usage string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)(?:^\s+|\[)(--?[a-z][a-z-]*)`).FindAllStringSubmatch(usage, -1) {
		name := "-" + strings.TrimLeft(m[1], "-")
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
}

// TestDocsCLI: every subcommand, every flag of the client and of each
// subcommand, every shell command (each alternative of its help), and
// every flow subcommand is in cli.md.
func TestDocsCLI(t *testing.T) {
	page := docsPage(t, "cli.md")
	documented := func(word string) bool {
		return regexp.MustCompile("`" + regexp.QuoteMeta(word) + "(?:[ `]|$)").MatchString(page)
	}
	// A flag is a whole token, spelled with one dash or two (-x or --x).
	flagDocumented := func(flag string) bool {
		name := regexp.QuoteMeta(strings.TrimLeft(flag, "-"))
		return regexp.MustCompile("(?:^|[\\s`(\\[])--?" + name + "(?:[\\s`=\\])]|$)").MatchString(page)
	}
	var usage bytes.Buffer
	printUsage(&usage)
	subs := regexp.MustCompile(`(?m)weavster ([a-z]+(?: validate)?)\b`).FindAllStringSubmatch(usage.String(), -1)
	if len(subs) < 4 {
		t.Fatalf("subcommands not read from the usage:\n%s", usage.String())
	}
	for _, sub := range subs {
		if !strings.Contains(page, "weavster "+sub[1]) {
			t.Errorf("cli.md does not document weavster %s", sub[1])
		}
		var help, errb bytes.Buffer
		run(append(strings.Fields(sub[1]), "-h"), strings.NewReader(""), &help, &errb)
		for _, flag := range usageFlags(help.String() + errb.String()) {
			if flag != "-h" && !flagDocumented(flag) {
				t.Errorf("cli.md does not document weavster %s %s", sub[1], flag)
			}
		}
	}
	for _, flag := range usageFlags(usage.String()) {
		if !flagDocumented(flag) {
			t.Errorf("cli.md does not document the %s flag", flag)
		}
	}

	var shell bytes.Buffer
	printShellHelp(&shell)
	subcommand := regexp.MustCompile(`^[a-z-]+$`)
	for _, item := range strings.Split(strings.TrimPrefix(strings.TrimSpace(shell.String()), "commands: "), ", ") {
		words := strings.Fields(item)
		// An optional word before the subcommands ("snippet [library]
		// list|…") names a second form of the command.
		prefixes, rest := []string{""}, words[1:]
		if len(rest) > 1 && strings.HasPrefix(rest[0], "[") && strings.HasSuffix(rest[0], "]") {
			prefixes, rest = append(prefixes, " "+strings.Trim(rest[0], "[]")), rest[1:]
		}
		// "list [flow]|show <id>|requeue <id>": each alternative's first
		// word is a subcommand, unless one is an argument ("id|"name"|*").
		var subs []string
		if alternatives := strings.Split(strings.Join(rest, " "), "|"); len(alternatives) > 1 {
			for _, alt := range alternatives {
				if f := strings.Fields(alt); len(f) > 0 && subcommand.MatchString(f[0]) {
					subs = append(subs, f[0])
				} else {
					subs = nil
					break
				}
			}
		}
		for _, name := range strings.Split(words[0], "|") {
			if subs == nil && !documented(name) {
				t.Errorf("cli.md does not document the %s command", name)
			}
			for _, sub := range subs {
				for _, p := range prefixes {
					if !documented(name + p + " " + sub) {
						t.Errorf("cli.md does not document the %s%s %s command", name, p, sub)
					}
				}
			}
		}
	}
	for _, line := range strings.Split(flowUsage, "\n") {
		words := strings.Fields(line)
		if len(words) < 2 || words[0] != "flow" || strings.HasSuffix(words[1], ":") {
			continue
		}
		alternatives := strings.Split(words[1], "|")
		for i, sub := range alternatives {
			// "`flow deploy <id>`, and likewise `undeploy`, `start`": the
			// first one in full, the others by name.
			if !documented("flow "+sub) && (i == 0 || !documented(sub)) {
				t.Errorf("cli.md does not document flow %s", sub)
			}
		}
	}
}

// TestDocsDSL: every field of a transform is in processing-messages.md,
// and every step is a row of its steps table that names each of the step's
// fields. What a transform may hold is read from its published schema.
func TestDocsDSL(t *testing.T) {
	page := docsPage(t, "processing-messages.md")
	b, err := os.ReadFile(filepath.Join("..", "..", "agent-docs", "schemas", "transform.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	type object struct {
		Properties map[string]struct {
			Ref string `json:"$ref"`
		} `json:"properties"`
	}
	var schema struct {
		Defs map[string]object `json:"$defs"`
	}
	if err := json.Unmarshal(b, &schema); err != nil {
		t.Fatal(err)
	}
	def := func(ref string) object { return schema.Defs[strings.TrimPrefix(ref, "#/$defs/")] }
	for name := range schema.Defs["Transform"].Properties {
		if !strings.Contains(page, "`"+name+"`") {
			t.Errorf("processing-messages.md does not document the transform's %s", name)
		}
	}
	steps := schema.Defs["Step"].Properties
	if len(steps) < 5 {
		t.Fatalf("steps not read from the schema: %v", steps)
	}
	for step, ref := range steps {
		// Its row in the steps table: "| `map` | `from`, `to`, …".
		row := ""
		for _, line := range strings.Split(page, "\n") {
			if strings.HasPrefix(line, "| `"+step+"` |") {
				row = line
			}
		}
		if row == "" {
			t.Errorf("processing-messages.md has no row for the %s step", step)
		}
		for field := range def(ref.Ref).Properties {
			if !strings.Contains(row, "`"+field+"`") {
				t.Errorf("processing-messages.md does not document %s.%s in the %s row", step, field, step)
			}
		}
	}
}

// TestDocsFlowDefinition: every property of a flow, its source, and its
// destinations (flow.schema.json) is documented, in backticks, on the
// flow pages.
func TestDocsFlowDefinition(t *testing.T) {
	pages := publishedPages(t)
	flowDocs := pages["processing-messages.md"] + pages["flow-lifecycle.md"] + pages["api-formats.md"]
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
		if !strings.Contains(flowDocs, "`"+name+"`") {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	for _, n := range missing {
		t.Errorf("the flow pages do not document the flow property `%s`", n)
	}
}

// TestDocsConfigAsCode: every section of a config-as-code document is
// named in config-as-code.md.
func TestDocsConfigAsCode(t *testing.T) {
	page := docsPage(t, "config-as-code.md")
	var sections []string
	fields(reflect.TypeOf(config.Config{}), "json", "", func(path string, _ bool) {
		if !strings.Contains(path, ".") {
			sections = append(sections, path)
		}
	})
	// weavster test tells a config-as-code document from other YAML by
	// these sections, so its list must be the same.
	if !slices.Equal(sections, configSections) {
		t.Errorf("configSections is %v, but a document has the sections %v", configSections, sections)
	}
	for _, section := range sections {
		if !strings.Contains(page, "`"+section+"`") {
			t.Errorf("config-as-code.md does not document the `%s` section", section)
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
	published := []string{"openapi.yaml"}
	err = filepath.WalkDir(filepath.Join(dir, "schemas"), func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(path, ".json") {
			rel, _ := filepath.Rel(dir, path)
			published = append(published, filepath.ToSlash(rel))
		}
		return err
	})
	if err != nil || len(published) < 2 {
		t.Fatalf("schemas: %v (%v)", published, err)
	}
	for _, f := range published {
		if !strings.Contains(string(index), "]("+f+")") {
			t.Errorf("llms.txt does not list %s", f)
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
