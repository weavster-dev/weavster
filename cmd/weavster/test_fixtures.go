package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
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
	Output       any                            `yaml:"output"`
	OutputText   *string                        `yaml:"outputText"`
	Excluded     []string                       `yaml:"excluded"`
	Destinations map[string]destinationExpected `yaml:"destinations"`
}

// destinationExpected is a destination transform's expected result.
type destinationExpected struct {
	Status     string  `yaml:"status"`
	Error      string  `yaml:"error"`
	Output     any     `yaml:"output"`
	OutputText *string `yaml:"outputText"`
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
}

// discover walks the paths (files or directories; hidden directories are
// skipped) for fixture files and config-as-code documents (version "1").
func discover(paths []string) (discovered, error) {
	d := discovered{flows: map[string]gateway.Flow{}, defined: map[string][]string{}}
	for _, root := range paths {
		if _, err := os.Stat(root); err != nil {
			return d, err
		}
		err := filepath.WalkDir(root, func(path string, e fs.DirEntry, err error) error {
			switch {
			case err != nil:
				return err
			case e.IsDir():
				if path != root && strings.HasPrefix(e.Name(), ".") {
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
	var head struct {
		Version any `yaml:"version"`
	}
	if yaml.Unmarshal(data, &head) != nil || fmt.Sprint(head.Version) != "1" {
		return // not a config-as-code document
	}
	cfg, err := config.Parse(data)
	if err != nil {
		d.errs = append(d.errs, testResult{Name: filepath.ToSlash(path), Failure: err.Error()})
		return
	}
	for id, f := range cfg.Flows {
		if f.ID == "" {
			f.ID = id
		}
		d.flows[id] = f
		d.defined[id] = append(d.defined[id], path)
	}
}

// runFixtures runs every case of every fixture file.
func (d discovered) runFixtures() []testResult {
	results := append([]testResult(nil), d.errs...)
	for _, path := range d.fixtures {
		results = append(results, d.runFixture(path)...)
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

func (d discovered) runFixture(path string) []testResult {
	name := fixtureName(path)
	fail := func(format string, a ...any) []testResult {
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
		path := c.InputFile
		if !filepath.IsAbs(path) {
			path = filepath.Join(dir, path)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("inputFile: %w", err)
		}
		if len(b) > maxFixtureInput {
			return fmt.Errorf("inputFile %s is larger than 10 MiB", c.InputFile)
		}
		input = b
	}
	got := pipeline.Run(f, input)
	e := c.Expect
	if err := checkResult("", e.Status, e.Error, e.Output, e.OutputText, got.Status, got.Error, got.Output); err != nil {
		return err
	}
	if e.Excluded != nil && !slices.Equal(sorted(e.Excluded), got.Excluded) && (len(e.Excluded) != 0 || len(got.Excluded) != 0) {
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
		if err := checkResult("destination "+n+": ", de.Status, de.Error, de.Output, de.OutputText, dr.Status, dr.Error, dr.Output); err != nil {
			return err
		}
	}
	return nil
}

func sorted(s []string) []string {
	out := slices.Clone(s)
	sort.Strings(out)
	return out
}

// checkResult compares a status, error, and output with the expected ones.
func checkResult(prefix, wantStatus, wantErr string, wantOutput any, wantText *string, status, errText string, output []byte) error {
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
	if wantText != nil && string(output) != *wantText {
		return fmt.Errorf("%soutput is %q, want %q", prefix, output, *wantText)
	}
	if wantOutput != nil {
		var got, want any
		if err := json.Unmarshal(output, &got); err != nil {
			return fmt.Errorf("%soutput is not JSON (%q); compare it with outputText", prefix, output)
		}
		b, err := json.Marshal(wantOutput)
		if err != nil {
			return fmt.Errorf("%sexpected output: %w", prefix, err)
		}
		_ = json.Unmarshal(b, &want)
		if where, ok := includes(got, want, ""); !ok {
			return fmt.Errorf("%soutput differs at %s: it is %s, want the fields %s", prefix, where, output, b)
		}
	}
	return nil
}

// includes reports whether got has what want lists: an object the keys
// want gives (recursively; others are ignored), anything else equal. On a
// mismatch it names where.
func includes(got, want any, at string) (string, bool) {
	wm, ok := want.(map[string]any)
	if !ok {
		if reflect.DeepEqual(got, want) {
			return "", true
		}
		if at == "" {
			at = "the top"
		}
		return at, false
	}
	gm, ok := got.(map[string]any)
	if !ok {
		return strings.TrimPrefix(at, ".") + " (not an object)", false
	}
	keys := make([]string, 0, len(wm))
	for k := range wm {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if where, ok := includes(gm[k], wm[k], at+"."+k); !ok {
			return strings.TrimPrefix(where, "."), false
		}
	}
	return "", true
}
