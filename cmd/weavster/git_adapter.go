package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"

	"github.com/weavster-dev/weavster/internal/config"
	"github.com/weavster-dev/weavster/internal/gateway"
	"github.com/weavster-dev/weavster/internal/gitstore"
)

// gitAdapter serves gateway.GitRepository from one on-disk repository.
type gitAdapter struct {
	mu    *sync.Mutex // go-git repositories are not safe for concurrent use
	store *gitstore.Store
}

func newGitAdapter(path string) (gitAdapter, error) {
	s, err := gitstore.OpenOrInit(path)
	if err != nil {
		return gitAdapter{}, fmt.Errorf("git: %s: %w", path, err)
	}
	return gitAdapter{mu: &sync.Mutex{}, store: s}, nil
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
	save := func(f string) {
		if _, ok := backup[f]; !ok {
			b, readErr := a.store.ReadFile(f)
			if readErr != nil {
				b = nil
			}
			backup[f] = b
		}
	}
	for _, f := range existing {
		if _, keep := files[f]; !keep && managedFile(f) {
			save(f)
			if err = a.store.RemoveFile(f); err != nil {
				return gateway.GitCommitResult{}, err
			}
		}
	}
	for f, content := range files {
		save(f)
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
