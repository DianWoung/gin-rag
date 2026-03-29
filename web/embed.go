package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed index.html styles.css app.js
var staticFiles embed.FS

func FileSystem() http.FileSystem {
	sub, err := fs.Sub(staticFiles, ".")
	if err != nil {
		panic(err)
	}

	return http.FS(sub)
}
