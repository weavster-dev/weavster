package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/weavster-dev/weavster/internal/config"
	"github.com/weavster-dev/weavster/internal/gateway"
	"github.com/weavster-dev/weavster/internal/pipeline"
)

// fixtureSuffixes name fixture files (#107 D-101).
var fixtureSuffixes = []string{".test.yaml", ".test.yml", ".test.json"}

// fixtureFile is a fixture file: cases for one flow of the config-as-code
// documents under the same paths.
type fixtureFile struct {
	Flow  string        `yaml:"flow"`
	Cases []fixtureCase `yaml:"cases"`
}

// fixtureCase is one message and what the flow must make of it.
type fixtureCase struct {
	Name      string      `yaml:"name"`
	Input     *string     `yaml:"input"`
	InputFile string      `yaml:"inputFile"`
	Expect    expectation `yaml:"expect"`
}

// expectation is a case's expected result; left-out fields are not
// checked, except status (transformed by default).
type expectation struct {
	Status       string                         `yaml:"status"`
	Error        string                         `yaml:"error"`
	Output       jsonValue                      `yaml:"output"`
	OutputText   *string                        `yaml:"outputText"`
	OutputFile   string                         `yaml:"outputFile"`
	Excluded     []string                       `yaml:"excluded"`
	Destinations map[string]destinationExpected `yaml:"destinations"`
}

// destinationExpected is a destination transform's expected result.
type destinationExpected struct {
	Status     string    `yaml:"status"`
	Error      string    `yaml:"error"`
	Output     jsonValue `yaml:"output"`
	OutputText *string   `yaml:"outputText"`
	OutputFile string    `yaml:"outputFile"`
}

// jsonValue is an expected output: set when the fixture gives one, null
// included.
type jsonValue struct {
	set bool
	v   any
}

func (j *jsonValue) UnmarshalYAML(n *yaml.Node) error {
	j.set = true
	return n.Decode(&j.v)
}

// maxFixtureInput bounds an inputFile.
const maxFixtureInput = 10 << 20

// discovered is what the paths hold: fixture files, and flows by id with
// the documents that define them.
type discovered struct {
	fixtures []string
	flows    map[string]gateway.Flow
	defined  map[string][]string // flow id -> documents
	errs     []testResult        // documents that could not be read
	seen     map[string]bool     // files already read (paths may overlap)
}

// discover walks the paths (files or directories) for fixture files and
// config-as-code documents. Hidden directories, node_modules, and
// directories that cannot be read are skipped; a file under two of the
// paths is read once.
func discover(paths []string) (discovered, error) {
	d := discovered{flows: map[string]gateway.Flow{}, defined: map[string][]string{}, seen: map[string]bool{}}
	for _, root := range paths {
		if _, err := os.Stat(root); err != nil {
			return d, err
		}
		err := filepath.WalkDir(root, func(path string, e fs.DirEntry, err error) error {
			if abs, aerr := filepath.Abs(path); aerr == nil && e != nil && !e.IsDir() {
				if d.seen[abs] {
					return nil
				}
				d.seen[abs] = true
			}
			switch {
			case err != nil && path != root && errors.Is(err, fs.ErrPermission):
				return filepath.SkipDir
			case err != nil:
				return err
			case e.IsDir():
				if path != root && (strings.HasPrefix(e.Name(), ".") || e.Name() == "node_modules") {
					return filepath.SkipDir
				}
				return nil
			case slices.ContainsFunc(fixtureSuffixes, func(s string) bool { return strings.HasSuffix(path, s) }):
				d.fixtures = append(d.fixtures, path)
			case slices.Contains([]string{".yaml", ".yml", ".json"}, filepath.Ext(path)):
				d.document(path)
			}
			return nil
		})
		if err != nil {
			return d, err
		}
	}
	sort.Strings(d.fixtures)
	return d, nil
}

// document reads the flows of a config-as-code document; other YAML and
// JSON files are left alone.
func (d *discovered) document(path string) {
	data, err := readDocument(path)
	if err != nil {
		d.errs = append(d.errs, testResult{Name: filepath.ToSlash(path), Failure: err.Error()})
		return
	}
	if !configDocument(data) {
		return
	}
	cfg, err := config.Parse(data)
	if err != nil {
		d.errs = append(d.errs, testResult{Name: filepath.ToSlash(path), Failure: err.Error()})
		return
	}
	for id, f := range cfg.Flows {
		d.flows[id] = f
		d.defined[id] = append(d.defined[id], path)
	}
}

// configSections are the top-level keys of a config-as-code document.
var configSections = []string{"version", "flows", "alerts", "snippets", "snippetLibraries", "scripts", "configmap", "settings"}

