package middleware

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGZIPMiddleware(t *testing.T) {
	// A handler that echoes back the request body
	dummyHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write(body)
	})

	handlerToTest := GZIPMiddleware(dummyHandler)

	t.Run("uncompressed request and uncompressed response", func(t *testing.T) {
		payload := `{"id":"Alloc","type":"gauge","value":10.5}`
		req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(payload))

		rec := httptest.NewRecorder()
		handlerToTest.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.NotContains(t, rec.Header().Get("Content-Encoding"), "gzip")
		assert.JSONEq(t, payload, rec.Body.String())
	})

	t.Run("uncompressed request and compressed response (Accept-Encoding: gzip)", func(t *testing.T) {
		payload := `{"id":"Alloc","type":"gauge","value":10.5}`
		req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(payload))
		req.Header.Set("Accept-Encoding", "gzip")

		rec := httptest.NewRecorder()
		handlerToTest.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "gzip", rec.Header().Get("Content-Encoding"))

		gzReader, err := gzip.NewReader(rec.Body)
		require.NoError(t, err)
		defer gzReader.Close()

		decompressedPayload, err := io.ReadAll(gzReader)
		require.NoError(t, err)

		assert.JSONEq(t, payload, string(decompressedPayload))
	})

	t.Run("compressed request (Content-Encoding: gzip) and uncompressed response", func(t *testing.T) {
		payload := `{"id":"Alloc","type":"gauge","value":10.5}`

		var buf bytes.Buffer
		gzWriter := gzip.NewWriter(&buf)
		_, err := gzWriter.Write([]byte(payload))
		require.NoError(t, err)
		gzWriter.Close()

		req := httptest.NewRequest(http.MethodPost, "/", &buf)
		req.Header.Set("Content-Encoding", "gzip")

		rec := httptest.NewRecorder()
		handlerToTest.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.NotContains(t, rec.Header().Get("Content-Encoding"), "gzip")
		assert.JSONEq(t, payload, rec.Body.String())
	})

	t.Run("compressed request and compressed response (Both headers active)", func(t *testing.T) {
		payload := `{"id":"Alloc","type":"gauge","value":10.5}`

		var buf bytes.Buffer
		gzWriter := gzip.NewWriter(&buf)
		_, err := gzWriter.Write([]byte(payload))
		require.NoError(t, err)
		gzWriter.Close()

		req := httptest.NewRequest(http.MethodPost, "/", &buf)
		req.Header.Set("Content-Encoding", "gzip")
		req.Header.Set("Accept-Encoding", "gzip")

		rec := httptest.NewRecorder()
		handlerToTest.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "gzip", rec.Header().Get("Content-Encoding"))

		gzReader, err := gzip.NewReader(rec.Body)
		require.NoError(t, err)
		defer gzReader.Close()

		decompressedPayload, err := io.ReadAll(gzReader)
		require.NoError(t, err)

		assert.JSONEq(t, payload, string(decompressedPayload))
	})

	t.Run("invalid gzip request payload returns 400 Bad Request", func(t *testing.T) {
		payload := `{"id":"Alloc","type":"gauge","value":10.5}`
		req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(payload))
		req.Header.Set("Content-Encoding", "gzip")

		rec := httptest.NewRecorder()
		handlerToTest.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)

		assert.Contains(t, rec.Body.String(), "gzip: invalid header")
	})
}
