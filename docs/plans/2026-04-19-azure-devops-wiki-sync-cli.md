# Azure DevOps Wiki Sync CLI Implementation Plan

**Goal:** A cross-platform Go CLI `wiki` with two commands — `wiki login --pat <PAT>` (stores the PAT in the OS keyring) and `wiki sync` (reads `./wiki.yaml`, downloads the full wiki with one ADO API call, writes pages into CWD mirroring the ADO page hierarchy).

**Stack:** Go 1.22, `spf13/cobra`, `zalando/go-keyring`, `yaml.v3`, GoReleaser for multi-OS builds.

**Conventions:**
- Module: `github.com/arjayads/wikivault`. To rename, run `find . -type f -name '*.go' -exec sed -i 's|github.com/arjayads/wikivault|<new-path>|g' {} +` plus edit `go.mod`.
- Keyring service/account: `wikivault` / `azure-devops-pat`.
- Config file: `./wiki.yaml` with `organization`, `project`, `wiki` — user creates or edits a generated template.
- API version: `7.1`. One call: `GET /{project}/_apis/wiki/wikis/{wiki}/pages?path=/&recursionLevel=Full&includeContent=True`.
- Filename sanitization: replace `< > : " / \ | ? *` and control chars with `_`; trailing `.` and space replaced with `_`; Windows reserved basenames (`CON`, `PRN`, `AUX`, `NUL`, `COM1-9`, `LPT1-9`, case-insensitive) get a `_` suffix. Spaces inside names preserved.
- Page with children → `Page.md` plus sibling `Page/` dir with descendants (matches ADO's git-backed layout).
- Re-sync prunes pages listed in the previous `.wikisync.json` manifest that aren't in the new fetch. User-authored `.md` files the tool has never written are left alone.

Snippets assume a POSIX shell — on Windows use Git Bash or WSL.

---

## Task 1: Scaffold

**Files:** `go.mod`, `cmd/wiki/main.go`, `internal/cli/root.go`, `Makefile`, `.gitignore`

- [ ] **Initialize module**

Pinning `go 1.22` keeps contributors on older toolchains able to build; without it, `go mod init` stamps whatever version is on the dev's `PATH`.

```bash
[ -f go.mod ] || go mod init github.com/arjayads/wikivault
go mod edit -go=1.22
go get github.com/spf13/cobra
```

- [ ] **`cmd/wiki/main.go`**

`version` is stamped by GoReleaser `-ldflags -X` (Task 8). It must exist as a package-level string or the flag silently no-ops.

```go
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/arjayads/wikivault/internal/cli"
)

var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := cli.Execute(ctx, version); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
```

- [ ] **`internal/cli/root.go`**

```go
package cli

import (
	"context"

	"github.com/spf13/cobra"
)

func newRoot(version string) *cobra.Command {
	return &cobra.Command{
		Use:           "wiki",
		Short:         "Sync an Azure DevOps wiki to your local filesystem",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
}

func Execute(ctx context.Context, version string) error {
	return newRoot(version).ExecuteContext(ctx)
}
```

- [ ] **`Makefile`**

```makefile
BINARY := wiki
ifeq ($(OS),Windows_NT)
	BINARY := wiki.exe
endif

.PHONY: build test release-snapshot
build:
	go build -o $(BINARY) ./cmd/wiki
test:
	go test ./...
release-snapshot:
	goreleaser release --snapshot --clean
```

- [ ] **`.gitignore`**

```
/wiki
/wiki.exe
/dist/
*.test
coverage.*
.idea/
.vscode/
.DS_Store
```

- [ ] **Verify**

```bash
make build
./wiki --help       # ./wiki.exe on Windows
./wiki --version    # prints "wiki version dev"
```

---

## Task 2: `wiki.yaml` config

**Files:** `internal/config/config.go` + test

- [ ] **`internal/config/config.go`**

`KnownFields(true)` turns typos like `organisation:` into a parse error that names the unknown key, instead of a downstream "organization is required" with no hint at the typo. Placeholder strings (the ones Sync writes into the template) are rejected by Validate so the user gets a clear "edit wiki.yaml first" instead of a 404 from a literal `your-azure-devops-organization` URL.

```go
package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const FileName = "wiki.yaml"

// Placeholders is every string the Sync template writes. Validate rejects
// configs that still contain any of them so we don't hit ADO with the
// literal placeholder values.
var Placeholders = map[string]bool{
	"your-azure-devops-organization": true,
	"Your Project Name":              true,
	"Your Project.wiki":              true,
}

type Config struct {
	Organization string `yaml:"organization"`
	Project      string `yaml:"project"`
	Wiki         string `yaml:"wiki"`
}

func (c *Config) Validate() error {
	fields := []struct {
		name, value string
	}{
		{"organization", c.Organization},
		{"project", c.Project},
		{"wiki", c.Wiki},
	}
	for _, f := range fields {
		if f.value == "" {
			return fmt.Errorf("%s is required", f.name)
		}
		if Placeholders[f.value] {
			return fmt.Errorf("%s still has the template placeholder %q — edit wiki.yaml", f.name, f.value)
		}
	}
	return nil
}

var ErrNotFound = errors.New("wiki.yaml not found")

func Path(dir string) string { return filepath.Join(dir, FileName) }

func Load(dir string) (*Config, error) {
	data, err := os.ReadFile(Path(dir))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w at %s", ErrNotFound, Path(dir))
		}
		return nil, err
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var c Config
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("wiki.yaml: %w", err)
	}
	return &c, nil
}

func Save(dir string, c *Config) error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(Path(dir), data, 0o644)
}
```

- [ ] **`internal/config/config_test.go`**

```go
package config

import (
	"errors"
	"testing"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	in := &Config{Organization: "contoso", Project: "Platform", Wiki: "Platform.wiki"}
	if err := Save(dir, in); err != nil {
		t.Fatal(err)
	}
	out, err := Load(dir)
	if err != nil || *out != *in {
		t.Fatalf("roundtrip: got %+v err=%v, want %+v", out, err, in)
	}
}

func TestLoadMissing(t *testing.T) {
	if _, err := Load(t.TempDir()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
}

func TestValidate(t *testing.T) {
	if err := (&Config{Organization: "o", Project: "p", Wiki: "w"}).Validate(); err != nil {
		t.Errorf("valid config rejected: %v", err)
	}
	for _, miss := range []*Config{
		{Project: "p", Wiki: "w"},
		{Organization: "o", Wiki: "w"},
		{Organization: "o", Project: "p"},
	} {
		if err := miss.Validate(); err == nil {
			t.Errorf("missing-field config accepted: %+v", miss)
		}
	}
	placeholder := &Config{Organization: "your-azure-devops-organization", Project: "p", Wiki: "w"}
	if err := placeholder.Validate(); err == nil || !strings.Contains(err.Error(), "placeholder") {
		t.Errorf("placeholder not rejected: %v", err)
	}
}

func TestLoadRejectsUnknownKey(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(Path(dir), []byte("organisation: contoso\nproject: p\nwiki: w\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(dir)
	if err == nil || !strings.Contains(err.Error(), "organisation") {
		t.Fatalf("want unknown-key error naming \"organisation\", got %v", err)
	}
}
```

The new test imports need `os` and `strings`:

```go
import (
	"errors"
	"os"
	"strings"
	"testing"
)
```

```bash
go get gopkg.in/yaml.v3
go test ./internal/config/...
```

---

## Task 3: PAT keyring

**Files:** `internal/credentials/keyring.go` + test

- [ ] **`internal/credentials/keyring.go`**

```go
package credentials

import (
	"errors"

	"github.com/zalando/go-keyring"
)

const (
	service = "wikivault"
	account = "azure-devops-pat"
)

var ErrNotFound = errors.New("pat not found in keyring")

func Save(pat string) error {
	if pat == "" {
		return errors.New("pat is empty")
	}
	return keyring.Set(service, account, pat)
}

func Get() (string, error) {
	v, err := keyring.Get(service, account)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", ErrNotFound
	}
	return v, err
}

func Delete() error {
	err := keyring.Delete(service, account)
	if errors.Is(err, keyring.ErrNotFound) {
		return ErrNotFound
	}
	return err
}
```

- [ ] **`internal/credentials/keyring_test.go`**

`keyring.MockInit()` swaps in an in-memory backend so tests don't need a real OS keyring.

```go
package credentials

import (
	"errors"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestSaveGet(t *testing.T) {
	keyring.MockInit()
	if err := Save("secret"); err != nil {
		t.Fatal(err)
	}
	got, err := Get()
	if err != nil || got != "secret" {
		t.Fatalf("got %q err=%v", got, err)
	}
}

func TestGetMissing(t *testing.T) {
	keyring.MockInit()
	if _, err := Get(); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got %v, want ErrNotFound", err)
	}
}

func TestDelete(t *testing.T) {
	keyring.MockInit()
	_ = Save("secret")
	if err := Delete(); err != nil {
		t.Fatal(err)
	}
	if _, err := Get(); !errors.Is(err, ErrNotFound) {
		t.Fatalf("after Delete, Get = %v, want ErrNotFound", err)
	}
	if err := Delete(); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second Delete = %v, want ErrNotFound", err)
	}
}
```

```bash
go get github.com/zalando/go-keyring
go test ./internal/credentials/...
```

---

## Task 4: Azure DevOps client + wiki fetch

**Files:** `internal/azuredevops/client.go`, `internal/azuredevops/wiki.go` + tests

Basic auth is `":{PAT}"` base64-encoded (the username half is empty by ADO convention).

- [ ] **`internal/azuredevops/client.go`**

```go
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
```

- [ ] **`internal/azuredevops/wiki.go`**

```go
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
```

- [ ] **`internal/azuredevops/client_test.go`**

```go
package azuredevops

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGetWikiPageTree(t *testing.T) {
	var gotAuth, gotAPIVer, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAPIVer = r.URL.Query().Get("api-version")
		gotPath = r.URL.Path
		w.Write([]byte(`{"id":1,"path":"/","content":"","subPages":[
			{"id":2,"path":"/Home","content":"# Home","subPages":[
				{"id":3,"path":"/Home/Child","content":"c","subPages":[]}
			]}
		]}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "the-pat")
	root, err := c.GetWikiPageTree(context.Background(), "Platform", "Platform.wiki")
	if err != nil {
		t.Fatal(err)
	}

	wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte(":the-pat"))
	if gotAuth != wantAuth {
		t.Errorf("Authorization = %q, want %q", gotAuth, wantAuth)
	}
	if gotAPIVer != "7.1" {
		t.Errorf("api-version = %q", gotAPIVer)
	}
	if !strings.Contains(gotPath, "/Platform/_apis/wiki/wikis/Platform.wiki/pages") {
		t.Errorf("path = %q", gotPath)
	}
	if len(root.SubPages) != 1 || root.SubPages[0].Content != "# Home" {
		t.Errorf("tree mismatch: %+v", root)
	}
}

