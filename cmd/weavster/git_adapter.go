package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"

	"github.com/weavster-dev/weavster/internal/config"
	"github.com/weavster-dev/weavster/internal/gateway"
	"github.com/weavster-dev/weavster/internal/gitstore"
	"github.com/weavster-dev/weavster/internal/serverconfig"
)

// gitAdapter serves gateway.GitRepository from one on-disk repository.
type gitAdapter struct {
	mu     *sync.Mutex // go-git repositories are not safe for concurrent use
	store  *gitstore.Store
	remote serverconfig.GitRemote
}

func newGitAdapter(cfg serverconfig.Git) (gitAdapter, error) {
	s, err := gitstore.OpenOrInit(cfg.Path)
	if err != nil {
		return gitAdapter{}, fmt.Errorf("git: %s: %w", cfg.Path, err)
	}
	return gitAdapter{mu: &sync.Mutex{}, store: s, remote: cfg.Remote}, nil
}

// remoteOf returns the configured remote with its password read from the
// environment now, so a rotated token is picked up without a restart.
func (a gitAdapter) remoteOf() (gitstore.Remote, error) {
	if a.remote.URL == "" {
		return gitstore.Remote{}, fmt.Errorf("%w: no remote configured (server config git.remote.url)", gateway.ErrGitConflict)
	}
	r := gitstore.Remote{URL: a.remote.URL, Username: a.remote.Username}
	if a.remote.PasswordEnv != "" {
		r.Password = os.Getenv(a.remote.PasswordEnv)
	}
	return r, nil
}

// remoteError maps a remote operation's error to the gateway's, never
// showing the password.
func remoteError(r gitstore.Remote, branch string, err error) error {
	switch {
	case errors.Is(err, gitstore.ErrRejected):
		return fmt.Errorf("%w: %w; pull first", gateway.ErrGitConflict, err)
	case errors.Is(err, gitstore.ErrNothingToPush):
		return fmt.Errorf("%w: %w", gateway.ErrGitConflict, err)
	case errors.Is(err, gitstore.ErrNotFound):
		return fmt.Errorf("%w: the remote has no branch %s; push first", gateway.ErrGitConflict, branch)
	}
	msg := err.Error()
	if r.Password != "" {
		msg = strings.ReplaceAll(msg, r.Password, "***")
	}
	return fmt.Errorf("%w: %s: %s", gateway.ErrGitRemote, r.URL, msg)
}

func remoteStatus(r gitstore.Remote, st gitstore.RemoteState) gateway.GitRemoteStatus {
	return gateway.GitRemoteStatus{URL: r.URL, Branch: st.Branch, Head: st.Head, RemoteHead: st.RemoteHead, Ahead: len(st.Ahead), Behind: st.Behind}
}

