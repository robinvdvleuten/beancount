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
	"path/filepath"
	"sync"

	"github.com/robinvdvleuten/beancount/ledger"
	"github.com/robinvdvleuten/beancount/loader"
	"github.com/robinvdvleuten/beancount/telemetry"
)

//go:embed index.html.tmpl
var indexTemplate string

type Server struct {
	Port      int
	Host      string
	Version   string
	CommitSHA string

	mu         sync.RWMutex
	ledger     *ledger.Ledger
	ledgerFile string
}

func New(port int, ledgerFile string) *Server {
	return NewWithVersion(port, ledgerFile, "", "")
}

func NewWithVersion(port int, ledgerFile, version, commitSHA string) *Server {
	return &Server{
		Port:       port,
		Host:       "127.0.0.1",
		Version:    version,
		CommitSHA:  commitSHA,
		ledgerFile: ledgerFile,
	}
}

func (s *Server) Start(ctx context.Context) error {
	collector := telemetry.FromContext(ctx)
	timer := collector.Start(fmt.Sprintf("web.start %s:%d", s.Host, s.Port))
	defer timer.End()

	// Load initial ledger
	if s.ledgerFile != "" {
		loadTimer := timer.Child(fmt.Sprintf("web.load_ledger %s", filepath.Base(s.ledgerFile)))
		if err := s.reloadLedger(ctx); err != nil {
			loadTimer.End()
			return fmt.Errorf("failed to load ledger: %w", err)
		}
		loadTimer.End()
	}

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

// reloadLedger loads or reloads the ledger from disk.
// Caller must NOT hold the mutex - this method acquires it internally.
func (s *Server) reloadLedger(ctx context.Context) error {
	ldr := loader.New(loader.WithFollowIncludes())

	ast, err := ldr.Load(ctx, s.ledgerFile)
	if err != nil {
		return err // I/O or parse error
	}

	l := ledger.New()
	_ = l.Process(ctx, ast) // Validation errors in l.Errors()

	s.mu.Lock()
	s.ledger = l
	s.mu.Unlock()

	return nil
}
