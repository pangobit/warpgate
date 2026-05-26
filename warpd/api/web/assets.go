package web

import (
	"embed"
	"net/http"

	"github.com/a-h/templ"
)

//go:embed assets
var assetFiles embed.FS

// Assets serves embedded JavaScript and stylesheet files.
type Assets struct {
	handler http.Handler
}

// NewAssets creates the static asset handler.
func NewAssets() *Assets {
	mux := http.NewServeMux()
	mux.Handle("/assets/css/components.css", templ.NewCSSHandler(ComponentStyles()...))
	mux.Handle("/assets/", http.FileServer(http.FS(assetFiles)))
	return &Assets{handler: mux}
}

// ServeHTTP serves embedded assets.
func (a *Assets) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.handler.ServeHTTP(w, r)
}
