package web

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
)

// writeJSONResponse writes a JSON response to the http.ResponseWriter.
// If encoding fails, it writes an error response.
func writeJSONResponse(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
	}
}

// Files represents the loaded beancount files (root + includes).
// Matches the structure of window.__files in the frontend.
type Files struct {
	Root     string   `json:"root"`
	Includes []string `json:"includes"`
}

// SourceResponse is the response for GET and PUT /api/source.
type SourceResponse struct {
	Source      string  `json:"source"`
	Fingerprint string  `json:"fingerprint"`
	Errors      []error `json:"errors"`
	Files       Files   `json:"files"`
}

// computeFingerprint returns a short hash of content for change detection.
func computeFingerprint(content []byte) string {
	hash := sha256.Sum256(content)
	return hex.EncodeToString(hash[:])[:8]
}

// isAllowedFile checks if the given path is in the allowlist (root or includes).
// Must be called with s.mu held for reading.
func (s *Server) isAllowedFile(path string) bool {
	return path == s.rootFile || slices.Contains(s.includeFiles, path)
}

// resolveFilepathFromString resolves a filepath string to an absolute path.
// If the path is empty, returns the server's root file.
// The resolved path is validated against the allowlist.
func (s *Server) resolveFilepathFromString(path string) (string, error) {
	if path == "" {
		s.mu.RLock()
		root := s.rootFile
		s.mu.RUnlock()

		if root == "" {
			return "", fmt.Errorf("no filepath provided and no root file configured")
		}
		return root, nil
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("invalid filepath: %w", err)
	}

	s.mu.RLock()
	allowed := s.isAllowedFile(absPath)
	s.mu.RUnlock()

	if !allowed {
		return "", fmt.Errorf("access denied: file not in ledger")
	}

	return absPath, nil
}

// resolveFilepath extracts the filepath from the request query parameters.
// If no filepath is provided, returns the server's root file.
// The returned path is always absolute and validated against the allowlist.
func (s *Server) resolveFilepath(r *http.Request) (string, error) {
	filename := r.URL.Query().Get("filepath")
	return s.resolveFilepathFromString(filename)
}

// buildResponse creates a SourceResponse from the current ledger state.
// Must be called with s.mu held for reading.
func (s *Server) buildResponse(source []byte) *SourceResponse {
	includes := s.includeFiles
	if includes == nil {
		includes = []string{}
	}
	return &SourceResponse{
		Source:      string(source),
		Fingerprint: computeFingerprint(source),
		Errors:      s.ledger.Errors(),
		Files: Files{
			Root:     s.rootFile,
			Includes: includes,
		},
	}
}

// handleGetSource handles GET requests to /api/source.
// Returns the file content, validation errors, and files list as JSON.
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
	response := s.buildResponse(content)
	s.mu.RUnlock()

	writeJSONResponse(w, response)
}

// handlePutSource handles PUT requests to /api/source.
// Writes the provided content to the file and returns validation errors and updated files list.
// If fingerprint is provided and doesn't match current file, returns 409 Conflict (unless force=true).
func (s *Server) handlePutSource(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Filepath    string `json:"filepath"`
		Source      string `json:"source"`
		Fingerprint string `json:"fingerprint,omitempty"`
		Force       bool   `json:"force,omitempty"`
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

	// Conflict detection: compare fingerprints if provided
	if request.Fingerprint != "" && !request.Force {
		currentContent, err := os.ReadFile(filename)
		if err == nil {
			currentFingerprint := computeFingerprint(currentContent)
			if request.Fingerprint != currentFingerprint {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusConflict)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"error": "File changed since last load",
				})
				return
			}
		}
	}

	// Write file (outside lock)
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
	response := s.buildResponse([]byte(request.Source))
	s.mu.RUnlock()

	writeJSONResponse(w, response)
}
