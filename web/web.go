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
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/robinvdvleuten/beancount/ledger"
	"github.com/robinvdvleuten/beancount/loader"
	"github.com/robinvdvleuten/beancount/telemetry"
)

type Server struct {
	Port         int
	Host         string
	Version      string
	CommitSHA    string
	ReadOnly     bool
	WatchEnabled bool

	mu           sync.RWMutex
	ledger       *ledger.Ledger
	rootFile     string   // Absolute path of the root ledger file
	includeFiles []string // Absolute paths of included files

	// inputFile is the file path passed to New(), used only for initial loading.
	// After loading, rootFile contains the resolved absolute path.
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
		Port:      port,
		Host:      "127.0.0.1",
		Version:   version,
		CommitSHA: commitSHA,
		inputFile: ledgerFile,
	}
}

func (s *Server) Start(ctx context.Context) error {
	collector := telemetry.FromContext(ctx)
	timer := collector.Start(fmt.Sprintf("web.start %s:%d", s.Host, s.Port))
	defer timer.End()

	// Require ledger file
	if s.inputFile == "" {
		return fmt.Errorf("ledger file is required")
	}

	// Initialize SSE clients map
	s.sseClients = make(map[chan string]struct{})

	loadTimer := timer.Child(fmt.Sprintf("web.load_ledger %s", filepath.Base(s.inputFile)))
	if err := s.reloadLedger(ctx); err != nil {
		loadTimer.End()
		return fmt.Errorf("failed to load ledger: %w", err)
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
	return http.ListenAndServe(addr, mux)
}

func (s *Server) setupRouter() (*http.ServeMux, error) {
	mux := http.NewServeMux()

	// API routes (both dev and prod)
	mux.HandleFunc("GET /api/source", s.handleGetSource)
	mux.HandleFunc("PUT /api/source", s.requireWritable(s.handlePutSource))
	mux.HandleFunc("GET /api/accounts", s.handleGetAccounts)
	mux.HandleFunc("GET /api/balances", s.handleGetBalances)
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

// reloadLedger loads or reloads the ledger from disk.
// Caller must NOT hold the mutex - this method acquires it internally.
func (s *Server) reloadLedger(ctx context.Context) error {
	ldr := loader.New(loader.WithFollowIncludes())

	result, err := ldr.Load(ctx, s.inputFile)
	if err != nil {
		return err // I/O or parse error
	}

	l := ledger.New()
	_ = l.Process(ctx, result.AST) // Validation errors in l.Errors()

	s.mu.Lock()
	s.ledger = l
	s.rootFile = result.Root
	s.includeFiles = result.Includes
	s.mu.Unlock()

	return nil
}

// startWatcher starts a file watcher for the root file and all includes.
// It reloads the ledger and broadcasts SSE events when files change.
func (s *Server) startWatcher(ctx context.Context) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to create file watcher: %w", err)
	}

	// Add root file and all includes to watch
	s.mu.RLock()
	filesToWatch := append([]string{s.rootFile}, s.includeFiles...)
	s.mu.RUnlock()

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
	// Get old include files for comparison
	s.mu.RLock()
	oldIncludes := make(map[string]bool)
	for _, f := range s.includeFiles {
		oldIncludes[f] = true
	}
	s.mu.RUnlock()

	// Reload ledger
	if err := s.reloadLedger(ctx); err != nil {
		log.Printf("Failed to reload ledger: %v", err)
		return
	}

	// Update watch list (includes may have changed)
	s.mu.RLock()
	newIncludes := make(map[string]bool)
	for _, f := range s.includeFiles {
		newIncludes[f] = true
	}
	newRoot := s.rootFile
	s.mu.RUnlock()

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
