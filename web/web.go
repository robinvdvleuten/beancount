// Package web provides an HTTP server for the Beancount web editor.
//
// The server exposes a REST API for reading and writing Beancount files,
// with real-time validation and error reporting. It also serves the
// web-based editor frontend as static files.
//
// SECURITY WARNING: This server has no authentication and should only be
// bound to localhost (127.0.0.1). Do not expose it to untrusted networks.
// File access is restricted to the root file and its includes.
package web

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/robinvdvleuten/beancount/telemetry"
)

type Server struct {
	Port         int
	Host         string
	Version      string
	CommitSHA    string
	ReadOnly     bool
	WatchEnabled bool

	// mu guards state, the ledger the handlers answer from. Read it
	// through snapshot; only reloadLedger replaces it.
	mu    sync.RWMutex
	state *snapshot
	// reloadMu makes reloads follow one another, so each snapshot is built
	// from the one before it and the last reload to start is the one served.
	reloadMu sync.Mutex

	// inputFile is the ledger file passed to New(), which every load reads.
	// A snapshot holds its resolved absolute path.
	inputFile string

	// SSE clients for broadcasting reload events
	sseClients map[chan string]struct{}
	sseMu      sync.Mutex
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
		state:      newSnapshot(),
		inputFile:  ledgerFile,
		sseClients: make(map[chan string]struct{}),
	}
}

// snapshot returns the ledger as last loaded. A handler takes it once and
// answers from it alone.
func (s *Server) snapshot() *snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

func (s *Server) Start(ctx context.Context) error {
	collector := telemetry.FromContext(ctx)
	timer := collector.Start(fmt.Sprintf("web.start %s:%d", s.Host, s.Port))
	defer timer.End()

	// Require ledger file
	if s.inputFile == "" {
		return fmt.Errorf("ledger file is required")
	}
	// A ledger that fails to load is still served, but not one whose path
	// cannot be resolved, which leaves no snapshot to build.
	if _, err := absolutePath(s.inputFile); err != nil {
		return err
	}

	loadTimer := timer.Child(fmt.Sprintf("web.load_ledger %s", filepath.Base(s.inputFile)))
	if _, _, err := s.reloadLedger(ctx); err != nil {
		log.Printf("Warning: initial ledger load failed: %v", err)
	}
	loadTimer.End()

	// Start file watcher if enabled
	if s.WatchEnabled {
		if err := s.startWatcher(ctx); err != nil {
			return fmt.Errorf("failed to start file watcher: %w", err)
		}
	}

	setupTimer := timer.Child("web.setup_router")
	mux, err := s.setupRouter()
	setupTimer.End()

	if err != nil {
		return fmt.Errorf("failed to setup router: %w", err)
	}

	addr := fmt.Sprintf("%s:%d", s.Host, s.Port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}

	serverCtx, cancelServer := context.WithCancel(ctx)
	defer cancelServer()

	server := &http.Server{
		Addr:    addr,
		Handler: mux,
		BaseContext: func(net.Listener) context.Context {
			return serverCtx
		},
	}

	go func() {
		<-serverCtx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("Failed to shut down web server: %v", err)
		}
	}()

	err = server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return fmt.Errorf("failed to serve on %s: %w", addr, err)
}

func (s *Server) setupRouter() (*http.ServeMux, error) {
	mux := http.NewServeMux()

	// API routes (both dev and prod)
	mux.HandleFunc("GET /api/source", s.handleGetSource)
	mux.HandleFunc("PUT /api/source", s.requireWritable(s.handlePutSource))
	mux.HandleFunc("GET /api/accounts", s.handleGetAccounts)
	mux.HandleFunc("GET /api/balances", s.handleGetBalances)
	mux.HandleFunc("POST /api/query", s.handleQuery)
	mux.HandleFunc("GET /api/events", s.handleSSE)

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

// reloadLedger loads the ledger from disk and swaps in the snapshot that
// follows from it. It returns the snapshot it replaced and the one now
// served, and the error of a load that could not go on, which the new
// snapshot carries too. When no snapshot could be built, the one served
// stays, and is returned as both.
// Caller must NOT hold the mutex - this method acquires it internally.
func (s *Server) reloadLedger(ctx context.Context) (prev, next *snapshot, err error) {
	s.reloadMu.Lock()
	defer s.reloadMu.Unlock()

	prev = s.snapshot()
	next, err = prev.reload(ctx, s.inputFile)
	if err != nil {
		return prev, prev, err
	}

	s.mu.Lock()
	s.state = next
	s.mu.Unlock()

	return prev, next, next.loadErr
}

// startWatcher starts a file watcher for the root file and all includes.
// It reloads the ledger and broadcasts SSE events when files change.
func (s *Server) startWatcher(ctx context.Context) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to create file watcher: %w", err)
	}

	// Add root file and all includes to watch
	snap := s.snapshot()
	filesToWatch := append([]string{snap.root}, snap.includes...)

	for _, file := range filesToWatch {
		if err := watcher.Add(file); err != nil {
			log.Printf("Warning: failed to watch %s: %v", file, err)
		}
	}

	// Start watcher goroutine
	go s.runWatcher(ctx, watcher)

	return nil
}

