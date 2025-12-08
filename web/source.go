package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	beancountErrors "github.com/robinvdvleuten/beancount/errors"
)

// writeJSONResponse writes a JSON response to the http.ResponseWriter.
// If encoding fails, it writes an error response.
func writeJSONResponse(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
	}
}

type SourceResponse struct {
	Filepath string                      `json:"filepath"`
	Source   string                      `json:"source"`
	Errors   []beancountErrors.ErrorJSON `json:"errors"`
}

// resolveFilepathFromString resolves a filepath string to an absolute path.
// If the path is empty, returns the server's default ledger file.
// The resolved path is validated to ensure it's within the allowed directory.
func (s *Server) resolveFilepathFromString(path string) (string, error) {
	if path == "" {
		if s.ledgerFile == "" {
			return "", fmt.Errorf("no filepath provided and no default ledger file configured")
		}
		return s.ledgerFile, nil
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("invalid filepath: %w", err)
	}

	if err := s.validateFilepath(absPath); err != nil {
		return "", err
	}

	return absPath, nil
}

// validateFilepath ensures the path is within the allowed directory by resolving
// all symlinks and checking the canonical path. If a default ledger file is
// configured, only files within its directory tree are allowed. This prevents
// both relative path traversal (../) and symlink-based directory traversal attacks.
func (s *Server) validateFilepath(path string) error {
	if s.ledgerFile == "" {
		return nil
	}

	allowedDir := filepath.Dir(s.ledgerFile)

	absAllowedDir, err := filepath.EvalSymlinks(allowedDir)
	if err != nil {
		return fmt.Errorf("invalid allowed directory: %w", err)
	}

	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		parentDir := filepath.Dir(path)
		resolvedParent, err := filepath.EvalSymlinks(parentDir)
		if err != nil {
			return fmt.Errorf("access denied: invalid path")
		}
		resolvedPath = filepath.Join(resolvedParent, filepath.Base(path))
	}

	relPath, err := filepath.Rel(absAllowedDir, resolvedPath)
	if err != nil {
		return fmt.Errorf("access denied: cannot determine relative path")
	}

	if relPath == ".." || strings.HasPrefix(relPath, ".."+string(filepath.Separator)) {
		return fmt.Errorf("access denied: filepath outside allowed directory")
	}

	return nil
}

// resolveFilepath extracts the filepath from the request query parameters.
// If no filepath is provided, returns the server's default ledger file.
// The returned path is always absolute and validated for security.
func (s *Server) resolveFilepath(r *http.Request) (string, error) {
	filename := r.URL.Query().Get("filepath")
	return s.resolveFilepathFromString(filename)
}

// buildResponse creates a SourceResponse from the current ledger state.
// Must be called with s.mu held for reading.
func (s *Server) buildResponse(filename string, source []byte) *SourceResponse {
	errorsJSON := []beancountErrors.ErrorJSON{}

	if len(s.ledger.Errors()) > 0 {
		jsonFormatter := beancountErrors.NewJSONFormatter()
		errorsJSON = jsonFormatter.FormatAllToSlice(s.ledger.Errors())
	}

	return &SourceResponse{
		Filepath: filename,
		Source:   string(source),
		Errors:   errorsJSON,
	}
}

// handleGetSource handles GET requests to /api/source.
// Returns the file content and validation errors as JSON.
func (s *Server) handleGetSource(w http.ResponseWriter, r *http.Request) {
	filename, err := s.resolveFilepath(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	content, err := os.ReadFile(filename)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "File not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Failed to read file", http.StatusInternalServerError)
		return
	}

	// Read lock for accessing ledger state
	s.mu.RLock()
	response := s.buildResponse(filename, content)
	s.mu.RUnlock()

	writeJSONResponse(w, response)
}

// handlePutSource handles PUT requests to /api/source.
// Writes the provided content to the file and returns validation errors.
func (s *Server) handlePutSource(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Filepath string `json:"filepath"`
		Source   string `json:"source"`
	}

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	filename, err := s.resolveFilepathFromString(request.Filepath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Write file first (outside lock)
	if err := os.WriteFile(filename, []byte(request.Source), 0600); err != nil {
		http.Error(w, "Failed to write file", http.StatusInternalServerError)
		return
	}

	// Reload ledger after save
	if err := s.reloadLedger(r.Context()); err != nil {
		http.Error(w, "Failed to reload ledger", http.StatusInternalServerError)
		return
	}

	// Build response from reloaded state
	s.mu.RLock()
	response := s.buildResponse(filename, []byte(request.Source))
	s.mu.RUnlock()

	writeJSONResponse(w, response)
}
