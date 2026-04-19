package sync

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/arjayads/wikivault/internal/azuredevops"
)

// Fetcher is the narrow interface Run depends on; *azuredevops.Client
// satisfies it in production and fakes satisfy it in tests.
type Fetcher interface {
	GetWikiPageTree(ctx context.Context, project, wiki string) (*azuredevops.Page, error)
	GetWikiPageContent(ctx context.Context, project, wiki, pagePath string) (string, error)
	GetWikiInfo(ctx context.Context, project, wiki string) (*azuredevops.WikiInfo, error)
	GetWikiAttachment(ctx context.Context, project, repoID, name string) ([]byte, error)
}

type Options struct {
	Fetcher   Fetcher
	Project   string
	Wiki      string
	OutputDir string
	// Progress, if set, is called after each per-page content fetch with the
	// 1-based index, total pages being fetched, and the page path just
	// fetched. Optional — nil means silent.
	Progress func(done, total int, path string)
	// AttachProgress, if set, is called after each attachment download with
	// the 1-based index, total attachments, and the attachment name.
	AttachProgress func(done, total int, name string)
}

type Result struct {
	Written     int
	Deleted     int
	Attachments int
	Missing     int
}

func Run(ctx context.Context, opts Options) (*Result, error) {
	tree, err := opts.Fetcher.GetWikiPageTree(ctx, opts.Project, opts.Wiki)
	if err != nil {
		return nil, err
	}
	// ADO's recursive pages endpoint returns empty Content and no ids for
	// subpages; fill in each body with a second call keyed on path.
	total := countFetchable(tree)
	done := 0
	var fill func(p *azuredevops.Page) error
	fill = func(p *azuredevops.Page) error {
		if p.Path != "/" && p.Content == "" {
			body, err := opts.Fetcher.GetWikiPageContent(ctx, opts.Project, opts.Wiki, p.Path)
			if err != nil {
				return fmt.Errorf("fetch content path=%s: %w", p.Path, err)
			}
			p.Content = body
			done++
			if opts.Progress != nil {
				opts.Progress(done, total, p.Path)
			}
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

	attachments := collectAttachments(tree)
	attachDir := filepath.Join(outDir, ".attachments")
	missing := 0
	if len(attachments) > 0 {
		if err := os.MkdirAll(attachDir, 0o755); err != nil {
			return nil, err
		}
		info, err := opts.Fetcher.GetWikiInfo(ctx, opts.Project, opts.Wiki)
		if err != nil {
			return nil, fmt.Errorf("resolve wiki repo id: %w", err)
		}
		for i, name := range attachments {
			data, err := opts.Fetcher.GetWikiAttachment(ctx, opts.Project, info.RepositoryID, name)
			if errors.Is(err, azuredevops.ErrUnauthorized) {
				return nil, fmt.Errorf("attachment download requires a PAT with 'Code (Read)' scope in addition to 'Wiki (Read)'; regenerate the PAT and re-run 'wiki login'")
			}
			if err != nil {
				// A referenced attachment may no longer exist in the repo
				// (renamed, deleted, branch mismatch). Skip it so one bad
				// reference doesn't abort the whole sync.
				missing++
				if opts.AttachProgress != nil {
					opts.AttachProgress(i+1, len(attachments), "[missing] "+name)
				}
				continue
			}
			if err := os.WriteFile(filepath.Join(attachDir, name), data, 0o644); err != nil {
				return nil, fmt.Errorf("write attachment %s: %w", name, err)
			}
			if opts.AttachProgress != nil {
				opts.AttachProgress(i+1, len(attachments), name)
			}
		}
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
		content := rewriteAttachmentLinks(w.Content, strings.Count(w.RelPath, "/"))
		if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
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
	return &Result{
		Written:     len(writes),
		Deleted:     deleted,
		Attachments: len(attachments) - missing,
		Missing:     missing,
	}, nil
}

// attachmentLinkPrefix matches the prefix of a markdown attachment link:
// the `](` followed by optional `/` and `.attachments/`. We replace only the
// prefix so filenames with spaces/parens inside the URL stay intact.
var attachmentLinkPrefix = regexp.MustCompile(`\]\(/?\.attachments/`)

// rewriteAttachmentLinks rewrites `/.attachments/...` and `.attachments/...`
// references to a path relative to the page's location. `depth` is the
// number of directory segments between the page file and the wiki root.
func rewriteAttachmentLinks(content string, depth int) string {
	prefix := strings.Repeat("../", depth) + ".attachments/"
	return attachmentLinkPrefix.ReplaceAllString(content, "]("+prefix)
}

func collectAttachments(root *azuredevops.Page) []string {
	seen := map[string]struct{}{}
	var walk func(p *azuredevops.Page)
	walk = func(p *azuredevops.Page) {
		scanAttachments(p.Content, seen)
		for i := range p.SubPages {
			walk(&p.SubPages[i])
		}
	}
	walk(root)
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// scanAttachments finds every markdown link/image target that points at the
// `.attachments` folder and records the attachment filename in `seen`.
//
// Simple regex parsing fails here because ADO wiki emits attachment names
// verbatim — including spaces, `(`, `)` — so `](foo (1).pdf)` has nested
// parens that a naive `\([^)]+\)` stops on. We scan for `](` and then walk
// the URL with paren-depth tracking until the matching `)`.
func scanAttachments(content string, seen map[string]struct{}) {
	i := 0
	for {
		rel := strings.Index(content[i:], "](")
		if rel < 0 {
			return
		}
		openParen := i + rel + 1
		target, end := readLinkTarget(content, openParen)
		i = end + 1
		if target == "" {
			continue
		}
		// Strip any "title" part: CommonMark allows `](url "title")`.
		if sp := strings.IndexAny(target, " \t"); sp >= 0 {
			target = target[:sp]
		}
		t := strings.TrimPrefix(target, "/")
		if !strings.HasPrefix(t, ".attachments/") {
			continue
		}
		name := strings.TrimPrefix(t, ".attachments/")
		if k := strings.IndexAny(name, "?#"); k >= 0 {
			name = name[:k]
		}
		if decoded, err := url.QueryUnescape(name); err == nil {
			name = decoded
		}
		if name != "" {
			seen[name] = struct{}{}
		}
	}
}

// readLinkTarget returns the content between `(` at `start` and the matching
// `)` (paren-balanced, stops on newline or end). Returns end index of the
// closing `)` or -1 if unterminated.
func readLinkTarget(s string, start int) (string, int) {
	if start >= len(s) || s[start] != '(' {
		return "", start
	}
	depth := 1
	j := start + 1
	for j < len(s) {
		c := s[j]
		switch c {
		case '\\':
			if j+1 < len(s) {
				j += 2
				continue
			}
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return s[start+1 : j], j
			}
		case '\n', '\r':
			return "", j
		}
		j++
	}
	return "", j
}

func countFetchable(p *azuredevops.Page) int {
	if p == nil {
		return 0
	}
	n := 0
	if p.Path != "/" && p.Content == "" {
		n = 1
	}
	for i := range p.SubPages {
		n += countFetchable(&p.SubPages[i])
	}
	return n
}
