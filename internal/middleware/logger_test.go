package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestLoggerMiddleware(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	log := zap.New(core).Sugar()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, err := w.Write([]byte("created"))
		require.NoError(t, err)
	})

	handler := Logger(log)(next)

	req := httptest.NewRequest(http.MethodPost, "/update/gauge/test/10", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, req)

	assert.Equal(t, http.StatusCreated, recorder.Code)
	assert.Equal(t, "created", recorder.Body.String())

	require.Equal(t, 1, logs.Len())

	entry := logs.All()[0]
	assert.Equal(t, "request completed", entry.Message)

	fields := entry.ContextMap()
	assert.Equal(t, http.MethodPost, fields["method"])
	assert.Equal(t, "/update/gauge/test/10", fields["uri"])
	assert.Equal(t, int64(http.StatusCreated), fields["response status"])
	assert.Equal(t, int64(len("created")), fields["response size"])
	assert.Contains(t, fields, "duration")
}