func TestNon2xxIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "bad")
	if _, err := c.GetWikiPageTree(context.Background(), "p", "w"); err == nil {
		t.Fatal("expected error on 500")
	}
}

func TestUnauthorizedIsTyped(t *testing.T) {
	for _, code := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(code)
		}))
		c := NewClient(srv.URL, "bad")
		_, err := c.GetWikiPageTree(context.Background(), "p", "w")
		srv.Close()
		if !errors.Is(err, ErrUnauthorized) {
			t.Errorf("status %d: err=%v, want wraps ErrUnauthorized", code, err)
		}
	}
}

func TestContinuationTokenWarns(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-MS-ContinuationToken", "abc")
		w.Write([]byte(`{"id":1,"path":"/","subPages":[]}`))
	}))
	defer srv.Close()

	var buf bytes.Buffer
	orig := Warn
	Warn = &buf
	t.Cleanup(func() { Warn = orig })

	c := NewClient(srv.URL, "pat")
	if _, err := c.GetWikiPageTree(context.Background(), "p", "w"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "continuation token") {
		t.Errorf("expected warning, got %q", buf.String())
	}
}
```

---

## Task 5: Tree → file writes

**Files:** `internal/sync/filesystem.go` + test

- [ ] **`internal/sync/filesystem.go`**

```go
package sync

