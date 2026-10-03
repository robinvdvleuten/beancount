package web

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"os"

	"github.com/robinvdvleuten/beancount/diagnostic"
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

// sourceError is an error without the shape, such as an I/O failure, as
// the web API sends it.
type sourceError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

func (e *sourceError) Error() string {
	return e.Message
}

// positionedError is an error of a loaded ledger as the web API sends it:
// its kind as type, its message starting with its Error line, and its
// position, so the editor marks its line.
type positionedError struct{ diagnostic.Positioned }

func (e positionedError) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{
		"type":     e.Kind(),
		"message":  e.Error(),
		"position": e.GetPosition(),
	})
}

// jsonSafeSourceError returns err as the web API marshals it: as itself
// when it marshals itself (a ledger error, which adds its account and
// date), from its shape when it has one, and otherwise as a LoadError with
// its message.
func jsonSafeSourceError(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := err.(json.Marshaler); ok {
		return err
	}
	if positioned, ok := err.(diagnostic.Positioned); ok {
		return positionedError{positioned}
	}
	return &sourceError{Type: "LoadError", Message: err.Error()}
}

// computeFingerprint returns a short hash of content for change detection.
func computeFingerprint(content []byte) string {
	hash := sha256.Sum256(content)
	return hex.EncodeToString(hash[:])[:8]
}

// handleGetSource handles GET requests to /api/source.
// Returns the file content, validation errors, and files list as JSON.
func (s *Server) handleGetSource(w http.ResponseWriter, r *http.Request) {
	snap := s.snapshot()
	filename, err := snap.resolve(r.URL.Query().Get("filepath"))
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

	writeJSONResponse(w, snap.sourceResponse(content))
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

	filename, err := s.snapshot().resolve(request.Filepath)
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

	// Reload ledger after save. Parse/validation errors are expected and
	// returned to the client — only log unexpected failures.
	_, snap, err := s.reloadLedger(r.Context())
	if err != nil {
		log.Printf("Warning: ledger reload after save: %v", err)
	}

	// The response is the reloaded ledger's (includes any validation errors)
	writeJSONResponse(w, snap.sourceResponse([]byte(request.Source)))
}
