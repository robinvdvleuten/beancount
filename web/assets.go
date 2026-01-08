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

// filesData represents the loaded files injected into the HTML template.
type filesData struct {
	Root     string   `json:"root"`
	Includes []string `json:"includes"`
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

	// Marshal files data to JSON
	s.mu.RLock()
	files := filesData{
		Root:     s.rootFile,
		Includes: s.includeFiles,
	}
	s.mu.RUnlock()

	// Ensure includes is never null in JSON
	if files.Includes == nil {
		files.Includes = []string{}
	}

	filesJSON, err := json.Marshal(files)
	if err != nil {
		panic(fmt.Sprintf("failed to marshal files: %v", err))
	}

	// Execute template with JSON metadata and files
	var buf bytes.Buffer
	data := struct {
		Metadata template.JS
		Files    template.JS
	}{
		Metadata: template.JS(metadataJSON),
		Files:    template.JS(filesJSON),
	}
	if err := tmpl.Execute(&buf, data); err != nil {
		panic(fmt.Sprintf("failed to execute index.html template: %v", err))
	}

	htmlContent := buf.String()

	// Register static assets
	mux.Handle("/assets/", http.FileServerFS(fsys))

	// Register catch-all route for SPA (serves index.html for all unmatched paths)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, htmlContent)
	})
}
