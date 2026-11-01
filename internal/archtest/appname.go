package archtest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path"
	"strings"
)

// AllowMarker on the same line as a Go string literal permits the app name in
// it. Use it only for the places plan/CONTRACTS.md §1 lists, such as the JSON
// export header.
const AllowMarker = "archtest:allow-app-name"

// Finding is one occurrence of the app name where it is not allowed.
type Finding struct {
	File string
	Line int
	Text string
}

// ScanAppName looks for "hyphal" (any case) in the string literals and struct
// tags of non-test Go files, and anywhere in .sql and .json files, under roots
// in fsys. testdata directories are skipped: fixtures hold real exports, whose
// header carries the app name (plan/CONTRACTS.md §1).
func ScanAppName(fsys fs.FS, roots ...string) ([]Finding, error) {
	var out []Finding
	for _, root := range roots {
		err := fs.WalkDir(fsys, root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == "testdata" {
					return fs.SkipDir
				}
				return nil
			}
			var found []Finding
			switch ext := path.Ext(p); {
			case ext == ".go" && !strings.HasSuffix(p, "_test.go"):
				found, err = scanGo(fsys, p)
			case ext == ".sql" || ext == ".json":
				found, err = scanText(fsys, p)
			}
			out = append(out, found...)
			return err
		})
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func containsAppName(s string) bool {
	return strings.Contains(strings.ToLower(s), "hyphal")
}

func scanGo(fsys fs.FS, p string) ([]Finding, error) {
	src, err := fs.ReadFile(fsys, p)
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, p, src, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	allowed := map[int]bool{}
	for _, cg := range f.Comments {
		for _, c := range cg.List {
			if strings.Contains(c.Text, AllowMarker) {
				allowed[fset.Position(c.Pos()).Line] = true
			}
		}
	}
	var out []Finding
	ast.Inspect(f, func(n ast.Node) bool {
		if _, isImport := n.(*ast.ImportSpec); isImport {
			return false // the module path is not a stored name
		}
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING || !containsAppName(lit.Value) {
			return true
		}
		if line := fset.Position(lit.Pos()).Line; !allowed[line] {
			out = append(out, Finding{p, line, lit.Value})
		}
		return true
	})
	return out, nil
}

func scanText(fsys fs.FS, p string) ([]Finding, error) {
	src, err := fs.ReadFile(fsys, p)
	if err != nil {
		return nil, err
	}
	var out []Finding
	for i, line := range strings.Split(string(src), "\n") {
		if containsAppName(line) {
			out = append(out, Finding{p, i + 1, line})
		}
	}
	return out, nil
}
