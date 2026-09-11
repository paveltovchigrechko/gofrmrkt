package middleware

import (
	"bytes"
	"io"
	"net/http"
	"time"

	"go.uber.org/zap"
)

type (
	responseData struct {
		status int
		size   int
		err    error
	}

	loggingResponseWriter struct {
		http.ResponseWriter
		responseData *responseData
	}
)

type ErrorRecorder interface {
	SetError(error)
}

func (r *loggingResponseWriter) Write(b []byte) (int, error) {
	if r.responseData.status == 0 {
		r.WriteHeader(http.StatusOK)
	}
	size, err := r.ResponseWriter.Write(b)
	r.responseData.size += size
	return size, err
}

func (r *loggingResponseWriter) WriteHeader(statusCode int) {
	if r.responseData.status != 0 {
		return
	}
	r.responseData.status = statusCode
	r.ResponseWriter.WriteHeader(statusCode)
}

func (r *loggingResponseWriter) SetError(err error) {
	r.responseData.err = err
}

func NewLogger(filePath string) (*zap.SugaredLogger, error) {
	cfg := zap.NewDevelopmentConfig()

	cfg.OutputPaths = []string{"stdout", filePath}
	cfg.ErrorOutputPaths = []string{"stderr", filePath}

	logger, err := cfg.Build()
	if err != nil {
		return nil, err
	}

	return logger.Sugar(), nil
}

func Logger(log *zap.SugaredLogger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			responseData := &responseData{
				status: 0,
				size:   0,
			}
			lw := loggingResponseWriter{
				ResponseWriter: w,
				responseData:   responseData,
			}

			var bodyBytes []byte
			if r.Body != nil {
				var err error
				bodyBytes, err = io.ReadAll(r.Body)
				if err != nil {
					log.Errorw("Failed to read request body for logging", "error", err)
				}
				r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
			}

			ct := r.Header.Get("Content-Type")
			ce := r.Header.Get("Content-Encoding")
			next.ServeHTTP(&lw, r)

			duration := time.Since(start)

			log.Infow(
				"request completed",
				"uri", r.RequestURI,
				"method", r.Method,
				"request body", string(bodyBytes),
				"request content type", ct,
				"request content encoding", ce,
				"response status", responseData.status,
				"duration", duration,
				"response size", responseData.size,
				"response error", responseData.err,
			)
		})
	}
}
