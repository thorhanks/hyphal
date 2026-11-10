package web

import (
	"io/fs"
	"os"
	"path"
	"regexp"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

// The asset checks of TESTING §6. Each runs on the real assets and on a
// crafted violation that must fail.

// allowedURLs are external links that may appear outside comments: docs links.
var allowedURLs = []string{}

var (
	blockComment = regexp.MustCompile(`(?s)/\*.*?\*/`)
	lineComment  = regexp.MustCompile(`(?m)(^|\s)//.*$`)
	htmlComment  = regexp.MustCompile(`(?s)<!--.*?-->`)
	externalURL  = regexp.MustCompile(`https?://[^\s"'` + "`" + `)]*`)

	jsImport   = regexp.MustCompile(`(?m)(?:^\s*import\s+(?:[^"'` + "`" + `;]*?\s+from\s+)?|^\s*export\s+[^"'` + "`" + `;]*?\s+from\s+|\bimport\s*\(\s*)["'` + "`" + `]([^"'` + "`" + `]+)["'` + "`" + `]`)
	htmlRef    = regexp.MustCompile(`\b(?:src|href)\s*=\s*["']([^"']+)["']`)
	cssURL     = regexp.MustCompile(`(?:@import\s+|url\(\s*)["']?([^"')\s]+)`)
	hexColor   = regexp.MustCompile(`#[0-9a-fA-F]{3,8}\b`)
	funcColor  = regexp.MustCompile(`\b(?:rgba?|hsla?|hwb|lab|lch|oklab|oklch|color)\(`)
	declRe     = regexp.MustCompile(`([\w-]+)\s*:\s*([^;{}]+)[;}]`)
	namedColor = regexp.MustCompile(`\b(?:black|white|red|green|blue|gray|grey|yellow|orange|purple|pink|brown|cyan|magenta)\b`)
	rawPx      = regexp.MustCompile(`(?:^|[^\w.-])(\d*\.?\d+)px\b`)
	tokenDef   = regexp.MustCompile(`(--[\w-]+)\s*:`)
	tokenUse   = regexp.MustCompile(`var\(\s*(--[\w-]+)`)
	htmlSink   = regexp.MustCompile(`\b(?:innerHTML|outerHTML|insertAdjacentHTML|document\.write)\b`)
)

func stripComments(name, src string) string {
	switch path.Ext(name) {
	case ".html":
		return htmlComment.ReplaceAllString(src, "")
	case ".js", ".mjs":
		return lineComment.ReplaceAllString(blockComment.ReplaceAllString(src, ""), "$1")
	case ".css":
		return blockComment.ReplaceAllString(src, "")
	}
	return src
}

// textAssets returns every text asset, comments removed, by path.
func textAssets(t *testing.T, fsys fs.FS) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.HasPrefix(p, "vendor/") {
			return err
		}
		switch path.Ext(p) {
		case ".html", ".css", ".js", ".mjs":
			b, err := fs.ReadFile(fsys, p)
			if err != nil {
				return err
			}
			out[p] = stripComments(p, string(b))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func checkNoExternalURLs(t *testing.T, fsys fs.FS) []string {
	var bad []string
	for name, src := range textAssets(t, fsys) {
		for _, u := range externalURL.FindAllString(src, -1) {
			if !slices.Contains(allowedURLs, u) {
				bad = append(bad, name+": "+u)
			}
		}
	}
	return bad
}

// resolve joins a relative reference to the file it appears in. The page is
// served at "/", so index.html resolves against the asset root's parent.
func resolve(from, ref string) (string, bool) {
	ref, _, _ = strings.Cut(ref, "?")
	ref, _, _ = strings.Cut(ref, "#")
	if ref == "" || strings.HasPrefix(ref, "/") || strings.Contains(ref, ":") {
		return "", false
	}
	if from == "index.html" {
		p, ok := strings.CutPrefix(path.Clean(ref), "assets/")
		return p, ok
	}
	if !strings.HasPrefix(ref, "./") && !strings.HasPrefix(ref, "../") && !strings.HasSuffix(from, ".css") {
		return "", false // a bare module specifier
	}
	p := path.Join(path.Dir(from), ref)
	return p, !strings.HasPrefix(p, "../")
}

func checkImportsRelative(t *testing.T, fsys fs.FS) []string {
	var bad []string
	for name, src := range textAssets(t, fsys) {
		var refs []string
		var re *regexp.Regexp
		switch path.Ext(name) {
		case ".js", ".mjs":
			re = jsImport
		case ".html":
			re = htmlRef
		case ".css":
			re = cssURL
		}
		for _, m := range re.FindAllStringSubmatch(src, -1) {
			refs = append(refs, m[1])
		}
		for _, ref := range refs {
			if strings.HasPrefix(ref, "data:") {
				continue
			}
			p, ok := resolve(name, ref)
			if !ok {
				bad = append(bad, name+": not relative: "+ref)
				continue
			}
			if _, err := fs.Stat(fsys, p); err != nil {
				bad = append(bad, name+": doesn't resolve: "+ref)
			}
		}
	}
	return bad
}

// themeFile is where raw values belong (§4.4).
func themeFile(name string) bool {
	return name == "tokens.css" || strings.HasPrefix(name, "themes/")
}

func checkTokensOnly(t *testing.T, fsys fs.FS) []string {
	var bad []string
	for name, src := range textAssets(t, fsys) {
		if path.Ext(name) != ".css" || themeFile(name) {
			continue
		}
		for _, m := range declRe.FindAllStringSubmatch(src, -1) {
			prop, value := m[1], strings.TrimSpace(m[2])
			where := name + ": " + prop + ": " + value
			if hexColor.MatchString(value) || funcColor.MatchString(value) || namedColor.MatchString(value) {
				bad = append(bad, where+" (color literal)")
			}
			if prop == "font" {
				bad = append(bad, where+" (use the longhands with tokens)")
			}
			if prop == "font-family" && !regexp.MustCompile(`^var\(--[\w-]+\)$`).MatchString(value) {
				bad = append(bad, where+" (font-family value)")
			}
			for _, px := range rawPx.FindAllStringSubmatch(value, -1) {
				if px[1] != "0" && px[1] != "1" {
					bad = append(bad, where+" (raw px)")
				}
			}
		}
	}
	return bad
}

func checkTokensUsed(t *testing.T, fsys fs.FS) []string {
	all := textAssets(t, fsys)
	defined := map[string]bool{}
	for name, src := range all {
		if path.Ext(name) == ".css" {
			for _, m := range tokenDef.FindAllStringSubmatch(src, -1) {
				defined[m[1]] = true
			}
		}
	}
	used := map[string]bool{}
	for _, src := range all {
		for _, m := range tokenUse.FindAllStringSubmatch(src, -1) {
			used[m[1]] = true
		}
	}
	var bad []string
	for _, m := range tokenDef.FindAllStringSubmatch(all["tokens.css"], -1) {
		if !used[m[1]] {
			bad = append(bad, "tokens.css: "+m[1]+" is never used")
		}
	}
	for tok := range used {
		if !defined[tok] {
			bad = append(bad, tok+" is used but never defined")
		}
	}
	slices.Sort(bad)
	return slices.Compact(bad)
}

func checkNoInnerHTML(t *testing.T, fsys fs.FS) []string {
	var bad []string
	for name, src := range textAssets(t, fsys) {
		if path.Ext(name) == ".js" || path.Ext(name) == ".mjs" || path.Ext(name) == ".html" {
			for _, m := range htmlSink.FindAllString(src, -1) {
				bad = append(bad, name+": "+m)
			}
		}
	}
	return bad
}

// checkBoth runs a check on the real assets, which must pass, and on a
// crafted violation, which must fail (AC7).
func checkBoth(t *testing.T, check func(*testing.T, fs.FS) []string, violation fstest.MapFS) {
	t.Helper()
	if bad := check(t, os.DirFS("assets")); len(bad) > 0 {
		t.Errorf("assets fail:\n%s", strings.Join(bad, "\n"))
	}
	if bad := check(t, violation); len(bad) == 0 {
		t.Error("the crafted violation passed")
	}
}

func file(s string) *fstest.MapFile { return &fstest.MapFile{Data: []byte(s)} }

func TestWebAssets_NoExternalURLs(t *testing.T) {
	checkBoth(t, checkNoExternalURLs, fstest.MapFS{
		"lib/a.js": file("// https://example.com is fine in a comment\nfetch(\"https://example.com/x\");\n"),
	})
}

func TestWebAssets_ImportsRelative(t *testing.T) {
	checkBoth(t, checkImportsRelative, fstest.MapFS{"lib/a.js": file(`import { x } from "lit";`)})
	checkBoth(t, checkImportsRelative, fstest.MapFS{"lib/a.js": file(`import "./missing.js";`)})
	checkBoth(t, checkImportsRelative, fstest.MapFS{"lib/a.js": file(`const m = await import("/abs.js");`)})
	checkBoth(t, checkImportsRelative, fstest.MapFS{"index.html": file(`<script src="https://cdn.example/x.js"></script>`)})
}

func TestWebAssets_TokensOnly(t *testing.T) {
	for _, css := range []string{
		"a { color: #fff; }",
		"a { background: rgb(0 0 0); }",
		"a { color: white; }",
		"a { font-family: Georgia; }",
		"a { font: 12px/1 var(--font-text); }",
		"a { padding: 3px; }",
	} {
		checkBoth(t, checkTokensOnly, fstest.MapFS{"components/a.css": file(css)})
	}
	// Raw values are fine in tokens.css and themes; 0 and 1px anywhere.
	if bad := checkTokensOnly(t, fstest.MapFS{
		"tokens.css":       file(":root { --x: #fff; --y: 3px; }"),
		"themes/a.css":     file(":root { --x: red; }"),
		"components/b.css": file("a { border: 1px solid var(--x); margin: 0px; }"),
	}); len(bad) > 0 {
		t.Errorf("allowed values fail: %v", bad)
	}
}

func TestWebAssets_TokensUsed(t *testing.T) {
	checkBoth(t, checkTokensUsed, fstest.MapFS{
		"tokens.css": file(":root { --used: 1; --unused: 2; }"),
		"a.css":      file("a { order: var(--used); }"),
	})
	checkBoth(t, checkTokensUsed, fstest.MapFS{
		"tokens.css": file(":root { --used: 1; }"),
		"a.css":      file("a { order: var(--used); width: var(--nowhere); }"),
	})
}

func TestWebAssets_NoInnerHTML(t *testing.T) {
	checkBoth(t, checkNoInnerHTML, fstest.MapFS{"components/a.js": file("el.innerHTML = row.text;")})
	if bad := checkNoInnerHTML(t, fstest.MapFS{"vendor/cm.js": file("x.innerHTML = y;")}); len(bad) > 0 {
		t.Errorf("vendor files are exempt: %v", bad)
	}
}

// Release builds leave out the dev hooks (TESTING §5).
func TestAssets_ReleaseLeavesOutDev(t *testing.T) {
	if _, err := fs.Stat(Assets(), "index.html"); err != nil {
		t.Fatal(err)
	}
	_, err := fs.Stat(Assets(), "dev/hooks.js")
	if devEmbedded != (err == nil) {
		t.Errorf("dev/hooks.js embedded = %v, want %v", err == nil, devEmbedded)
	}
}
