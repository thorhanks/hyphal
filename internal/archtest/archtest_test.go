package archtest

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"testing/fstest"
)

const module = "github.com/thorhanks/hyphal"

// moduleRoot returns the directory holding go.mod.
func moduleRoot(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}").Output()
	if err != nil {
		t.Fatalf("go list -m: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func listPackages(t *testing.T, root string) []Package {
	t.Helper()
	cmd := exec.Command("go", "list", "-json=ImportPath,Imports", "./...")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	var pkgs []Package
	dec := json.NewDecoder(bytes.NewReader(out))
	for {
		var p Package
		if err := dec.Decode(&p); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatalf("decode go list output: %v", err)
		}
		pkgs = append(pkgs, p)
	}
	return pkgs
}

func TestImportRules(t *testing.T) {
	pkgs := listPackages(t, moduleRoot(t))
	if len(pkgs) == 0 {
		t.Fatal("go list returned no packages")
	}
	for _, v := range CheckImports(module, pkgs) {
		t.Error(v)
	}
}

func TestImportRules_DetectsViolation(t *testing.T) {
	m := func(rel string) string { return module + "/" + rel }
	cases := []struct {
		name    string
		pkg     Package
		wantBad string // the import expected to be reported; "" for none
	}{
		{"store imports core", Package{m("internal/store"), []string{m("internal/core")}}, m("internal/core")},
		{"store imports model", Package{m("internal/store"), []string{m("internal/model"), "fmt"}}, ""},
		{"leaf imports unlisted leaf", Package{m("internal/markup"), []string{m("internal/opml")}}, m("internal/opml")},
		{"leaf edge", Package{m("internal/opml"), []string{m("internal/markup"), m("internal/model")}}, ""},
		{"server imports database/sql", Package{m("internal/server"), []string{"database/sql"}}, "database/sql"},
		{"web imports core", Package{m("internal/web"), []string{m("internal/core")}}, m("internal/core")},
		{"removed terminal UI module", Package{m("internal/cli"), []string{"charm.land/bubbletea/v2"}}, "charm.land/bubbletea/v2"},
		{"cli imports the sqlite driver", Package{m("internal/cli"), []string{"modernc.org/sqlite"}}, "modernc.org/sqlite"},
		{"store imports the sqlite driver", Package{m("internal/store"), []string{"database/sql", "modernc.org/sqlite"}}, ""},
		{"unapproved module", Package{m("internal/core"), []string{"github.com/stretchr/testify/assert"}}, "github.com/stretchr/testify/assert"},
		{"approved module subpackage", Package{m("internal/textfold"), []string{"golang.org/x/text/cases"}}, ""},
		{"production code imports testgen", Package{m("internal/core"), []string{m("internal/testgen")}}, m("internal/testgen")},
		{"cmd imports store", Package{m("cmd/hyphal"), []string{m("internal/store")}}, m("internal/store")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := CheckImports(module, []Package{c.pkg})
			switch {
			case c.wantBad == "" && len(got) > 0:
				t.Errorf("unexpected violations: %v", got)
			case c.wantBad != "" && (len(got) != 1 || got[0].To != c.wantBad):
				t.Errorf("got %v, want one violation for %s", got, c.wantBad)
			}
		})
	}

	t.Run("unknown package", func(t *testing.T) {
		got := CheckImports(module, []Package{{ImportPath: m("internal/misc")}})
		if len(got) != 1 || got[0].From != m("internal/misc") {
			t.Errorf("got %v, want one violation for internal/misc", got)
		}
	})
}

func TestAppNamePlacement(t *testing.T) {
	found, err := ScanAppName(os.DirFS(moduleRoot(t)), "internal/store", "internal/model")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range found {
		t.Errorf("%s:%d: app name outside plan/CONTRACTS.md §1: %s", f.File, f.Line, f.Text)
	}
}

func TestAppNamePlacement_DetectsViolation(t *testing.T) {
	file := func(src string) *fstest.MapFile { return &fstest.MapFile{Data: []byte(src)} }
	cases := []struct {
		name string
		path string
		src  string
		want int
	}{
		{"meta key literal", "internal/store/meta.go", "package store\n\nconst k = \"hyphal_version\"\n", 1},
		{"any case", "internal/model/m.go", "package model\n\nvar s = `Hyphal`\n", 1},
		{"json field tag", "internal/model/m.go", "package model\n\ntype T struct {\n\tA int `json:\"hyphal_a\"`\n}\n", 1},
		{"allowed with marker", "internal/store/export.go", "package store\n\nconst f = \"hyphal-export\" // " + AllowMarker + " (CONTRACTS §1)\n", 0},
		{"comment only", "internal/store/s.go", "package store\n\n// hyphal is fine in comments.\nconst k = \"x\"\n", 0},
		{"test file", "internal/store/s_test.go", "package store\n\nconst k = \"hyphal\"\n", 0},
		{"import path", "internal/store/s.go", "package store\n\nimport _ \"example.com/hyphal/model\"\n", 0},
		{"sql", "internal/store/schema/0001_init.sql", "CREATE TABLE hyphal_meta (k TEXT);\n", 1},
		{"testdata skipped", "internal/store/testdata/export.json", "{\"format\": \"hyphal-export\"}\n", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fsys := fstest.MapFS{c.path: file(c.src)}
			found, err := ScanAppName(fsys, "internal")
			if err != nil {
				t.Fatal(err)
			}
			if len(found) != c.want {
				t.Errorf("got %d findings %v, want %d", len(found), found, c.want)
			}
		})
	}
}
