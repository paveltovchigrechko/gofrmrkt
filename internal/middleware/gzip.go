package middleware

import (
	"compress/gzip"
	"io"
	"net/http"
	"strings"
)

type gzipWriter struct {
	http.ResponseWriter
	Writer io.Writer
}

func (w gzipWriter) Write(b []byte) (int, error) {
	return w.Writer.Write(b)
}

func (w gzipWriter) WriteHeader(statusCode int) {
	// Delete Content-Length because compression changes the payload size
	w.Header().Del("Content-Length")
	w.ResponseWriter.WriteHeader(statusCode)
}

func GZIPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Handle gzip-encoded request
		if strings.Contains(r.Header.Get("Content-Encoding"), "gzip") {
			gzReader, err := gzip.NewReader(r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			defer gzReader.Close()

			r.Body = gzReader
		}

		// If request doesn't accept gzip, return immediately
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}

		gzWriter, err := gzip.NewWriterLevel(w, gzip.BestSpeed)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		defer gzWriter.Close()

		w.Header().Set("Content-Encoding", "gzip")

		wrappedWriter := gzipWriter{
			ResponseWriter: w,
			Writer:         gzWriter,
		}

		next.ServeHTTP(wrappedWriter, r)
	})
}
