//go:build dev

package web

import (
	"io/fs"
	"net/http"
	"os"

	"github.com/olivere/vite"
)

// getDistFS returns the assets directory filesystem for development mode.
func (s *Server) getDistFS() (fs.FS, error) {
	return os.DirFS("./assets"), nil
}

// getViteFragment returns a Vite HTML fragment for development mode.
// In dev mode, vite points to the dev server for hot reloading.
func (s *Server) getViteFragment(distFS fs.FS) (*vite.Fragment, error) {
	return vite.HTMLFragment(vite.Config{
		FS:      distFS,
		IsDev:   true,
		ViteURL: "http://localhost:5173",
	})
}

// createAssetsHandler creates the handler for serving static assets.
// In dev mode, serves assets from the assets directory.
func (s *Server) createAssetsHandler(distFS fs.FS) http.Handler {
	return http.FileServerFS(distFS)
}
