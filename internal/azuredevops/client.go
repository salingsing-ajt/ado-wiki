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
	h, body, err := c.getRaw(ctx, path, q, "application/json")
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(body, out); err != nil {
		preview := string(body)
		if len(preview) > 200 {
			preview = preview[:200] + "..."
		}
		return nil, fmt.Errorf("decode %s: %w (body: %s)", path, err, strings.TrimSpace(preview))
	}
	return h, nil
}

// getRaw performs the request and returns the response body bytes. Used for
// binary payloads (wiki attachments) that don't decode as JSON.
func (c *Client) getRaw(ctx context.Context, path string, q url.Values, accept string) (http.Header, []byte, error) {
	u, err := url.Parse(c.baseURL + path)
	if err != nil {
		return nil, nil, err
	}
	if q == nil {
		q = url.Values{}
	}
	q.Set("api-version", "7.1")
	u.RawQuery = q.Encode()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(":"+c.pat)))
	if accept != "" {
		req.Header.Set("Accept", accept)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, nil, fmt.Errorf("%w (http %d)", ErrUnauthorized, resp.StatusCode)
	}
	// ADO often returns a 200 HTML sign-in page instead of a proper 401 when
	// the PAT is missing/expired — detect and treat as auth failure.
	if accept == "application/json" && looksLikeHTML(resp.Header.Get("Content-Type"), body) {
		return nil, nil, fmt.Errorf("%w (received HTML sign-in page — PAT is likely expired or revoked)", ErrUnauthorized)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, nil, fmt.Errorf("azure devops http %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return resp.Header, body, nil
}

func looksLikeHTML(contentType string, body []byte) bool {
	if strings.Contains(strings.ToLower(contentType), "text/html") {
		return true
	}
	trimmed := strings.TrimLeft(string(body), " \t\r\n")
	return strings.HasPrefix(trimmed, "<!DOCTYPE") || strings.HasPrefix(trimmed, "<html") || strings.HasPrefix(trimmed, "<!doctype")
}