import (
	"path"
	"strings"

	"github.com/arjayads/wikivault/internal/azuredevops"
)

type FileWrite struct {
	RelPath string // forward-slash, relative to output root
	Content string
}

var forbidden = map[rune]bool{
	'<': true, '>': true, ':': true, '"': true, '/': true,
	'\\': true, '|': true, '?': true, '*': true,
}

// windowsReserved is the set of device names Windows refuses to open. Match
// is case-insensitive and ignores anything from the first dot onward, so
// "CON", "con.md", and "Con.anything.md" are all reserved.
var windowsReserved = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true,
	"COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true,
	"LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
}

func SanitizeTitle(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r < 0x20 || forbidden[r] {
			b.WriteRune('_')
		} else {
			b.WriteRune(r)
		}
	}
	result := b.String()
	if result == "" {
		return "_"
	}
	// "." and ".." as path segments could traverse outside the output dir;
	// prefix with "_" so they become ordinary filenames.
	if result == "." || result == ".." {
		return "_" + result
	}
	// Windows silently strips trailing dots and spaces, which can collide
	// with a sibling page. Replace them so the filename is stable.
	for strings.HasSuffix(result, ".") || strings.HasSuffix(result, " ") {
		result = result[:len(result)-1] + "_"
	}
	// Windows refuses to open reserved device names regardless of extension.
	// Check the portion before the first dot.
	stem := strings.ToUpper(result)
	if i := strings.IndexByte(stem, '.'); i >= 0 {
		stem = stem[:i]
	}
	if windowsReserved[stem] {
		result += "_"
	}
	return result
}

