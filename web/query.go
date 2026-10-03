package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/robinvdvleuten/beancount/query"
)

// QueryRequest is the JSON body of POST /api/query.
type QueryRequest struct {
	Query string `json:"query"`
	// Format is "text" (the default) or "csv", like beancount query -f.
	Format string `json:"format"`
}

// QueryResponse holds what beancount query prints for the statement.
type QueryResponse struct {
	Output string `json:"output"`
}

// handleQuery runs one BQL statement against the loaded ledger and returns
// what beancount query prints for it: its result, or the error it prints on
// stderr for a statement that does not parse or compile, which is output,
// not an HTTP error. It changes nothing, so it also runs in read-only mode.
func (s *Server) handleQuery(w http.ResponseWriter, r *http.Request) {
	var request QueryRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}
	format := query.Format(request.Format)
	if format == "" {
		format = query.FormatText
	}
	if format != query.FormatText && format != query.FormatCSV {
		http.Error(w, "invalid format: "+request.Format, http.StatusBadRequest)
		return
	}

	qctx := s.snapshot().queryContext()
	if qctx == nil {
		http.Error(w, "the ledger failed to load, so there is nothing to query", http.StatusConflict)
		return
	}

	var output strings.Builder
	if err := query.Run(r.Context(), qctx, strings.TrimSpace(request.Query), format, false, &output); err != nil {
		var queryErr *query.Error
		if !errors.As(err, &queryErr) {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_, _ = fmt.Fprintln(&output, queryErr.Report())
	}

	writeJSONResponse(w, QueryResponse{Output: output.String()})
}
