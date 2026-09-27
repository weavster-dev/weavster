package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"

	"github.com/weavster-dev/weavster/internal/config"
	"github.com/weavster-dev/weavster/internal/gateway"
	"github.com/weavster-dev/weavster/internal/gitstore"
)

// gitSections maps an artifact kind to its repository directory and
// document section (#107 D-51). The config map is never committed.
var gitSections = map[string]string{
	"flow": "flows", "alert": "alerts", "snippet": "snippets", "library": "snippetLibraries",
	"script": "scripts", "settings": "settings",
}

// gitAdapter serves gateway.GitRepository from one on-disk repository.
type gitAdapter struct {
	mu    *sync.Mutex // go-git repositories are not safe for concurrent use
	store *gitstore.Store
	path  string
}

func newGitAdapter(path string) (gitAdapter, error) {
	s, err := gitstore.OpenOrInit(path)
	if err != nil {
		return gitAdapter{}, fmt.Errorf("git: %s: %w", path, err)
	}
	return gitAdapter{mu: &sync.Mutex{}, store: s, path: path}, nil
}

func (a gitAdapter) GitInfo(context.Context) (gateway.GitInfo, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	head, branch, err := a.store.Head()
	return gateway.GitInfo{Path: a.path, Branch: branch, Head: head}, err
}

// GitCommit makes the repository's configuration files match live and
// commits them when anything changed. Files outside the configuration
// directories are left alone.
func (a gitAdapter) GitCommit(_ context.Context, live gateway.ConfigBundle, message, author string) (gateway.GitCommitResult, error) {
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
	for _, f := range existing {
		dir, _, _ := strings.Cut(f, "/")
		if _, keep := files[f]; !keep && isConfigDir(dir) && strings.HasSuffix(f, ".yaml") {
			if err := a.store.RemoveFile(f); err != nil {
				return gateway.GitCommitResult{}, err
			}
		}
	}
	for f, content := range files {
		if err := a.store.WriteFile(f, content); err != nil {
			return gateway.GitCommitResult{}, err
		}
	}
	changed, err := a.store.WorkingTreeDiff()
	if err != nil {
		return gateway.GitCommitResult{}, err
	}
	res := gateway.GitCommitResult{Changed: changed}
	if len(changed) == 0 {
		res.Head, _, err = a.store.Head()
		return res, err
	}
	res.Committed = true
	res.Head, err = a.store.Commit(message, gitstore.Author{Name: author})
	return res, err
}

func isConfigDir(dir string) bool {
	for _, d := range gitSections {
		if d == dir {
			return true
		}
	}
	return false
}

func (a gitAdapter) GitLog(_ context.Context, path string, limit int) ([]gateway.GitRevision, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	var revs []gitstore.Revision
	var err error
	if path == "" {
		revs, err = a.store.Log()
	} else {
		revs, err = a.store.History(path)
	}
	if err != nil {
		return nil, err
	}
	out := make([]gateway.GitRevision, 0, min(len(revs), limit))
	for _, r := range revs[:min(len(revs), limit)] {
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
// document per artifact at <section>/<escaped name>.yaml (#107 D-51).
func configFiles(cfg *config.Config) (map[string][]byte, error) {
	out := map[string][]byte{}
	for key, content := range cfg.Artifacts() {
		kind, name, _ := strings.Cut(key, "/")
		section, ok := gitSections[kind]
		if !ok {
			continue // the config map
		}
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
		out[section+"/"+url.PathEscape(name)+".yaml"] = b
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
