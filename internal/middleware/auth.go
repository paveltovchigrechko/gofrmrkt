package middleware

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/paveltovchigrechko/gofrmrkt/internal/service"
)

type contextKey string

const (
	authCookieName            = "token"
	userIDKey      contextKey = "user_id"
)

var (
	errMissingToken = errors.New("request has no authorization token")
)

type Authenticator struct {
	authService *service.AuthService
}

func NewAuthenticator(authService *service.AuthService) *Authenticator {
	return &Authenticator{
		authService: authService,
	}
}

func (a *Authenticator) UserIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := extractToken(r)
		if token == "" {
			http.Error(w, errMissingToken.Error(), http.StatusUnauthorized)
			return
		}

		userID, err := a.authService.GetUserID(token)
		if err != nil {
			if errors.Is(err, service.ErrInvalidToken) {
				http.Error(w, service.ErrInvalidToken.Error(), http.StatusUnauthorized)
				return
			} else if errors.Is(err, service.ErrUnexpectedSigningMethod) {
				http.Error(w, service.ErrUnexpectedSigningMethod.Error(), http.StatusUnauthorized)
				return
			}
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), userIDKey, userID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func extractToken(r *http.Request) string {
	if cookie, err := r.Cookie(authCookieName); err == nil && cookie.Value != "" {
		return cookie.Value
	}

	// 2. Check Authorization Header
	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		return strings.TrimPrefix(authHeader, "Bearer ")
	}

	return ""
}

// UserIDFromContext extracts the userID from context if present.
func UserIDFromContext(ctx context.Context) (int64, bool) {
	userID, ok := ctx.Value(userIDKey).(int64)
	return userID, ok
}

// ContextWithUserID returns a copy of ctx carrying userID under the same
// key UserIDFromContext reads. Exported so handler tests can simulate an
// already-authenticated request without exercising the JWT/middleware
// pipeline itself.
func ContextWithUserID(ctx context.Context, userID int64) context.Context {
	return context.WithValue(ctx, userIDKey, userID)
}
