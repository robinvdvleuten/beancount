package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alecthomas/assert/v2"
)

func TestAPISource(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "test-*.beancount")
	assert.NoError(t, err)
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	testContent := "2024-01-01 * \"Test transaction\"\n  Assets:Checking  100 USD\n  Expenses:Food   -100 USD"
	_, err = tmpFile.WriteString(testContent)
	assert.NoError(t, err)
	_ = tmpFile.Close()

	server := New(8080, tmpFile.Name())
	err = server.reloadLedger(context.Background())
	assert.NoError(t, err)
	mux, err := server.setupRouter()
	assert.NoError(t, err)

	t.Run("WithDefaultFile", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/source", nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))

		var response map[string]interface{}
		err := json.NewDecoder(rec.Body).Decode(&response)
		assert.NoError(t, err)
		assert.Equal(t, testContent, response["source"].(string))
		assert.True(t, strings.HasSuffix(response["filepath"].(string), tmpFile.Name()))
		assert.NotEqual(t, nil, response["errors"])
	})

	t.Run("WithQueryParameter", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/source?filepath="+tmpFile.Name(), nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)

		var response map[string]interface{}
		err := json.NewDecoder(rec.Body).Decode(&response)
		assert.NoError(t, err)
		assert.Equal(t, testContent, response["source"].(string))
	})

	t.Run("FileNotFound", func(t *testing.T) {
		nonexistentPath := filepath.Join(filepath.Dir(tmpFile.Name()), "nonexistent.beancount")
		req := httptest.NewRequest(http.MethodGet, "/api/source?filepath="+nonexistentPath, nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("NoFilepathNoDefault", func(t *testing.T) {
		serverNoDefault := New(8080, "")
		muxNoDefault, err := serverNoDefault.setupRouter()
		assert.NoError(t, err)
		req := httptest.NewRequest(http.MethodGet, "/api/source", nil)
		rec := httptest.NewRecorder()

		muxNoDefault.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("PutUpdateContent", func(t *testing.T) {
		updatedContent := "2024-01-02 * \"Updated transaction\"\n  Assets:Checking  200 USD\n  Expenses:Food   -200 USD"
		requestBody := map[string]string{
			"source": updatedContent,
		}
		bodyBytes, err := json.Marshal(requestBody)
		assert.NoError(t, err)

		req := httptest.NewRequest(http.MethodPut, "/api/source", strings.NewReader(string(bodyBytes)))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)

		var response SourceResponse
		err = json.NewDecoder(rec.Body).Decode(&response)
		assert.NoError(t, err)
		assert.Equal(t, updatedContent, response.Source)
		assert.Equal(t, tmpFile.Name(), response.Filepath)

		content, err := os.ReadFile(tmpFile.Name())
		assert.NoError(t, err)
		assert.Equal(t, updatedContent, string(content))
	})

	t.Run("PutWithFilepath", func(t *testing.T) {
		tmpFile2, err := os.CreateTemp("", "test2-*.beancount")
		assert.NoError(t, err)
		defer func() { _ = os.Remove(tmpFile2.Name()) }()
		_ = tmpFile2.Close()

		updatedContent := "2024-01-03 * \"New file content\"\n  Assets:Bank  300 USD"
		requestBody := map[string]string{
			"filepath": tmpFile2.Name(),
			"source":   updatedContent,
		}
		bodyBytes, err := json.Marshal(requestBody)
		assert.NoError(t, err)

		req := httptest.NewRequest(http.MethodPut, "/api/source", strings.NewReader(string(bodyBytes)))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)

		content, err := os.ReadFile(tmpFile2.Name())
		assert.NoError(t, err)
		assert.Equal(t, updatedContent, string(content))
	})

	t.Run("PutInvalidJSON", func(t *testing.T) {
		body := strings.NewReader(`invalid json`)
		req := httptest.NewRequest(http.MethodPut, "/api/source", body)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("GetWithParseError", func(t *testing.T) {
		tmpFileErr, err := os.CreateTemp("", "test-error-*.beancount")
		assert.NoError(t, err)
		defer func() { _ = os.Remove(tmpFileErr.Name()) }()

		invalidContent := "2024-13-99 * \"Invalid date\""
		_, err = tmpFileErr.WriteString(invalidContent)
		assert.NoError(t, err)
		_ = tmpFileErr.Close()

		req := httptest.NewRequest(http.MethodGet, "/api/source?filepath="+tmpFileErr.Name(), nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)

		var response map[string]interface{}
		err = json.NewDecoder(rec.Body).Decode(&response)
		assert.NoError(t, err)
		assert.Equal(t, invalidContent, response["source"].(string))

		errors, ok := response["errors"].([]interface{})
		assert.True(t, ok)
		assert.True(t, len(errors) > 0, "Expected at least one parse error")
	})

	t.Run("GetWithValidationError", func(t *testing.T) {
		tmpFileErr, err := os.CreateTemp("", "test-validation-*.beancount")
		assert.NoError(t, err)
		defer func() { _ = os.Remove(tmpFileErr.Name()) }()

		invalidContent := "2024-01-01 * \"Using unopened account\"\n  Assets:Checking  100 USD\n  Expenses:Food   -100 USD"
		_, err = tmpFileErr.WriteString(invalidContent)
		assert.NoError(t, err)
		_ = tmpFileErr.Close()

		req := httptest.NewRequest(http.MethodGet, "/api/source?filepath="+tmpFileErr.Name(), nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)

		var response map[string]interface{}
		err = json.NewDecoder(rec.Body).Decode(&response)
		assert.NoError(t, err)

		errors, ok := response["errors"].([]interface{})
		assert.True(t, ok)
		assert.True(t, len(errors) > 0, "Expected validation errors for unopened accounts")
	})

	t.Run("RejectPathTraversal", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/source?filepath=../../../etc/passwd", nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.True(t, strings.Contains(rec.Body.String(), "access denied"))
	})

	t.Run("RejectAbsolutePathOutsideAllowedDir", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/source?filepath=/etc/passwd", nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.True(t, strings.Contains(rec.Body.String(), "access denied"))
	})

	t.Run("RejectSymlinkToSensitiveDir", func(t *testing.T) {
		symlinkPath := filepath.Join(filepath.Dir(tmpFile.Name()), "evil_link")
		err := os.Symlink("/etc", symlinkPath)
		if err != nil {
			t.Skip("Cannot create symlink, skipping test")
		}
		defer func() { _ = os.Remove(symlinkPath) }()

		evilPath := filepath.Join(symlinkPath, "passwd")
		req := httptest.NewRequest(http.MethodGet, "/api/source?filepath="+evilPath, nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
		assert.True(t, strings.Contains(rec.Body.String(), "access denied"))
	})
}