func adoPathToFSDir(adoPath string) string {
	trimmed := strings.TrimPrefix(adoPath, "/")
	if trimmed == "" {
		return ""
	}
	segs := strings.Split(trimmed, "/")
	for i, s := range segs {
		segs[i] = SanitizeTitle(s)
	}
	return path.Join(segs...)
}

// WalkTree flattens a page tree into writes. The root "/" is never emitted;
// its children become top-level files. A page with children produces both
// `Page.md` and a `Page/` directory (via its children's paths).
func WalkTree(root *azuredevops.Page) []FileWrite {
	if root == nil {
		return nil
	}
	var out []FileWrite
	var walk func(p *azuredevops.Page)
	walk = func(p *azuredevops.Page) {
		if p.Path != "/" {
			out = append(out, FileWrite{
				RelPath: adoPathToFSDir(p.Path) + ".md",
				Content: p.Content,
			})
		}
		for i := range p.SubPages {
			walk(&p.SubPages[i])
		}
	}
	walk(root)
	return out
}
```

- [ ] **`internal/sync/filesystem_test.go`**

```go
package sync

import (
	"reflect"
	"sort"
	"testing"

	"github.com/arjayads/wikivault/internal/azuredevops"
)

func TestSanitizeTitle(t *testing.T) {
	cases := map[string]string{
		"Getting Started":    "Getting Started",
		"Foo/Bar":            "Foo_Bar",
		`a:b*c?d"e<f>g|h\i`: "a_b_c_d_e_f_g_h_i",
		"":                   "_",
		".":                  "_.",
		"..":                 "_..",
		"Foo.":                "Foo_",
		"Foo ":                "Foo_",
		"Foo. ":               "Foo._", // the loop stops at the first non-trailing-dot-or-space
		"CON":                 "CON_",
		"con":                 "con_",
		"PRN.md":              "PRN.md_", // extension doesn't save it
		"COM1":                "COM1_",
		"COM10":               "COM10", // only 1-9 are reserved
		"console":             "console",
	}
	for in, want := range cases {
		if got := SanitizeTitle(in); got != want {
			t.Errorf("SanitizeTitle(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWalkTree(t *testing.T) {
	root := &azuredevops.Page{Path: "/", SubPages: []azuredevops.Page{
		{Path: "/Alpha", Content: "A", SubPages: []azuredevops.Page{
			{Path: "/Alpha/Beta", Content: "B"},
		}},
		{Path: "/Bad:Name", Content: "X"},
	}}
	got := WalkTree(root)
	sort.Slice(got, func(i, j int) bool { return got[i].RelPath < got[j].RelPath })
	want := []FileWrite{
		{"Alpha.md", "A"},
		{"Alpha/Beta.md", "B"},
		{"Bad_Name.md", "X"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestWalkTreeNil(t *testing.T) {
	if got := WalkTree(nil); len(got) != 0 {
		t.Fatalf("WalkTree(nil) = %+v, want empty", got)
	}
}
```

---

## Task 6: Manifest + sync orchestration

**Files:** `internal/sync/manifest.go`, `internal/sync/sync.go` + test

The manifest tracks the previous run's output so we can prune pages deleted in ADO without touching user-authored files. Lives at `<outputDir>/.wikisync.json`.

- [ ] **`internal/sync/manifest.go`**

```go
package sync

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

type Manifest struct {
	Files []string `json:"files"`
}

func manifestPath(dir string) string { return filepath.Join(dir, ".wikisync.json") }

func LoadManifest(dir string) (*Manifest, error) {
	data, err := os.ReadFile(manifestPath(dir))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Manifest{}, nil
		}
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func SaveManifest(dir string, m *Manifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(manifestPath(dir), data, 0o600)
}
```

- [ ] **`internal/sync/sync.go`**

```go
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
```

- [ ] **`internal/sync/sync_test.go`**

```go
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
```

---

## Task 7: `login`, `logout`, and `sync` commands

**Files:** `internal/cli/login.go`, `internal/cli/logout.go`, `internal/cli/sync.go`, update `internal/cli/root.go` + tests

`sync` takes zero flags. Missing PAT → tells the user to run `wiki login`. Missing `wiki.yaml` → writes a template and exits with instructions. Rejected PAT (401/403) → same "run `wiki login`" hint, keyed off `azuredevops.ErrUnauthorized`.

The base URL for tests is injected via a package-level var `baseURLFn` rather than an env var, so the production binary has no hidden redirect hook.

```bash
go get golang.org/x/term
```

- [ ] **`internal/cli/login.go`**

When `--pat` isn't passed, the command reads from stdin. If stdin is a terminal we prompt and use `term.ReadPassword` (no echo, no shell-history leak). If stdin is a pipe we just read it. `readPasswordFn` is a package var so tests can stub it.

```go
package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/arjayads/wikivault/internal/credentials"
)

// readPasswordFn is swapped by tests; production calls term.ReadPassword on
// the stdin file descriptor.
var readPasswordFn = func() (string, error) {
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// isTerminalFn is swapped by tests to simulate tty vs pipe.
var isTerminalFn = func() bool { return term.IsTerminal(int(os.Stdin.Fd())) }

func newLoginCmd() *cobra.Command {
	var pat string
	c := &cobra.Command{
		Use:   "login",
		Short: "Store your Azure DevOps PAT in the OS keyring",
		Long: "Store your Azure DevOps PAT in the OS keyring. Provide it via " +
			"--pat, pipe it on stdin, or omit both to be prompted (preferred — " +
			"keeps the token out of shell history).",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if pat == "" {
				if isTerminalFn() {
					fmt.Fprint(cmd.ErrOrStderr(), "Azure DevOps PAT: ")
					got, err := readPasswordFn()
					fmt.Fprintln(cmd.ErrOrStderr())
					if err != nil {
						return err
					}
					pat = strings.TrimSpace(got)
				} else {
					data, err := io.ReadAll(cmd.InOrStdin())
					if err != nil {
						return err
					}
					pat = strings.TrimSpace(string(data))
				}
			}
			if pat == "" {
				return fmt.Errorf("PAT is empty — pass --pat <PAT>, pipe it via stdin, or enter it at the prompt")
			}
			if err := credentials.Save(pat); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "login: PAT stored in OS keyring.")
			return nil
		},
	}
	c.Flags().StringVar(&pat, "pat", "", "Azure DevOps PAT (prefer stdin/prompt to avoid shell history)")
	return c
}
```

- [ ] **`internal/cli/logout.go`**

```go
package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/arjayads/wikivault/internal/credentials"
)

func newLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove the stored Azure DevOps PAT from the OS keyring",
		RunE: func(cmd *cobra.Command, _ []string) error {
			err := credentials.Delete()
			if errors.Is(err, credentials.ErrNotFound) {
				fmt.Fprintln(cmd.OutOrStdout(), "logout: no PAT was stored.")
				return nil
			}
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "logout: PAT removed from OS keyring.")
			return nil
		},
	}
}
```

- [ ] **`internal/cli/sync.go`**

Values in the template are double-quoted so user-supplied names containing `:` or leading whitespace don't break YAML parsing. `baseURLFn` is a package-level seam so tests can point the client at `httptest.NewServer` — production code path always resolves through `azuredevops.DefaultBaseURL`.

```go
package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/arjayads/wikivault/internal/azuredevops"
	"github.com/arjayads/wikivault/internal/config"
	"github.com/arjayads/wikivault/internal/credentials"
	"github.com/arjayads/wikivault/internal/sync"
)

