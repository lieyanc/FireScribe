package api

import (
	"embed"
	"io/fs"
)

//go:embed all:webdist
var embeddedWebDist embed.FS

func embeddedStaticFS() (fs.FS, bool) {
	sub, err := fs.Sub(embeddedWebDist, "webdist")
	if err != nil {
		return nil, false
	}
	if info, err := fs.Stat(sub, "index.html"); err != nil || info.IsDir() {
		return nil, false
	}
	return sub, true
}
