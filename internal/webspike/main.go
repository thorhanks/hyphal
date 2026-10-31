package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
)

func main() {
	home, _ := os.UserHomeDir()
	seed := flag.String("seed", "", "fixture file (see parseSeed)")
	gen := flag.Int("gen", 0, "generate this many nodes")
	expanded := flag.Int("expanded", 10000, "visible rows when generating")
	port := flag.Int("port", 0, "loopback port; 0 picks one")
	colors := flag.String("colors", filepath.Join(home, ".local/state/omarchy/current/theme/colors.toml"), "theme file")
	editor := flag.String("editor-cmd", `xdg-terminal-exec --app-id=hyphal-edit -- "${EDITOR:-nvim}" "$1"`, "shell command run with the temp file as $1")
	flag.Parse()

	var t *Tree
	switch {
	case *seed != "":
		b, err := os.ReadFile(*seed)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		t = parseSeed(string(b))
	case *gen > 0:
		t = generate(*gen, *expanded)
	default:
		t = parseSeed("a||Welcome to the spike\n  b||Enter adds a bullet\n  c||Tab nests it\nd||Esc selects\n")
	}
	s, l, err := NewServer(t, *port, *colors, *editor)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	go s.WatchTheme(context.Background())
	fmt.Println("URL", s.URL())
	if err := http.Serve(l, s.Handler()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
