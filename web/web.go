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
	"fmt"
	"net/http"
	"path/filepath"
	"sync"

	"github.com/robinvdvleuten/beancount/ledger"
	"github.com/robinvdvleuten/beancount/loader"
	"github.com/robinvdvleuten/beancount/telemetry"
)

type Server struct {
	Port      int
	Host      string
	Version   string
	CommitSHA string
	ReadOnly  bool

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

	// Require ledger file
	if s.ledgerFile == "" {
		return fmt.Errorf("ledger file is required")
	}

	loadTimer := timer.Child(fmt.Sprintf("web.load_ledger %s", filepath.Base(s.ledgerFile)))
	if err := s.reloadLedger(ctx); err != nil {
		loadTimer.End()
		return fmt.Errorf("failed to load ledger: %w", err)
	}
	loadTimer.End()

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

	// API routes (both dev and prod)
	mux.HandleFunc("GET /api/source", s.handleGetSource)
	mux.HandleFunc("PUT /api/source", s.requireWritable(s.handlePutSource))
	mux.HandleFunc("GET /api/accounts", s.handleGetAccounts)

	// Asset routes (prod: serves embedded files with template vars replaced, dev: no-op)
	s.mountAssets(mux)

	return mux, nil
}

// requireWritable is middleware that rejects write requests in read-only mode.
func (s *Server) requireWritable(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.ReadOnly {
			http.Error(w, "Server is in read-only mode", http.StatusForbidden)
			return
		}
		next(w, r)
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
