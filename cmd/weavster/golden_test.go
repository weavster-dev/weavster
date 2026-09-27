package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/weavster-dev/weavster/internal/serverconfig"
)

// updateGolden rewrites testdata/golden/*.golden:
//
//	go test ./cmd/weavster -run TestCLIGolden -update
var updateGolden = flag.Bool("update", false, "rewrite the CLI golden files")

// goldenCase runs lines against the shared server, in batch mode (-s) or,
// with interactive, as the shell reading stdin.
type goldenCase struct {
	name        string
	lines       string
	interactive bool
	// before prepares server state the case needs (it runs first).
	before func(t *testing.T, c apiClient)
}

// sendOneMessage gives the message commands a message of flow adt to move.
func sendOneMessage(t *testing.T, c apiClient) {
	if code, body, _ := c.do(http.MethodPost, "/api/v1/flows/adt/messages", `{"k":"v"}`, basic(bootstrapAdmin, testAdminPassword)); code != http.StatusAccepted {
		t.Fatalf("send: %d %s", code, body)
	}
}

// goldenCases run in order against one server, so later cases see what
// earlier ones did. Every command in help and every flow subcommand appears
// at least once (TestCLIGolden checks).
var goldenCases = []goldenCase{
	{name: "help", lines: "help\nflow help\n"},
	{name: "version", lines: "version\n"},
	{name: "unknown-command", lines: "bogus\n"},
	{name: "unterminated-quote", lines: "flow get \"adt\n"},
	{name: "flow-create", lines: "flow create $DIR/adt.json\nflow create $DIR/none.json\nflow create\n"},
	{name: "flow-list-get", lines: "flow list\nflow get adt\nflow get \"ADT Inbound\"\nflow get nope\n"},
	{name: "flow-update", lines: "flow update adt $DIR/adt.json\nflow update-all $DIR/all.json\nflow update adt\n"},
	{name: "flow-rename", lines: "flow rename adt \"ADT In\"\nflow rename adt \"ADT Inbound\"\nflow rename nope x\n"},
	{name: "flow-enable-disable", lines: "flow disable adt\nflow enable adt\nflow enable nope\n"},
	{name: "flow-lifecycle", lines: "flow deploy adt\nflow start adt\nflow pause adt\nflow resume adt\nflow halt adt\nflow resume adt\nflow stop adt\nflow start adt\nflow pause nope\nflow stop-destination adt out\nflow start-destination adt out\nflow start-destination adt nope\n"},
	{name: "status", lines: "status\n"},
	{name: "flow-all", lines: "flow stop-all\nflow start-all\nflow pause-all\nflow resume-all\nflow halt-all\nflow resume-all\nflow redeploy-all\nflow undeploy-all\nflow deploy-all\nflow start-all\n"},
	{name: "deploy", lines: "flow undeploy adt\ndeploy\ndeploy 30\ndeploy soon\nflow start adt\n"},
	{name: "flow-connectors-ports", lines: "flow connectors\nflow ports\n"},
	{name: "flow-stats", lines: "flow stats\nflow stats adt\nflow reset-stats adt\nflow reset-stats adt lifetime\nresetstats\nresetstats lifetime\nresetstats now\n"},
	{name: "flow-export-import", lines: "flow export $DIR/flows.json adt\nflow import $DIR/flows.json\nflow import $DIR/flows.json --overwrite\nexport adt \"$DIR/one.json\"\nexport * \"$DIR/all-flows.json\"\nimport \"$DIR/one.json\"\nimport \"$DIR/one.json\" force\nimport \"$DIR/none.json\"\n"},
	{name: "messages", before: sendOneMessage, lines: "exportmessages \"$DIR/msgs.gz\" adt\nexportmessages \"$DIR/all.gz\" *\nimportmessages \"$DIR/msgs.gz\" adt\nexportmessages \"$DIR/x.gz\"\nimportmessages \"$DIR/none.gz\" adt\n"},
	{name: "users", lines: "user add ops Ops-Passw0rd flows:view\nuser list\nuser changepw ops New-Passw0rd-1\nuser add ops Ops-Passw0rd\nuser remove ops\nuser remove ops\nuser\n"},
	{name: "config-items", lines: "importmap \"$DIR/map.json\"\nexportmap \"$DIR/map-out.json\"\nimportscripts \"$DIR/scripts.json\"\nexportscripts \"$DIR/scripts-out.json\"\nimportmap \"$DIR/none.json\"\nimportmap\n"},
	{name: "snippets", lines: "snippet library import \"$DIR/libs.json\"\nsnippet import \"$DIR/snippets.json\"\nsnippet list\nsnippet library list\nsnippet export \"$DIR/snippets-out.json\"\nsnippet library export \"$DIR/libs-out.json\"\nsnippet library remove hl7\nsnippet remove pid\nsnippet library remove hl7\nsnippet remove pid\nsnippet list extra\n"},
	{name: "alerts", lines: "importalert \"$DIR/alerts.json\"\nimportalert \"$DIR/alerts.json\"\nimportalert \"$DIR/alerts.json\" force\nexportalert errors \"$DIR/alert.json\"\nexportalert * \"$DIR/alerts-out.json\"\nexportalert nope \"$DIR/x.json\"\nimportalert\n"},
	{name: "config-validate", lines: "config validate \"$DIR/config.yaml\"\nconfig validate \"$DIR/map.json\"\nconfig validate\n"},
	{name: "config-plan", lines: "config diff \"$DIR/config.yaml\"\nconfig diff \"$DIR/map.json\"\nconfig plan \"$DIR/map.json\"\nconfig apply \"$DIR/config.yaml\" --dry-run\n"},
	{name: "config-transfer", lines: "exportcfg \"$DIR/cfg.json\"\nexportcfg \"$DIR/cfg-map.json\" overwriteconfigmap\nimportcfg \"$DIR/cfg.json\"\nimportcfg \"$DIR/cfg.json\" force nodeploy\nimportcfg \"$DIR/cfg-map.json\" overwriteconfigmap force\nimportcfg \"$DIR/none.json\"\nexportcfg\n"},
	{name: "dump-clear", lines: "dump stats \"$DIR/stats.json\"\ndump events \"$DIR/events.json\"\ndump logs \"$DIR/x\"\nclearallmessages\nclearallmessages now\n"},
	{name: "deprecated", lines: "channel list\ncodetemplate list\n"},
	{name: "flow-remove", lines: "flow remove adt\nflow remove adt\nflow create $DIR/adt.json\n"},
	{name: "quit-in-batch", lines: "quit\nexit\nstatus\n"},
	{name: "interactive", interactive: true, lines: "status\nbogus\n\nflow get nope\nquit\nstatus\n"},
	{name: "interactive-eof", interactive: true, lines: "version\n"},
}

