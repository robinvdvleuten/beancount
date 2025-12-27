//go:build !dev

package web

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
)

//go:embed all:dist
var distEmbed embed.FS

// metadata represents the server metadata injected into the HTML template.
type metadata struct {
	Version   string `json:"version"`
	CommitSHA string `json:"commitSHA"`
	ReadOnly  bool   `json:"readOnly"`
}

// mountAssets registers all asset routes (index + static files) for production.
// Replaces Go template variables in index.html at server startup.
func (s *Server) mountAssets(mux *http.ServeMux) {
	fsys, err := fs.Sub(distEmbed, "dist")
	if err != nil {
		panic(fmt.Sprintf("failed to create sub filesystem: %v", err))
	}

	// Read index.html and parse as template
	indexHTML, err := fs.ReadFile(fsys, "index.html")
	if err != nil {
		panic(fmt.Sprintf("failed to read embedded index.html: %v", err))
	}

	tmpl, err := template.New("index").Parse(string(indexHTML))
	if err != nil {
		panic(fmt.Sprintf("failed to parse index.html template: %v", err))
	}

	// Marshal metadata to JSON
	meta := metadata{
		Version:   s.Version,
		CommitSHA: s.CommitSHA,
		ReadOnly:  s.ReadOnly,
	}
	metadataJSON, err := json.Marshal(meta)
	if err != nil {
		panic(fmt.Sprintf("failed to marshal metadata: %v", err))
	}

	// Execute template with JSON metadata
	var buf bytes.Buffer
	data := struct {
		Metadata template.JS
	}{
		Metadata: template.JS(metadataJSON),
	}
	if err := tmpl.Execute(&buf, data); err != nil {
		panic(fmt.Sprintf("failed to execute index.html template: %v", err))
	}

	htmlContent := buf.String()

	// Register index route
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, htmlContent)
	})

	// Register static assets
	mux.Handle("/assets/", http.FileServerFS(fsys))
}
