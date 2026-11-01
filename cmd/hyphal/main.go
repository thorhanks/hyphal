// Command hyphal is the local-first outliner (§1). Until the CLI lands in P1-25
// it only reports its version.
package main

import (
	"fmt"
	"os"
)

// version is set at release time with -ldflags "-X main.version=…".
var version = "dev"

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Println("hyphal " + version)
		return
	}
	fmt.Fprintln(os.Stderr, "usage: hyphal --version")
	os.Exit(2)
}
