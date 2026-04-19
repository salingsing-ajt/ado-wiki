package sync

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/arjayads/wikivault/internal/azuredevops"
)

// Fetcher is the narrow interface Run depends on; *azuredevops.Client
// satisfies it in production and fakes satisfy it in tests.
type Fetcher interface {
	GetWikiPageTree(ctx context.Context, project, wiki string) (*azuredevops.Page, error)
	GetWikiPageContent(ctx context.Context, project, wiki, pagePath string) (string, error)
}

type Options struct {
	Fetcher   Fetcher
	Project   string
	Wiki      string
	OutputDir string
}

type Result struct {
	Written int
	Deleted int
}

func Run(ctx context.Context, opts Options) (*Result, error) {
	tree, err := opts.Fetcher.GetWikiPageTree(ctx, opts.Project, opts.Wiki)
	if err != nil {
		return nil, err
	}
	// ADO's recursive pages endpoint returns empty Content and no ids for
	// subpages; fill in each body with a second call keyed on path.
	var fill func(p *azuredevops.Page) error
	fill = func(p *azuredevops.Page) error {
		if p.Path != "/" && p.Content == "" {
			body, err := opts.Fetcher.GetWikiPageContent(ctx, opts.Project, opts.Wiki, p.Path)
			if err != nil {
				return fmt.Errorf("fetch content path=%s: %w", p.Path, err)
			}
			p.Content = body
		}
		for i := range p.SubPages {
			if err := fill(&p.SubPages[i]); err != nil {
				return err
			}
		}
		return nil
	}
	if err := fill(tree); err != nil {
		return nil, err
	}
	outDir := filepath.Clean(opts.OutputDir)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}
	writes := WalkTree(tree)

	prev, err := LoadManifest(outDir)
	if err != nil {
		return nil, err
	}

	newSet := make(map[string]struct{}, len(writes))
	for _, w := range writes {
		abs := filepath.Join(outDir, filepath.FromSlash(w.RelPath))
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(abs, []byte(w.Content), 0o644); err != nil {
			return nil, fmt.Errorf("write %s: %w", abs, err)
		}
		newSet[w.RelPath] = struct{}{}
	}

	var deleted int
	for _, rel := range prev.Files {
		if _, kept := newSet[rel]; kept {
			continue
		}
		abs := filepath.Join(outDir, filepath.FromSlash(rel))
		if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		// Remove now-empty ancestor directories, stopping at outDir.
		// os.Remove fails on non-empty dirs, which ends the walk.
		for parent := filepath.Dir(abs); parent != outDir; parent = filepath.Dir(parent) {
			if err := os.Remove(parent); err != nil {
				break
			}
		}
		deleted++
	}

	files := make([]string, 0, len(newSet))
	for rel := range newSet {
		files = append(files, rel)
	}
	sort.Strings(files) // deterministic manifest output
	if err := SaveManifest(outDir, &Manifest{Files: files}); err != nil {
		return nil, err
	}
	return &Result{Written: len(writes), Deleted: deleted}, nil
}
