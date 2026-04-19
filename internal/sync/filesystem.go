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
