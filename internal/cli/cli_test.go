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
	outDir := filepath.Join(dir, "articles", "Platform.wiki")
	body, err := os.ReadFile(filepath.Join(outDir, "Home.md"))
	if err != nil || string(body) != "# Home" {
		t.Fatalf("Home.md = %q err=%v", body, err)
	}
	if _, err := os.Stat(filepath.Join(outDir, ".wikisync.json")); err != nil {
		t.Fatalf(".wikisync.json missing: %v", err)
	}
}
