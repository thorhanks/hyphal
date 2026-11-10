// Package web holds the browser client: native web components, ES modules and
// plain CSS, embedded and served as written (§9.1, ARCHITECTURE §5.1, D-039).
// Only the server imports it.
package web

import "io/fs"

// Assets returns the embedded client, rooted at the assets directory. Release
// builds leave out assets/dev (TESTING §5).
func Assets() fs.FS {
	sub, err := fs.Sub(files, "assets")
	if err != nil {
		panic(err) // the embed pattern names the directory
	}
	return sub
}
