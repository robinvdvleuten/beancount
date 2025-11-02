// Package web provides an HTTP server for the Beancount web editor.
//
// The server exposes a REST API for reading and writing Beancount files,
// with real-time validation and error reporting. It also serves the
// web-based editor frontend as static files.
//
// SECURITY WARNING: This server has no authentication and should only be
// bound to localhost (127.0.0.1). Do not expose it to untrusted networks.
// File access is restricted to the directory containing the configured
// ledger file.
package web

import (
	"context"
	_ "embed"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"

	"github.com/robinvdvleuten/beancount/telemetry"
)

//go:embed index.html.tmpl
var indexTemplate string

type Server struct {
	Port       int
	LedgerFile string
	Host       string
	Version    string
	CommitSHA  string
}

func New(port int, ledgerFile string) *Server {
	return NewWithVersion(port, ledgerFile, "", "")
}

func NewWithVersion(port int, ledgerFile, version, commitSHA string) *Server {
	return &Server{
		Port:       port,
		LedgerFile: ledgerFile,
		Host:       "127.0.0.1",
		Version:    version,
		CommitSHA:  commitSHA,
	}
}

func (s *Server) Start(ctx context.Context) error {
	collector := telemetry.FromContext(ctx)
	timer := collector.Start(fmt.Sprintf("web.start %s:%d", s.Host, s.Port))
	defer timer.End()

	setupTimer := timer.Child("web.setup_router")
	mux, err := s.setupRouter()
	setupTimer.End()

	if err != nil {
		return fmt.Errorf("failed to setup router: %w", err)
	}

	addr := fmt.Sprintf("%s:%d", s.Host, s.Port)
	return http.ListenAndServe(addr, mux)
}

func (s *Server) setupRouter() (*http.ServeMux, error) {
	mux := http.NewServeMux()

	// API routes
	mux.HandleFunc("GET /api/source", s.handleGetSource)
	mux.HandleFunc("PUT /api/source", s.handlePutSource)

	// Get the appropriate dist filesystem for the build mode
	distFS, err := s.getDistFS()
	if err != nil {
		return nil, err
	}

	// Index handler with Vite fragment + version injection
	mux.HandleFunc("GET /{$}", s.makeIndexHandler(distFS))

	// Asset handlers
	assetsHandler := s.createAssetsHandler(distFS)
	mux.Handle("/assets/", assetsHandler) // Prod: hashed assets
	mux.Handle("/src/", assetsHandler)    // Dev: source files

	return mux, nil
}

// makeIndexHandler creates handler that injects Vite tags and version/commit into HTML
func (s *Server) makeIndexHandler(distFS fs.FS) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")

		// Get Vite fragment (build-tag specific implementation)
		viteFragment, err := s.getViteFragment(distFS)
		if err != nil {
			http.Error(w, "Error instantiating vite fragment", http.StatusInternalServerError)
			return
		}

		tmpl, err := template.New("index").Parse(indexTemplate)
		if err != nil {
			http.Error(w, "Error parsing template", http.StatusInternalServerError)
			return
		}

		data := map[string]interface{}{
			"Vite":      viteFragment,
			"Version":   s.Version,
			"CommitSHA": s.CommitSHA,
		}

		if err := tmpl.Execute(w, data); err != nil {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		}
	}
}