// goldenFiles are the inputs the cases read, written to $DIR.
func goldenFiles(dir string) map[string]string {
	return map[string]string{
		"adt.json":      `{"id":"adt","name":"ADT Inbound","enabled":true,"destinations":[{"name":"out","type":"file","dir":"` + filepath.Join(dir, "out") + `"}]}`,
		"all.json":      `{"flows":[{"id":"adt","name":"ADT Inbound","destinations":[{"name":"out","type":"file","dir":"` + filepath.Join(dir, "out") + `"}]}]}`,
		"map.json":      `{"region":"eu"}`,
		"config.yaml":   "flows:\n  adt: {name: ADT Inbound}\nconfigmap:\n  region: eu\n",
		"scripts.json":  `{"deploy":"log('deployed')"}`,
		"libs.json":     `[{"name":"hl7","description":"HL7 helpers"}]`,
		"snippets.json": `[{"name":"pid","library":"hl7","code":"get('PID.3')"}]`,
		"alerts.json":   `[{"id":"errors","name":"ADT errors","enabled":true,"trigger":{"events":["message.errored"],"flows":["adt"]},"actions":[{"type":"email","to":["ops@example.com"]}]}]`,
	}
}

// normalize replaces what changes between runs: the temporary directory
// and the server address.
func normalize(s, dir, base string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, dir, "$DIR"), base, "$SERVER")
}

// withNewline ends non-empty output with a newline so the section markers
// start on their own line, and says so when the output itself had none
// (the shell's last prompt), so a dropped newline still shows as a diff.
func withNewline(s string) string {
	if s != "" && !strings.HasSuffix(s, "\n") {
		return s + "\n(no newline at end)\n"
	}
	return s
}

// TestCLIGolden runs every golden case and compares its stdout, stderr, and
// exit code with testdata/golden/<case>.golden.
func TestCLIGolden(t *testing.T) {
	c := startComposed(t, serverconfig.Default(), io.Discard)
	dir := t.TempDir()
	for name, body := range goldenFiles(dir) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	script := filepath.Join(dir, "script.txt")
	// The cases share one server and run in order: each builds on the state
	// the earlier ones left (a flow created first is deployed later), so a
	// failure early on can show up in later cases too.
	for _, gc := range goldenCases {
		t.Run(gc.name, func(t *testing.T) { runGoldenCase(t, gc, c, dir, script) })
	}
	checkGoldenCoverage(t)
}

