// Package dashboard provides the web dashboard HTML embedded into the binary.
package dashboard

import (
	_ "embed"
	"io"
	"net/http"
	"os"
)

//go:embed index.html
var embeddedHTML string

// indexHTML returns the dashboard HTML, preferring the on-disk file for dev mode.
func indexHTML() string {
	if _, err := os.Stat("web/index.html"); err == nil {
		data, err := os.ReadFile("web/index.html")
		if err == nil {
			return string(data)
		}
	}
	return embeddedHTML
}

// serveIndex serves the dashboard HTML with proper content type.
func serveIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	io.WriteString(w, indexHTML())
}
