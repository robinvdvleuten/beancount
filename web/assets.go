//go:build !dev

package web

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/olivere/vite"
)

//go:embed all:dist
var dist embed.FS

// getDistFS returns the embedded dist filesystem for production mode.
func (s *Server) getDistFS() (fs.FS, error) {
	return fs.Sub(dist, "dist")
}

// getViteFragment returns a Vite HTML fragment for production mode.
// In prod mode, vite reads the manifest to inject hashed asset paths.
func (s *Server) getViteFragment(distFS fs.FS) (*vite.Fragment, error) {
	return vite.HTMLFragment(vite.Config{
		FS:    distFS,
		IsDev: false,
	})
}

// createAssetsHandler creates the handler for serving static assets.
// In prod mode, serves embedded assets from the dist directory.
func (s *Server) createAssetsHandler(distFS fs.FS) http.Handler {
	return http.FileServerFS(distFS)
}
