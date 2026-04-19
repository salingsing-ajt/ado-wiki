package azuredevops

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ErrUnauthorized is returned for any 401/403 response. Callers surface this
// as a "re-run wiki login" hint instead of echoing the raw HTTP body.
var ErrUnauthorized = errors.New("azure devops rejected the PAT")

type Client struct {
	baseURL string
	pat     string
	http    *http.Client
}

func NewClient(baseURL, pat string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		pat:     pat,
		// Full-recursion responses on large wikis can run past 60s.
		http: &http.Client{Timeout: 5 * time.Minute},
	}
}

func DefaultBaseURL(org string) string {
	return "https://dev.azure.com/" + url.PathEscape(org)
}

func (c *Client) get(ctx context.Context, path string, q url.Values, out any) (http.Header, error) {
	u, err := url.Parse(c.baseURL + path)
	if err != nil {
		return nil, err
	}
	if q == nil {
		q = url.Values{}
	}
	q.Set("api-version", "7.1")
	u.RawQuery = q.Encode()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(":"+c.pat)))
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("%w (http %d)", ErrUnauthorized, resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("azure devops http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if err := json.Unmarshal(body, out); err != nil {
		return nil, err
	}
	return resp.Header, nil
}
