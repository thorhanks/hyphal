//go:build dev

package web

import "embed"

// The dev build also serves assets/dev: window.__hyphal and, from P0-19, the
// key probe (TESTING §5).
//
//go:embed assets
var files embed.FS
