package archtest

import (
	"fmt"
	"slices"
	"strings"
)

// Package is one package and the imports of its production (non-test) files.
type Package struct {
	ImportPath string
	Imports    []string
}

// Violation is one import that breaks a rule.
type Violation struct {
	From, To, Reason string
}

func (v Violation) String() string {
	return fmt.Sprintf("%s imports %s: %s", v.From, v.To, v.Reason)
}

// leaves are the dependency-free leaf packages (plan/ARCHITECTURE.md §1, D-001).
var leaves = []string{
	"internal/model", "internal/ids", "internal/fracindex", "internal/textfold",
	"internal/markup", "internal/dialect", "internal/opml", "internal/layout",
	"internal/keymap", "internal/config", "internal/clipboard", "internal/platform",
}

// leafEdges are the only imports allowed between leaves besides model.
var leafEdges = map[string][]string{
	"internal/markup":  {"internal/textfold"},
	"internal/dialect": {"internal/markup", "internal/ids"},
	"internal/opml":    {"internal/markup"},
	"internal/layout":  {"internal/markup"},
}

// approvedModules are the third-party modules of plan/ARCHITECTURE.md §3.
// Adding one needs the human's approval (plan/README.md §4).
var approvedModules = []string{
	"charm.land/bubbletea/v2",
	"charm.land/lipgloss/v2",
	"charm.land/bubbles/v2",
	"github.com/charmbracelet/x/exp/teatest/v2",
	"github.com/spf13/cobra",
	"modernc.org/sqlite",
	"github.com/ncruces/go-sqlite3",
	"github.com/rivo/uniseg",
	"golang.org/x/text",
	"golang.org/x/sys",
	"github.com/sahilm/fuzzy",
	"github.com/pelletier/go-toml/v2",
	"pgregory.net/rapid",
}

// sqlImports may only be imported by the store layer (§9.2).
var sqlImports = []string{"database/sql", "modernc.org/sqlite", "github.com/ncruces/go-sqlite3"}

// allowedInternal returns the module-relative packages that rel may import,
// and false if rel is not a package named in plan/ARCHITECTURE.md §1.
func allowedInternal(rel string) ([]string, bool) {
	withLeaves := func(pkgs ...string) []string { return append(pkgs, leaves...) }
	tuiLayer := withLeaves("internal/core", "internal/store", "internal/tui/theme", "internal/tui/pageview")
	switch {
	case slices.Contains(leaves, rel):
		return append([]string{"internal/model"}, leafEdges[rel]...), true
	case rel == "internal/store/crashtest" || strings.HasPrefix(rel, "internal/store/crashtest/"):
		// Test support: drives core against a real store (plan/TESTING.md §5).
		return withLeaves("internal/core", "internal/store", "internal/testgen"), true
	case rel == "internal/store" || strings.HasPrefix(rel, "internal/store/"):
		return []string{"internal/model", "internal/ids", "internal/fracindex", "internal/platform", "internal/store"}, true
	case rel == "internal/core":
		return withLeaves("internal/store"), true
	case rel == "internal/cli":
		return withLeaves("internal/core", "internal/store"), true
	case rel == "internal/tui", rel == "internal/tui/theme", rel == "internal/tui/pageview":
		return tuiLayer, true
	case rel == "internal/tui/tuitest":
		return append(tuiLayer, "internal/tui", "internal/testgen"), true
	case rel == "internal/testgen":
		return withLeaves("internal/core", "internal/store"), true
	case rel == "internal/archtest":
		return nil, true
	case rel == "cmd/hyphal":
		return []string{"internal/cli", "internal/tui"}, true
	case rel == "cmd/hyphal-keyprobe":
		return []string{"internal/keymap"}, true
	}
	return nil, false
}

// CheckImports applies the import rules of plan/ARCHITECTURE.md §2 and the
// dependency list of §3 to the packages of module.
func CheckImports(module string, pkgs []Package) []Violation {
	var out []Violation
	for _, p := range pkgs {
		rel, ok := relPath(module, p.ImportPath)
		if !ok {
			continue
		}
		allowed, known := allowedInternal(rel)
		if !known {
			out = append(out, Violation{p.ImportPath, "", "package is not in plan/ARCHITECTURE.md §1"})
			continue
		}
		for _, imp := range p.Imports {
			if v, bad := checkImport(module, rel, allowed, imp); bad {
				v.From = p.ImportPath
				out = append(out, v)
			}
		}
	}
	return out
}

func checkImport(module, rel string, allowed []string, imp string) (Violation, bool) {
	if impRel, ok := relPath(module, imp); ok {
		if !slices.Contains(allowed, impRel) {
			return Violation{To: imp, Reason: "not allowed by plan/ARCHITECTURE.md §2"}, true
		}
		return Violation{}, false
	}
	if slices.ContainsFunc(sqlImports, func(m string) bool { return within(imp, m) }) &&
		rel != "internal/store" && !strings.HasPrefix(rel, "internal/store/") {
		return Violation{To: imp, Reason: "only store touches the database (§9.2)"}, true
	}
	if isStdlib(imp) {
		return Violation{}, false
	}
	if !slices.ContainsFunc(approvedModules, func(m string) bool { return within(imp, m) }) {
		return Violation{To: imp, Reason: "module is not approved in plan/ARCHITECTURE.md §3"}, true
	}
	return Violation{}, false
}

// relPath returns path relative to module, and false for paths outside it.
func relPath(module, path string) (string, bool) {
	rest, ok := strings.CutPrefix(path, module+"/")
	return rest, ok
}

// within reports whether path is module or a package inside it.
func within(path, module string) bool {
	return path == module || strings.HasPrefix(path, module+"/")
}

// isStdlib reports whether path is a standard-library package: its first
// element has no dot.
func isStdlib(path string) bool {
	first, _, _ := strings.Cut(path, "/")
	return !strings.Contains(first, ".")
}
