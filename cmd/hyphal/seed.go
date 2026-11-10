//go:build !dev

package main

import (
	"flag"
	"io"
)

// seedFlag exists only in dev builds (TESTING §5).
func seedFlag(*flag.FlagSet) func() (io.Reader, error) { return nil }