// configDocument reports whether data is meant as a config-as-code
// document: a mapping whose version is exactly 1 (then any mistake in it
// is reported), or, without a version, whose keys are all its sections.
// Other YAML and JSON (a compose file with version 1.0, a CI workflow, a
// package.json) is left alone.
func configDocument(data []byte) bool {
	var root yaml.Node
	if yaml.Unmarshal(data, &root) != nil || len(root.Content) != 1 || root.Content[0].Kind != yaml.MappingNode {
		return false
	}
	m := root.Content[0]
	if len(m.Content) == 0 {
		return false
	}
	sections := true
	for i := 0; i < len(m.Content); i += 2 {
		k, v := m.Content[i].Value, m.Content[i+1]
		if k == "version" {
			return v.Value == "1" && (v.Tag == "!!str" || v.Tag == "!!int")
		}
		sections = sections && slices.Contains(configSections, k)
	}
	return sections
}

// runFixtures runs the cases of every fixture file whose name contains
// filter (every case when it is empty). A config-as-code document that
// could not be read fails the run whatever the filter.
func (d discovered) runFixtures(filter string) []testResult {
	results := append([]testResult(nil), d.errs...)
	for _, path := range d.fixtures {
		results = append(results, d.runFixture(path, filter)...)
	}
	return results
}

// fixtureName is a fixture file's name in results: its path without the
// suffix, with forward slashes.
func fixtureName(path string) string {
	for _, s := range fixtureSuffixes {
		path = strings.TrimSuffix(path, s)
	}
	return filepath.ToSlash(path)
}

func (d discovered) runFixture(path, filter string) []testResult {
	name := fixtureName(path)
	fail := func(format string, a ...any) []testResult {
		if !strings.Contains(name, filter) {
			return nil // a broken fixture shows only when its name matches
		}
		return []testResult{{Name: name, Failure: fmt.Sprintf(format, a...)}}
	}
	data, err := readDocument(path)
	if err != nil {
		return fail("%v", err)
	}
	var file fixtureFile
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&file); err != nil {
		return fail("%s: %v", path, err)
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return fail("%s: a fixture file holds exactly one YAML document", path)
	}
	switch defs := d.defined[file.Flow]; {
	case file.Flow == "":
		return fail("%s: flow is required", path)
	case len(defs) == 0:
		return fail("%s: no config-as-code document under the given paths defines flow %s", path, file.Flow)
	case len(defs) > 1:
		return fail("%s: flow %s is defined in more than one document: %s", path, file.Flow, strings.Join(defs, ", "))
	case len(file.Cases) == 0:
		return fail("%s: no cases", path)
	}
	flow, err := toPipelineFlow(d.flows[file.Flow])
	if err != nil {
		return fail("flow %s: %v", file.Flow, err)
	}
	var results []testResult
	seen := map[string]bool{}
	for i, c := range file.Cases {
		r := testResult{Name: name + "/" + c.Name}
		if !strings.Contains(r.Name, filter) {
			continue // not run at all
		}
		switch {
		case c.Name == "" || seen[c.Name]:
			r.Name, r.Failure = fmt.Sprintf("%s/%d", name, i+1), "every case needs a name of its own"
		default:
			seen[c.Name] = true
			if err := runCase(flow, filepath.Dir(path), c); err != nil {
				r.Failure = err.Error()
			} else {
				r.Passed = true
			}
		}
		results = append(results, r)
	}
	return results
}

// runCase runs one case through the flow and checks the expectations.
func runCase(f pipeline.Flow, dir string, c fixtureCase) error {
	var input []byte
	switch {
	case (c.Input == nil) == (c.InputFile == ""):
		return errors.New("give input or inputFile (one of them)")
	case c.Input != nil:
		input = []byte(*c.Input)
	default:
		b, err := readFixtureFile(dir, c.InputFile)
		if err != nil {
			return fmt.Errorf("inputFile: %w", err)
		}
		input = b
	}
	got := pipeline.Run(f, input)
	e := c.Expect
	if err := checkResult(dir, "", e.Status, e.Error, e.Output, e.OutputText, e.OutputFile, got.Status, got.Error, got.Output); err != nil {
		return err
	}
	if e.Excluded != nil && !slices.Equal(sorted(e.Excluded), got.Excluded) {
		return fmt.Errorf("excluded destinations are %v, want %v", got.Excluded, sorted(e.Excluded))
	}
	names := make([]string, 0, len(e.Destinations))
	for n := range e.Destinations {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		de := e.Destinations[n]
		dr, ok := got.Destinations[n]
		if !ok {
			return fmt.Errorf("destination %s: no transform result (it has no transform of its own, is excluded, or does not exist)", n)
		}
		if err := checkResult(dir, "destination "+n+": ", de.Status, de.Error, de.Output, de.OutputText, de.OutputFile, dr.Status, dr.Error, dr.Output); err != nil {
			return err
		}
	}
	return nil
}

