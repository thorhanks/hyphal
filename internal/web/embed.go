//go:build !dev

package web

import "embed"

//go:embed assets/index.html assets/*.css assets/components assets/lib
var files embed.FS