const wikiYamlTemplate = `organization: "your-azure-devops-organization"
project: "Your Project Name"
wiki: "Your Project.wiki"
`

// baseURLFn is the production resolver; tests swap it out.
var baseURLFn = azuredevops.DefaultBaseURL

func newSyncCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sync",
		Short: "Download the Azure DevOps wiki into the current directory",
		RunE: func(cmd *cobra.Command, _ []string) error {
			pat, err := credentials.Get()
			if errors.Is(err, credentials.ErrNotFound) {
				return fmt.Errorf("no PAT stored — run 'wiki login' first")
			}
			if err != nil {
				return err
			}

			cwd, err := os.Getwd()
			if err != nil {
				return err
			}

			cfg, err := config.Load(cwd)
			if errors.Is(err, config.ErrNotFound) {
				if writeErr := os.WriteFile(config.Path(cwd), []byte(wikiYamlTemplate), 0o644); writeErr != nil {
					return writeErr
				}
				return fmt.Errorf("no wiki.yaml found — wrote a template to %s, edit it and re-run 'wiki sync'", config.Path(cwd))
			}
			if err != nil {
				return err
			}
			if err := cfg.Validate(); err != nil {
				return fmt.Errorf("wiki.yaml: %w", err)
			}

			client := azuredevops.NewClient(baseURLFn(cfg.Organization), pat)

			res, err := sync.Run(cmd.Context(), sync.Options{
				Fetcher:   client,
				Project:   cfg.Project,
				Wiki:      cfg.Wiki,
				OutputDir: cwd,
			})
			if errors.Is(err, azuredevops.ErrUnauthorized) {
				return fmt.Errorf("Azure DevOps rejected the PAT — run 'wiki login' with a fresh token")
			}
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "synced %d pages to %s (pruned %d stale).\n",
				res.Written, cwd, res.Deleted)
			return nil
		},
	}
}
```

- [ ] **Register in `internal/cli/root.go`**

```go
func newRoot(version string) *cobra.Command {
	root := &cobra.Command{
		Use:           "wiki",
		Short:         "Sync an Azure DevOps wiki to your local filesystem",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newLoginCmd(), newLogoutCmd(), newSyncCmd())
	return root
}
```

- [ ] **`internal/cli/cli_test.go`**

```go
package cli

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/arjayads/wikivault/internal/config"
	"github.com/arjayads/wikivault/internal/credentials"
	"github.com/zalando/go-keyring"
)