// readFixtureFile reads a file a fixture names, relative to the fixture's
// directory, at most maxFixtureInput bytes.
func readFixtureFile(dir, name string) ([]byte, error) {
	path := name
	if !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	b, err := io.ReadAll(io.LimitReader(file, maxFixtureInput+1))
	if err == nil && len(b) > maxFixtureInput {
		err = fmt.Errorf("%s is larger than 10 MiB", name)
	}
	return b, err
}

// outputText is the exact output a case expects: outputText, or the
// contents of outputFile (not both).
func outputText(dir string, text *string, file string) (*string, error) {
	switch {
	case file == "":
		return text, nil
	case text != nil:
		return nil, errors.New("give outputText or outputFile, not both")
	}
	b, err := readFixtureFile(dir, file)
	if err != nil {
		return nil, fmt.Errorf("outputFile: %w", err)
	}
	s := string(b)
	return &s, nil
}

func sorted(s []string) []string {
	out := slices.Clone(s)
	sort.Strings(out)
	return out
}

// checkResult compares a status, error, and output with the expected ones;
// an outputFile (in dir) is read only once the status is as expected.
func checkResult(dir, prefix, wantStatus, wantErr string, wantOutput jsonValue, wantText *string, wantFile string, status, errText string, output []byte) error {
	if wantStatus == "" {
		wantStatus = "transformed"
	}
	if status != wantStatus {
		if errText != "" {
			return fmt.Errorf("%sstatus is %s (%s), want %s", prefix, status, errText, wantStatus)
		}
		return fmt.Errorf("%sstatus is %s, want %s", prefix, status, wantStatus)
	}
	if wantErr != "" && !strings.Contains(errText, wantErr) {
		return fmt.Errorf("%serror is %q, want it to contain %q", prefix, errText, wantErr)
	}
	wantText, err := outputText(dir, wantText, wantFile)
	if err != nil {
		return fmt.Errorf("%s%w", prefix, err)
	}
	if wantText != nil && string(output) != *wantText {
		return fmt.Errorf("%soutput is %q, want %q", prefix, output, *wantText)
	}
	if wantOutput.set {
		got, err := exactJSON(output)
		if err != nil {
			return fmt.Errorf("%soutput is not JSON (%q); compare it with outputText", prefix, output)
		}
		b, err := json.Marshal(wantOutput.v)
		if err != nil {
			return fmt.Errorf("%sexpected output: %w", prefix, err)
		}
		want, _ := exactJSON(b)
		if where, ok := includes(got, want, ""); !ok {
			return fmt.Errorf("%soutput differs at %s: it is %s, want the fields %s", prefix, where, output, b)
		}
	}
	return nil
}

// exactJSON decodes JSON keeping numbers exact (json.Number), as the
// pipeline does.
func exactJSON(b []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("more than one JSON value")
	}
	return v, nil
}

// jsonEqual compares JSON values; numbers by exact value (1 and 1.0 are
// equal, 9007199254740992 and 9007199254740993 are not).
func jsonEqual(a, b any) bool {
	switch av := a.(type) {
	case json.Number:
		bv, ok := b.(json.Number)
		if !ok {
			return false
		}
		x, okx := new(big.Rat).SetString(string(av))
		y, oky := new(big.Rat).SetString(string(bv))
		return okx && oky && x.Cmp(y) == 0
	case []any:
		bv, ok := b.([]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !jsonEqual(av[i], bv[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for k, v := range av {
			w, present := bv[k]
			if !present || !jsonEqual(v, w) {
				return false
			}
		}
		return true
	}
	return a == b // strings, booleans, null
}

// includes reports whether got has what want lists: an object the keys
// want gives (recursively; others are ignored), anything else equal. On a
// mismatch it names where.
func includes(got, want any, at string) (string, bool) {
	wm, ok := want.(map[string]any)
	if !ok {
		if jsonEqual(got, want) {
			return "", true
		}
		if at == "" {
			at = "the top"
		}
		return at, false
	}
	gm, ok := got.(map[string]any)
	if !ok {
		if at == "" {
			return "the top (not an object)", false
		}
		return strings.TrimPrefix(at, ".") + " (not an object)", false
	}
	keys := make([]string, 0, len(wm))
	for k := range wm {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		g, present := gm[k]
		if !present { // an expected null too needs the field
			return strings.TrimPrefix(at+"."+k, ".") + " (missing)", false
		}
		if where, ok := includes(g, wm[k], at+"."+k); !ok {
			return strings.TrimPrefix(where, "."), false
		}
	}
	return "", true
}