func (a gitAdapter) GitRemote(context.Context) (gateway.GitRemoteStatus, error) {
	r, err := a.remoteOf()
	if err != nil {
		return gateway.GitRemoteStatus{}, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	st, err := a.store.Status(r)
	if err != nil {
		return gateway.GitRemoteStatus{}, remoteError(r, st.Branch, err)
	}
	return remoteStatus(r, st), nil
}

func (a gitAdapter) GitPush(context.Context) (gateway.GitRemoteStatus, error) {
	r, err := a.remoteOf()
	if err != nil {
		return gateway.GitRemoteStatus{}, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.store.PushTo(r); err != nil {
		return gateway.GitRemoteStatus{}, remoteError(r, "", err)
	}
	st, err := a.store.Status(r)
	if err != nil {
		return gateway.GitRemoteStatus{}, remoteError(r, st.Branch, err)
	}
	return remoteStatus(r, st), nil
}

func (a gitAdapter) GitPull(context.Context) (gateway.GitPullResult, error) {
	r, err := a.remoteOf()
	if err != nil {
		return gateway.GitPullResult{}, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	dropped, err := a.store.PullRemoteWins(r)
	if err != nil {
		_, branch, _ := a.store.Head()
		return gateway.GitPullResult{}, remoteError(r, branch, err)
	}
	head, _, err := a.store.Head()
	return gateway.GitPullResult{Head: head, Dropped: dropped}, err
}

func (a gitAdapter) GitInfo(context.Context) (gateway.GitInfo, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	head, branch, err := a.store.Head()
	return gateway.GitInfo{Branch: branch, Head: head}, err
}

// GitCommit makes the repository's configuration files match live and
// commits them when anything changed. Only files directly in a section
// directory (flows/x.yaml) are managed; other files are left alone. On
// failure the files it touched are put back and the index is reset.
func (a gitAdapter) GitCommit(_ context.Context, live gateway.ConfigBundle, message, author string) (res gateway.GitCommitResult, err error) {
	cfg, err := liveConfigOf(live)
	if err != nil {
		return gateway.GitCommitResult{}, err
	}
	files, err := configFiles(cfg)
	if err != nil {
		return gateway.GitCommitResult{}, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	existing, err := a.store.Files()
	if err != nil {
		return gateway.GitCommitResult{}, err
	}
	backup := map[string][]byte{} // file -> content before (nil: did not exist)
	defer func() {
		if err != nil {
			a.rollback(backup)
		}
	}()
	// save records f's content before it changes; a file that cannot be
	// read aborts the commit, since it could not be put back.
	save := func(f string) error {
		if _, ok := backup[f]; ok {
			return nil
		}
		b, readErr := a.store.ReadFile(f)
		if readErr != nil && !errors.Is(readErr, fs.ErrNotExist) {
			return fmt.Errorf("git: back up %s: %w", f, readErr)
		}
		backup[f] = b // nil when it does not exist
		return nil
	}
	for _, f := range existing {
		if _, keep := files[f]; !keep && managedFile(f) {
			if err = save(f); err != nil {
				return gateway.GitCommitResult{}, err
			}
			if err = a.store.RemoveFile(f); err != nil {
				return gateway.GitCommitResult{}, err
			}
		}
	}
	for f, content := range files {
		if err = save(f); err != nil {
			return gateway.GitCommitResult{}, err
		}
		if err = a.store.WriteFile(f, content); err != nil {
			return gateway.GitCommitResult{}, err
		}
	}
	changed, err := a.store.WorkingTreeDiff()
	if err != nil {
		return gateway.GitCommitResult{}, err
	}
	res = gateway.GitCommitResult{Changed: changed}
	if len(changed) == 0 {
		res.Head, _, err = a.store.Head()
		return res, err
	}
	res.Committed = true
	res.Head, err = a.store.Commit(message, gitstore.Author{Name: author})
	return res, err
}

// rollback puts back the files a failed commit touched and resets the
// index; a failure here leaves the next commit to reconcile the files.
func (a gitAdapter) rollback(backup map[string][]byte) {
	for f, b := range backup {
		if b == nil {
			_ = a.store.RemoveFile(f)
		} else {
			_ = a.store.WriteFile(f, b)
		}
	}
	_ = a.store.Unstage()
}

// managedFile reports whether f is a configuration file commit manages: a
// .yaml file directly in a section directory.
func managedFile(f string) bool {
	dir, name, ok := strings.Cut(f, "/")
	if !ok || strings.Contains(name, "/") || !strings.HasSuffix(name, ".yaml") || dir == "configmap" {
		return false
	}
	_, isSection := config.KindOf(dir)
	return isSection
}

func (a gitAdapter) GitLog(_ context.Context, path string, limit int) ([]gateway.GitRevision, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	revs, err := a.store.Revisions(path, limit)
	if err != nil {
		return nil, err
	}
	out := make([]gateway.GitRevision, 0, len(revs))
	for _, r := range revs {
		out = append(out, gateway.GitRevision{Hash: r.Hash, Message: strings.TrimRight(r.Message, "\n"), Author: r.Author, At: r.When.UTC()})
	}
	return out, nil
}

func (a gitAdapter) GitContent(_ context.Context, path, rev string) ([]byte, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	b, err := a.store.ContentAtRevision(path, rev)
	if errors.Is(err, gitstore.ErrNotFound) {
		return nil, gateway.ErrGitNotFound
	}
	return b, err
}

// configFiles lays cfg out as repository files: one complete config
// document per artifact at <section>/<escaped name>.yaml (#107 D-51). The
// config map is left out. Two names differing only in case are refused:
// they would share a file on case-insensitive filesystems.
func configFiles(cfg *config.Config) (map[string][]byte, error) {
	out := map[string][]byte{}
	folded := map[string]string{} // lower-case file -> artifact key
	artifacts := cfg.Artifacts()
	keys := make([]string, 0, len(artifacts))
	for k := range artifacts {
		keys = append(keys, k)
	}
	sort.Strings(keys) // a clash names the same two artifacts every time
	for _, key := range keys {
		content := artifacts[key]
		kind, name, _ := strings.Cut(key, "/")
		section, ok := config.SectionOf(kind)
		if !ok || kind == "configmap" {
			continue
		}
		file := section + "/" + url.PathEscape(name) + ".yaml"
		if other, clash := folded[strings.ToLower(file)]; clash {
			return nil, fmt.Errorf("%w: %s and %s", gateway.ErrGitNameClash, other, key)
		}
		folded[strings.ToLower(file)] = key
		value := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: string(content)}
		if kind != "script" {
			// JSON is YAML: decoding into a node keeps numbers exact and
			// keys in order.
			var doc yaml.Node
			if err := yaml.Unmarshal(content, &doc); err != nil {
				return nil, fmt.Errorf("%s: %w", key, err)
			}
			value = doc.Content[0]
			blockStyle(value)
		}
		str := func(v string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v} }
		root := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{
			str("version"), str("1"),
			str(section), {Kind: yaml.MappingNode, Content: []*yaml.Node{str(name), value}},
		}}
		b, err := yaml.Marshal(root)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		out[file] = b
	}
	return out, nil
}

// blockStyle drops the JSON (flow, quoted) styles so the file reads as
// ordinary YAML; strings that would read as another type stay quoted.
func blockStyle(n *yaml.Node) {
	n.Style = 0
	for _, c := range n.Content {
		blockStyle(c)
	}
}

// repoSections are the document sections the repository holds, in document
// order; the config map is never read from it (#107 D-52).
var repoSections = []string{"flows", "alerts", "snippets", "snippetLibraries", "scripts", "settings"}

// GitDocument merges every managed file at rev into one config document
// and returns it with the commit it was read from. Each file must be a
// valid config document without YAML anchors or aliases (they would not
// survive the merge); an artifact defined in two files, or a configmap
// section, makes the document invalid.
func (a gitAdapter) GitDocument(_ context.Context, rev string) ([]byte, string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	commit, contents, err := a.store.ReadAt(rev, managedFile)
	if errors.Is(err, gitstore.ErrNotFound) {
		return nil, "", gateway.ErrGitNotFound
	}
	if err != nil {
		return nil, "", err
	}
	files := make([]string, 0, len(contents))
	for f := range contents {
		files = append(files, f)
	}
	sort.Strings(files)
	merged := map[string]map[string]*yaml.Node{}
	from := map[string]string{} // section/name -> file
	for _, f := range files {
		content := contents[f]
		invalid := func(err error) error { return fmt.Errorf("%w: %s: %w", gateway.ErrInvalidConfig, f, err) }
		if _, err := config.Parse(content); err != nil {
			return nil, "", invalid(err)
		}
		var doc yaml.Node
		if err := yaml.Unmarshal(content, &doc); err != nil || len(doc.Content) == 0 {
			return nil, "", invalid(errors.New("not a YAML document"))
		}
		if hasAnchors(&doc) {
			return nil, "", invalid(errors.New("YAML anchors and aliases are not supported in repository files"))
		}
		root := doc.Content[0]
		for i := 0; i+1 < len(root.Content); i += 2 {
			section, entries := root.Content[i].Value, root.Content[i+1]
			switch section {
			case "version":
				continue
			case "configmap":
				return nil, "", invalid(errors.New("the config map is not read from the repository"))
			}
			if merged[section] == nil {
				merged[section] = map[string]*yaml.Node{}
			}
			for j := 0; j+1 < len(entries.Content); j += 2 {
				name := entries.Content[j].Value
				if other, dup := from[section+"/"+name]; dup {
					return nil, "", invalid(fmt.Errorf("%s.%s is also defined in %s", section, name, other))
				}
				from[section+"/"+name] = f
				merged[section][name] = entries.Content[j+1]
			}
		}
	}
	str := func(v string) *yaml.Node { return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v} }
	root := &yaml.Node{Kind: yaml.MappingNode, Content: []*yaml.Node{str("version"), str("1")}}
	for _, section := range repoSections { // every section, so each is managed
		m := &yaml.Node{Kind: yaml.MappingNode}
		names := make([]string, 0, len(merged[section]))
		for n := range merged[section] {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			m.Content = append(m.Content, str(n), merged[section][n])
		}
		root.Content = append(root.Content, str(section), m)
	}
	doc, err := yaml.Marshal(root)
	if err != nil {
		return nil, "", err
	}
	// Validate the whole document here, so a problem names the files of
	// the artifacts it is about (cross-references may span files).
	if _, err := config.ParseValid(doc); err != nil {
		var in []string
		for key, f := range from {
			section, name, _ := strings.Cut(key, "/")
			// "flows.a" must not match inside "flows.adt".
			named := regexp.MustCompile(`(^|[^\w.-])` + regexp.QuoteMeta(section+"."+name) + `($|[^\w-])`)
			if named.MatchString(err.Error()) && !slices.Contains(in, f) {
				in = append(in, f)
			}
		}
		sort.Strings(in)
		if len(in) == 0 {
			return nil, "", fmt.Errorf("%w: %w", gateway.ErrInvalidConfig, err)
		}
		return nil, "", fmt.Errorf("%w: %s: %w", gateway.ErrInvalidConfig, strings.Join(in, ", "), err)
	}
	return doc, commit, nil
}

// hasAnchors reports whether n uses YAML anchors, aliases, or merge keys.
func hasAnchors(n *yaml.Node) bool {
	if n.Anchor != "" || n.Kind == yaml.AliasNode || (n.Kind == yaml.ScalarNode && n.Tag == "!!merge") {
		return true
	}
	for _, c := range n.Content {
		if hasAnchors(c) {
			return true
		}
	}
	return false
}