// runWatcher processes file system events with debouncing.
func (s *Server) runWatcher(ctx context.Context, watcher *fsnotify.Watcher) {
	var debounceTimer *time.Timer
	defer func() {
		if debounceTimer != nil {
			debounceTimer.Stop()
		}
		_ = watcher.Close()
	}()

	// Debounce timer - editors often write files in multiple steps
	const debounceDelay = 100 * time.Millisecond

	for {
		select {
		case <-ctx.Done():
			return

		case event, ok := <-watcher.Events:
			if !ok {
				return
			}

			// React to write/create/remove/rename events
			// (Remove/Rename are common in atomic saves)
			if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove|fsnotify.Rename) == 0 {
				continue
			}

			// Reset debounce timer
			if debounceTimer != nil {
				debounceTimer.Stop()
			}

			debounceTimer = time.AfterFunc(debounceDelay, func() {
				s.handleFileChange(ctx, watcher)
			})

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.Printf("File watcher error: %v", err)
		}
	}
}

// handleFileChange reloads the ledger and updates the watch list.
func (s *Server) handleFileChange(ctx context.Context, watcher *fsnotify.Watcher) {
	// Reload ledger — returns the snapshot it replaced and the one it built
	prev, next, err := s.reloadLedger(ctx)
	if err != nil {
		log.Printf("Failed to reload ledger: %v", err)
		s.broadcast("reload")
		return
	}

	oldIncludes := make(map[string]bool)
	for _, f := range prev.includes {
		oldIncludes[f] = true
	}
	newIncludes := make(map[string]bool)
	for _, f := range next.includes {
		newIncludes[f] = true
	}
	newRoot := next.root

	// Remove watches for files no longer included
	for file := range oldIncludes {
		if !newIncludes[file] {
			_ = watcher.Remove(file)
		}
	}

	// Update watches for all current includes (re-add to ensure we catch re-created files)
	for file := range newIncludes {
		if err := watcher.Add(file); err != nil {
			log.Printf("Warning: failed to watch %s: %v", file, err)
		}
	}

	// Re-add root (always needed)
	if err := watcher.Add(newRoot); err != nil {
		log.Printf("Warning: failed to watch root %s: %v", newRoot, err)
	}

	// Broadcast reload event to all SSE clients
	s.broadcast("reload")
}

// handleSSE handles Server-Sent Events connections for real-time updates.
func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Create client channel
	clientChan := make(chan string, 10)

	// Register client
	s.sseMu.Lock()
	s.sseClients[clientChan] = struct{}{}
	s.sseMu.Unlock()

	// Cleanup on disconnect
	defer func() {
		s.sseMu.Lock()
		delete(s.sseClients, clientChan)
		s.sseMu.Unlock()
		close(clientChan)
	}()

	// Get flusher for streaming
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	// Send initial connection event
	_, _ = fmt.Fprintf(w, "data: connected\n\n")
	flusher.Flush()

	// Stream events to client
	for {
		select {
		case <-r.Context().Done():
			return
		case event := <-clientChan:
			_, _ = fmt.Fprintf(w, "data: %s\n\n", event)
			flusher.Flush()
		}
	}
}

// broadcast sends an event to all connected SSE clients.
func (s *Server) broadcast(event string) {
	s.sseMu.Lock()
	defer s.sseMu.Unlock()

	for clientChan := range s.sseClients {
		select {
		case clientChan <- event:
		default:
			// Client buffer full, skip
		}
	}
}
