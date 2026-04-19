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
