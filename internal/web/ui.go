package web

import (
	"embed"
	"io/fs"
)

//go:embed static
var embeddedStatic embed.FS

// staticFiles is the sub-filesystem rooted at "static/", served at /static/.
var staticFiles, _ = fs.Sub(embeddedStatic, "static")