// runGoldenCase runs one case and compares (or, with -update, writes) its
// golden file.
func runGoldenCase(t *testing.T, gc goldenCase, c apiClient, dir, script string) {
	if gc.before != nil {
		gc.before(t, c)
	}
	{
		lines := strings.ReplaceAll(gc.lines, "$DIR", dir)
		args := []string{"-a", c.base, "-u", bootstrapAdmin, "-p", testAdminPassword}
		var stdin io.Reader = strings.NewReader(lines)
		if !gc.interactive {
			if err := os.WriteFile(script, []byte(lines), 0o600); err != nil {
				t.Fatal(err)
			}
			args, stdin = append(args, "-s", script), strings.NewReader("")
		}
		var out, errb bytes.Buffer
		code := run(args, stdin, &out, &errb)
		got := fmt.Sprintf("# %s\n%s--- stdout\n%s--- stderr\n%s--- exit %d\n", gc.name, gc.lines,
			withNewline(normalize(out.String(), dir, c.base)), withNewline(normalize(errb.String(), dir, c.base)), code)
		path := filepath.Join("testdata", "golden", gc.name+".golden")
		if *updateGolden {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
				t.Fatal(err)
			}
			return
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%v (run with -update to create it)", err)
		}
		if got != string(want) {
			t.Errorf("output differs from %s (run with -update to accept)\n--- got\n%s--- want\n%s", path, got, want)
		}
	}
}

// goldenSubcommands matches a help token listing subcommands (list|add).
var goldenSubcommands = regexp.MustCompile(`^[a-z-]+(\|[a-z-]+)+$`)

// checkGoldenCoverage fails when a command or subcommand listed by help, or
// a flow subcommand, has no golden case line that runs.
func checkGoldenCoverage(t *testing.T) {
	t.Helper()
	used := map[string]bool{}
	for _, gc := range goldenCases {
		for _, line := range strings.Split(gc.lines, "\n") {
			words := strings.Fields(line)
			if len(words) == 0 || strings.HasPrefix(words[0], "#") {
				continue // blank lines and comments run nothing
			}
			if gc.interactive && (words[0] == "quit" || words[0] == "exit") {
				used[words[0]] = true
				break // the shell stops here; later lines never run
			}
			for n := 1; n <= len(words) && n <= 3; n++ {
				used[strings.Join(words[:n], " ")] = true
			}
		}
	}
	require := func(cmd string) {
		if !used[cmd] {
			t.Errorf("%q has no golden case", cmd)
		}
	}
	var help bytes.Buffer
	printShellHelp(&help)
	commands := strings.TrimPrefix(strings.TrimSpace(help.String()), "commands: ")
	for _, entry := range strings.Split(commands, ", ") {
		words := strings.Fields(entry)
		if len(words) == 0 {
			continue
		}
		// An optional word before the subcommands ("snippet [library] list|…")
		// names a second form of the command.
		prefixes := []string{""}
		rest := words[1:]
		if len(rest) > 0 && strings.HasPrefix(rest[0], "[") && strings.HasSuffix(rest[0], "]") && len(rest) > 1 && goldenSubcommands.MatchString(rest[1]) {
			prefixes = append(prefixes, " "+strings.Trim(rest[0], "[]"))
			rest = rest[1:]
		}
		for _, cmd := range strings.Split(words[0], "|") {
			require(cmd)
			if len(rest) == 0 || !goldenSubcommands.MatchString(rest[0]) {
				continue
			}
			// "list|import "path"|export "path"|remove <name>": each
			// alternative's first word is a subcommand.
			for _, alt := range strings.Split(strings.Join(rest, " "), "|") {
				sub := strings.Fields(alt)[0]
				for _, p := range prefixes {
					require(cmd + p + " " + sub)
				}
			}
		}
	}
	for _, line := range strings.Split(flowUsage, "\n") {
		words := strings.Fields(line)
		if len(words) < 2 || words[0] != "flow" || strings.HasSuffix(words[1], ":") {
			continue
		}
		for _, sub := range strings.Split(words[1], "|") {
			require("flow " + sub)
		}
	}
}
