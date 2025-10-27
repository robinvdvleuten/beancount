package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	beancountErrors "github.com/robinvdvleuten/beancount/errors"
	"github.com/robinvdvleuten/beancount/ledger"
	"github.com/robinvdvleuten/beancount/loader"
)

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
		if s.LedgerFile == "" {
			return "", fmt.Errorf("no filepath provided and no default ledger file configured")
		}
		return s.LedgerFile, nil
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
	if s.LedgerFile == "" {
		return nil
	}

	allowedDir := filepath.Dir(s.LedgerFile)

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

	if len(relPath) >= 2 && relPath[:2] == ".." {
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

// validateAndBuildResponse parses and validates the beancount source,
// returning a response with the source content and any validation errors.
// Validation includes parsing, account opening checks, and balance assertions.
func (s *Server) validateAndBuildResponse(ctx context.Context, filename string, source []byte) (*SourceResponse, error) {
	var errorList []error

	ldr := loader.New(loader.WithFollowIncludes())
	ast, err := ldr.LoadBytes(ctx, filename, source)
	if err != nil {
		errorList = append(errorList, err)
	}

	if ast != nil {
		l := ledger.New()
		if err := l.Process(ctx, ast); err != nil {
			var validationErrors *ledger.ValidationErrors
			if errors.As(err, &validationErrors) {
				errorList = append(errorList, validationErrors.Errors...)
			}
		}
	}

	jsonFormatter := beancountErrors.NewJSONFormatter()
	var errorsJSON []beancountErrors.ErrorJSON
	if len(errorList) > 0 {
		jsonStr := jsonFormatter.FormatAll(errorList)
		if err := json.Unmarshal([]byte(jsonStr), &errorsJSON); err != nil {
			return nil, fmt.Errorf("failed to unmarshal errors: %w", err)
		}
	}

	return &SourceResponse{
		Filepath: filename,
		Source:   string(source),
		Errors:   errorsJSON,
	}, nil
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

	response, err := s.validateAndBuildResponse(r.Context(), filename, content)
	if err != nil {
		http.Error(w, "Failed to validate source", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
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

	if err := os.WriteFile(filename, []byte(request.Source), 0600); err != nil {
		http.Error(w, "Failed to write file", http.StatusInternalServerError)
		return
	}

	response, err := s.validateAndBuildResponse(r.Context(), filename, []byte(request.Source))
	if err != nil {
		http.Error(w, "Failed to validate source", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}
