//go:build dev

package web

import "net/http"

// mountAssets does nothing in dev mode - Vite dev server handles all assets.
// Go only serves /api/* routes, returns 404 for everything else.
func (s *Server) mountAssets(mux *http.ServeMux) {
	// Intentionally empty - Vite serves / and /assets/*
	// Users should access the app via http://localhost:5173 (Vite)
	// API requests from Vite get proxied to Go on :8080
}