func chdir(t *testing.T, dir string) {
	t.Helper()
	orig, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
}

// withBaseURL swaps the package-level base URL resolver for the test.
func withBaseURL(t *testing.T, url string) {
	t.Helper()
	orig := baseURLFn
	baseURLFn = func(string) string { return url }
	t.Cleanup(func() { baseURLFn = orig })
}

// withStdinPipe marks stdin as non-tty so login takes the pipe path.
func withStdinPipe(t *testing.T) {
	t.Helper()
	orig := isTerminalFn
	isTerminalFn = func() bool { return false }
	t.Cleanup(func() { isTerminalFn = orig })
}

func TestLoginSavesPAT(t *testing.T) {
	keyring.MockInit()
	cmd := newRoot("test")
	cmd.SetArgs([]string{"login", "--pat", "secret"})
	cmd.SetOut(&bytes.Buffer{})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if got, _ := credentials.Get(); got != "secret" {
		t.Fatalf("got %q", got)
	}
}

func TestLoginReadsPATFromStdinPipe(t *testing.T) {
	keyring.MockInit()
	withStdinPipe(t)
	cmd := newRoot("test")
	cmd.SetArgs([]string{"login"})
	cmd.SetIn(strings.NewReader("  stdin-pat  \n"))
	cmd.SetOut(&bytes.Buffer{})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if got, _ := credentials.Get(); got != "stdin-pat" {
		t.Fatalf("got %q, want stdin-pat", got)
	}
}

func TestLoginPromptsOnTTY(t *testing.T) {
	keyring.MockInit()
	origTTY, origRead := isTerminalFn, readPasswordFn
	isTerminalFn = func() bool { return true }
	readPasswordFn = func() (string, error) { return "typed-pat", nil }
	t.Cleanup(func() { isTerminalFn, readPasswordFn = origTTY, origRead })

	cmd := newRoot("test")
	cmd.SetArgs([]string{"login"})
	var errBuf bytes.Buffer
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&errBuf)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if got, _ := credentials.Get(); got != "typed-pat" {
		t.Fatalf("got %q, want typed-pat", got)
	}
	if !strings.Contains(errBuf.String(), "PAT") {
		t.Errorf("expected prompt on stderr, got %q", errBuf.String())
	}
}

