package sync

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/arjayads/wikivault/internal/azuredevops"
)

type fakeFetcher struct{ tree *azuredevops.Page }

func (f *fakeFetcher) GetWikiPageTree(_ context.Context, _, _ string) (*azuredevops.Page, error) {
	return f.tree, nil
}

func (f *fakeFetcher) GetWikiPageContent(_ context.Context, _, _, _ string) (string, error) {
	return "", nil
}

func (f *fakeFetcher) GetWikiInfo(_ context.Context, _, _ string) (*azuredevops.WikiInfo, error) {
	return &azuredevops.WikiInfo{RepositoryID: "fake-repo-id"}, nil
}

func (f *fakeFetcher) GetWikiAttachment(_ context.Context, _, _, _ string) ([]byte, error) {
	return nil, nil
}

func TestRunWritesAndPrunes(t *testing.T) {
	dir := t.TempDir()

	first := &fakeFetcher{tree: &azuredevops.Page{Path: "/", SubPages: []azuredevops.Page{
		{Path: "/A", Content: "a"},
		{Path: "/B", Content: "b"},
	}}}
	if _, err := Run(context.Background(), Options{Fetcher: first, OutputDir: dir}); err != nil {
		t.Fatal(err)
	}

	second := &fakeFetcher{tree: &azuredevops.Page{Path: "/", SubPages: []azuredevops.Page{
		{Path: "/A", Content: "a-updated"},
		{Path: "/C", Content: "c"},
	}}}
	res, err := Run(context.Background(), Options{Fetcher: second, OutputDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if res.Written != 2 || res.Deleted != 1 {
		t.Fatalf("counts: written=%d deleted=%d, want 2/1", res.Written, res.Deleted)
	}
	if _, err := os.Stat(filepath.Join(dir, "B.md")); !os.IsNotExist(err) {
		t.Fatalf("B.md should be pruned")
	}
	body, _ := os.ReadFile(filepath.Join(dir, "A.md"))
	if string(body) != "a-updated" {
		t.Fatalf("A.md = %q", body)
	}
}

func TestRunWritesNestedDirs(t *testing.T) {
	dir := t.TempDir()
	f := &fakeFetcher{tree: &azuredevops.Page{Path: "/", SubPages: []azuredevops.Page{
		{Path: "/Home", Content: "h", SubPages: []azuredevops.Page{
			{Path: "/Home/Child", Content: "c"},
		}},
	}}}
	if _, err := Run(context.Background(), Options{Fetcher: f, OutputDir: dir}); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"Home.md", "Home/Child.md", ".wikisync.json"} {
		if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
			t.Errorf("missing %s: %v", rel, err)
		}
	}
}

func TestRunRemovesEmptiedDirs(t *testing.T) {
	dir := t.TempDir()
	first := &fakeFetcher{tree: &azuredevops.Page{Path: "/", SubPages: []azuredevops.Page{
		{Path: "/Home", Content: "h", SubPages: []azuredevops.Page{
			{Path: "/Home/Child", Content: "c"},
		}},
	}}}
	if _, err := Run(context.Background(), Options{Fetcher: first, OutputDir: dir}); err != nil {
		t.Fatal(err)
	}
	second := &fakeFetcher{tree: &azuredevops.Page{Path: "/"}}
	if _, err := Run(context.Background(), Options{Fetcher: second, OutputDir: dir}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "Home")); !os.IsNotExist(err) {
		t.Errorf("Home/ should be pruned once empty; got %v", err)
	}
}
