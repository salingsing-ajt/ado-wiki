package azuredevops

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
)

type Page struct {
	ID       int64  `json:"id"`
	Path     string `json:"path"`
	Content  string `json:"content"`
	SubPages []Page `json:"subPages"`
}

type WikiInfo struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	RepositoryID   string `json:"repositoryId"`
	ProjectID      string `json:"projectId"`
	Type           string `json:"type"`
	MappedPath     string `json:"mappedPath"`
	Version        struct {
		Version string `json:"version"`
	} `json:"version"`
}

// Warn is where continuation-token warnings go; tests override it.
var Warn io.Writer = os.Stderr

func (c *Client) GetWikiPageTree(ctx context.Context, project, wiki string) (*Page, error) {
	path := fmt.Sprintf("/%s/_apis/wiki/wikis/%s/pages",
		url.PathEscape(project), url.PathEscape(wiki))
	q := url.Values{
		"path":           {"/"},
		"recursionLevel": {"Full"},
		"includeContent": {"True"},
	}
	var root Page
	h, err := c.get(ctx, path, q, &root)
	if err != nil {
		return nil, err
	}
	if h.Get("X-MS-ContinuationToken") != "" {
		fmt.Fprintln(Warn, "warning: Azure DevOps returned a continuation token; the wiki response may be truncated.")
	}
	return &root, nil
}

// GetWikiPageContent fetches the markdown body of a single page by path.
// Needed because /pages?recursionLevel=Full returns subpages with empty
// Content and without ids, so per-page fetching is the only option.
func (c *Client) GetWikiPageContent(ctx context.Context, project, wiki, pagePath string) (string, error) {
	apiPath := fmt.Sprintf("/%s/_apis/wiki/wikis/%s/pages",
		url.PathEscape(project), url.PathEscape(wiki))
	q := url.Values{
		"path":           {pagePath},
		"includeContent": {"True"},
	}
	var page Page
	if _, err := c.get(ctx, apiPath, q, &page); err != nil {
		return "", err
	}
	return page.Content, nil
}

// GetWikiInfo fetches wiki metadata, including the backing git repository id
// used for downloading attachments.
func (c *Client) GetWikiInfo(ctx context.Context, project, wiki string) (*WikiInfo, error) {
	apiPath := fmt.Sprintf("/%s/_apis/wiki/wikis/%s",
		url.PathEscape(project), url.PathEscape(wiki))
	var info WikiInfo
	if _, err := c.get(ctx, apiPath, nil, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// GetWikiAttachment downloads a single wiki attachment (image/file) by name,
// reading the raw blob from the wiki's backing git repository. `name` is the
// filename as it appears under `.attachments/` in the wiki.
// The /wiki/.../attachments endpoint is upload-only (PUT), so downloads go
// through git items instead.
func (c *Client) GetWikiAttachment(ctx context.Context, project, repoID, name string) ([]byte, error) {
	apiPath := fmt.Sprintf("/%s/_apis/git/repositories/%s/items",
		url.PathEscape(project), url.PathEscape(repoID))
	q := url.Values{
		"path":    {"/.attachments/" + name},
		"$format": {"octetStream"},
	}
	_, body, err := c.getRaw(ctx, apiPath, q, "")
	if err != nil {
		return nil, err
	}
	return body, nil
}