func TestLogoutRemovesPAT(t *testing.T) {
	keyring.MockInit()
	_ = credentials.Save("secret")
	cmd := newRoot("test")
	cmd.SetArgs([]string{"logout"})
	cmd.SetOut(&bytes.Buffer{})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if _, err := credentials.Get(); err == nil {
		t.Fatal("PAT still present after logout")
	}
}

func TestLogoutNoPATIsNoop(t *testing.T) {
	keyring.MockInit()
	cmd := newRoot("test")
	cmd.SetArgs([]string{"logout"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("logout with no PAT returned error: %v", err)
	}
	if !strings.Contains(out.String(), "no PAT") {
		t.Errorf("expected 'no PAT' message, got %q", out.String())
	}
}

func TestSyncMissingPAT(t *testing.T) {
	keyring.MockInit()
	chdir(t, t.TempDir())
	cmd := newRoot("test")
	cmd.SetArgs([]string{"sync"})
	cmd.SetOut(&bytes.Buffer{})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "wiki login") {
		t.Fatalf("want 'wiki login' in error, got %v", err)
	}
}

func TestSyncMissingConfigWritesTemplate(t *testing.T) {
	keyring.MockInit()
	_ = credentials.Save("pat")
	dir := t.TempDir()
	chdir(t, dir)

	cmd := newRoot("test")
	cmd.SetArgs([]string{"sync"})
	cmd.SetOut(&bytes.Buffer{})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "wiki.yaml") {
		t.Fatalf("want wiki.yaml hint, got %v", err)
	}
	body, readErr := os.ReadFile(filepath.Join(dir, "wiki.yaml"))
	if readErr != nil {
		t.Fatalf("template not written: %v", readErr)
	}
	for _, want := range []string{"organization:", "project:", "wiki:"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("template missing %q", want)
		}
	}
}

func TestSyncWithTemplatePlaceholdersFails(t *testing.T) {
	keyring.MockInit()
	_ = credentials.Save("pat")
	dir := t.TempDir()
	chdir(t, dir)
	_ = config.Save(dir, &config.Config{
		Organization: "your-azure-devops-organization",
		Project:      "Your Project Name",
		Wiki:         "Your Project.wiki",
	})
	cmd := newRoot("test")
	cmd.SetArgs([]string{"sync"})
	cmd.SetOut(&bytes.Buffer{})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "placeholder") {
		t.Fatalf("want placeholder error, got %v", err)
	}
}

func TestSyncUnauthorizedHintsLogin(t *testing.T) {
	keyring.MockInit()
	_ = credentials.Save("pat")
	dir := t.TempDir()
	chdir(t, dir)
	_ = config.Save(dir, &config.Config{Organization: "contoso", Project: "Platform", Wiki: "Platform.wiki"})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	withBaseURL(t, srv.URL)

	cmd := newRoot("test")
	cmd.SetArgs([]string{"sync"})
	cmd.SetOut(&bytes.Buffer{})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "wiki login") {
		t.Fatalf("want 'wiki login' hint on 401, got %v", err)
	}
}

func TestSyncHappyPath(t *testing.T) {
	keyring.MockInit()
	_ = credentials.Save("pat")
	dir := t.TempDir()
	chdir(t, dir)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantSuffix := "/Platform/_apis/wiki/wikis/Platform.wiki/pages"
		if !strings.Contains(r.URL.Path, wantSuffix) {
			t.Errorf("path = %q, want suffix %q", r.URL.Path, wantSuffix)
		}
		w.Write([]byte(`{"id":1,"path":"/","content":"","subPages":[
			{"id":2,"path":"/Home","content":"# Home","subPages":[]}
		]}`))
	}))
	defer srv.Close()
	withBaseURL(t, srv.URL)

	_ = config.Save(dir, &config.Config{Organization: "contoso", Project: "Platform", Wiki: "Platform.wiki"})

	cmd := newRoot("test")
	cmd.SetArgs([]string{"sync"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v\nout=%s", err, out.String())
	}
	body, err := os.ReadFile(filepath.Join(dir, "Home.md"))
	if err != nil || string(body) != "# Home" {
		t.Fatalf("Home.md = %q err=%v", body, err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".wikisync.json")); err != nil {
		t.Fatalf(".wikisync.json missing: %v", err)
	}
}
```

```bash
go test ./...
```

---

## Task 8: GoReleaser + README

**Files:** `.goreleaser.yaml`, `README.md`

- [ ] **`.goreleaser.yaml`**

```yaml
version: 2

