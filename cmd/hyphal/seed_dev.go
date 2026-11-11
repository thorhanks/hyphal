//go:build dev

package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/thorhanks/hyphal/internal/testgen"
)

// seedFlag adds --seed FILE: a fixture in the test notation (CONTRACTS §11),
// or a JSON export when the file starts with "{". Dev builds only, so the
// release binary never links testgen (A-013).
func seedFlag(fs *flag.FlagSet) func() (io.Reader, error) {
	path := fs.String("seed", "", "replace the outline with this fixture or JSON export")
	return func() (io.Reader, error) {
		if *path == "" {
			return nil, nil
		}
		b, err := os.ReadFile(*path)
		if err != nil {
			return nil, err
		}
		if bytes.HasPrefix(bytes.TrimSpace(b), []byte("{")) {
			return bytes.NewReader(b), nil
		}
		f, err := testgen.ParseOutline(string(bytes.TrimRight(b, "\n")))
		if err != nil {
			return nil, fmt.Errorf("seed %s: %w", *path, err)
		}
		out, err := testgen.ExportJSON(f.Dump)
		if err != nil {
			return nil, err
		}
		return bytes.NewReader(out), nil
	}
}

// platformFlag adds --platform: resolve the keymap's Mod for linux or darwin
// rather than this machine, so browser tests can check macOS keys (P0-19).
func platformFlag(fs *flag.FlagSet) *string {
	return fs.String("platform", "", "resolve key bindings for linux or darwin")
}
