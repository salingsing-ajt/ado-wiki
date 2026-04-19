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
		`a:b*c?d"e<f>g|h\i`:  "a_b_c_d_e_f_g_h_i",
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