before:
  hooks:
    - go mod tidy

builds:
  - main: ./cmd/wiki
    binary: wiki
    env: [CGO_ENABLED=0]
    goos: [linux, darwin, windows]
    goarch: [amd64, arm64]
    ldflags:
      - -s -w -X main.version={{.Version}}

archives:
  - name_template: "wiki_{{ .Version }}_{{ .Os }}_{{ .Arch }}"
    format_overrides:
      - goos: windows
        formats: [zip]
    files:
      - README.md

checksum:
  name_template: "checksums.txt"

snapshot:
  version_template: "{{ incpatch .Version }}-next"
```

- [ ] **`README.md`**

```markdown
# wiki

A small cross-platform CLI that downloads an Azure DevOps wiki to the
local filesystem, preserving the page hierarchy (a page with children
becomes `Page.md` plus a sibling `Page/` directory).

## Install

Download a binary from the latest release and put `wiki` on your PATH, or:

    go install github.com/arjayads/wikivault/cmd/wiki@latest

## Usage

    wiki login           # prompts for the PAT (no echo, no shell history)
    # or: cat pat.txt | wiki login
    # or (discouraged, leaks to history/ps): wiki login --pat <PAT>

    cd /path/to/sync/into
    wiki sync            # first run writes a wiki.yaml template; edit it
    wiki sync            # second run reads wiki.yaml and pulls the pages

    wiki logout          # remove the stored PAT

The PAT is stored in the OS-native keyring (Windows Credential Manager,
macOS Keychain, Linux Secret Service). On headless Linux (CI runners,
containers, servers with no desktop session) the Secret Service isn't
available and `wiki login` will fail — run the CLI from a machine that
has one. Wiki coordinates live in `./wiki.yaml` in the directory you
sync into:

    organization: your-azure-devops-organization
    project: Your Project Name
    wiki: Your Project.wiki

`sync` fetches the whole wiki in one ADO API call and writes `.md` files
mirroring the page hierarchy. Pages removed from ADO since the last run
are pruned based on a local `.wikisync.json` manifest; user-authored
files the tool has never written are left alone. If you're syncing into
a git repo, add `.wikisync.json` to `.gitignore` — it's local
bookkeeping, not content.

## Known limitations

- Attachments (`.attachments/`) are not synced — embedded images render
  as broken links.
- Cross-page links like `[Setup](/Onboarding/Setup)` won't resolve when
  opening `.md` files directly.
- First sync into a non-empty directory will overwrite any pre-existing
  `.md` file whose name matches an ADO page.
- Local edits to synced files are lost on the next `sync` if the page is
  deleted upstream.
- No 429/5xx retry handling; very large wikis may hit Azure DevOps
  throttling or exceed the single-response budget.
- Default NTFS/APFS volumes are case-insensitive, so sibling wiki pages
  that differ only in case (e.g. `Setup` and `setup`) will overwrite
  each other.
- PAT scope required: `Wiki (read)`.
```

- [ ] **Verify cross-OS build**

```bash
make release-snapshot
```

Expected: `dist/` contains archives for 6 OS/arch combos plus `checksums.txt`.

---

## Task 9: Manual smoke test

Not automated — verifies the CLI against a live Azure DevOps wiki before shipping.

Windows users: substitute `./wiki.exe` for `./wiki` and run in Git Bash or WSL.

- [ ] Create a throwaway PAT (Azure DevOps → User settings → Personal access tokens, scope `Wiki (read)`).
- [ ] `./wiki login` → enter PAT at prompt → `login: PAT stored in OS keyring.`
- [ ] `mkdir /tmp/wiki-smoke && cd /tmp/wiki-smoke && wiki sync` → writes `wiki.yaml` template and exits. Edit the file with real values.
- [ ] `wiki sync` → `synced N pages to /tmp/wiki-smoke (pruned 0 stale).` Spot-check the `.md` files.
- [ ] Re-run `wiki sync` → same counts, no spurious changes.
- [ ] Revoke the PAT in Azure DevOps, run `wiki sync` → "Azure DevOps rejected the PAT — run 'wiki login' with a fresh token".
- [ ] `./wiki logout` → `logout: PAT removed from OS keyring.`
- [ ] Delete the throwaway PAT in Azure DevOps.
