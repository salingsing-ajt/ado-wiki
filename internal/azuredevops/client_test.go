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
