package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/paveltovchigrechko/gofrmrkt/internal/service"
)

func TestUserIDMiddleware(t *testing.T) {
	authSvc, err := service.NewAuthService("test-secret", 1*time.Hour)
	require.NoError(t, err)

	authenticator := NewAuthenticator(authSvc)

	// Sample protected handler to check context value
	var capturedUserID int64
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := UserIDFromContext(r.Context())
		if ok {
			capturedUserID = id
		}
		w.WriteHeader(http.StatusOK)
	})

	handlerToTest := authenticator.UserIDMiddleware(nextHandler)

	t.Run("valid token in cookie allows access and sets context", func(t *testing.T) {
		var expectedID int64 = 55
		token, err := authSvc.BuildJWTString(expectedID)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodGet, "/protected", nil)
		req.AddCookie(&http.Cookie{Name: authCookieName, Value: token})
		rec := httptest.NewRecorder()

		handlerToTest.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, expectedID, capturedUserID)
	})

	t.Run("missing token returns 401 Unauthorized", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/protected", nil)
		rec := httptest.NewRecorder()

		handlerToTest.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("invalid token returns 401 Unauthorized", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/protected", nil)
		req.AddCookie(&http.Cookie{Name: authCookieName, Value: "invalid.jwt.token"})
		rec := httptest.NewRecorder()

		handlerToTest.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})
}
