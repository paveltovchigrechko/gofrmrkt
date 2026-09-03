package server

import (
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/paveltovchigrechko/gofrmrkt/internal/config"
	"github.com/paveltovchigrechko/gofrmrkt/internal/repo"
	"github.com/paveltovchigrechko/gofrmrkt/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// stubStorage is a minimal repo.Storage implementation with per-call
// override hooks; unconfigured methods return zero values.
type stubStorage struct {
	getOrdersFn func(ctx context.Context, userID int64) ([]repo.Order, error)
	closeCalled bool
}

func (s *stubStorage) RegisterUser(ctx context.Context, login, passwordHash string) (int64, error) {
	return 0, nil
}
func (s *stubStorage) GetUserByLogin(ctx context.Context, login string) (int64, string, error) {
	return 0, "", nil
}
func (s *stubStorage) UploadOrder(ctx context.Context, userID int64, orderNumber string) error {
	return nil
}
func (s *stubStorage) GetOrders(ctx context.Context, userID int64) ([]repo.Order, error) {
	if s.getOrdersFn != nil {
		return s.getOrdersFn(ctx, userID)
	}
	return nil, nil
}
func (s *stubStorage) GetBalance(ctx context.Context, userID int64) (float64, float64, error) {
	return 0, 0, nil
}
func (s *stubStorage) WithdrawBalance(ctx context.Context, userID int64, orderNumber string, sum float64) error {
	return nil
}
func (s *stubStorage) GetWithdrawals(ctx context.Context, userID int64) ([]repo.Withdrawal, error) {
	return nil, nil
}
func (s *stubStorage) GetPendingOrders(ctx context.Context, limit int) ([]repo.PendingOrder, error) {
	return nil, nil
}
func (s *stubStorage) UpdateOrderStatus(ctx context.Context, orderNumber, status string, accrual *float64) error {
	return nil
}
func (s *stubStorage) Close() error {
	s.closeCalled = true
	return nil
}

func newTestServer(t *testing.T, storage *stubStorage) *Server {
	t.Helper()
	cfg := &config.AppConfig{Addr: "localhost:0", SecretKey: "test-secret-key-for-server-tests"}
	return newTestServerWithConfig(t, cfg, storage)
}

func newTestServerWithConfig(t *testing.T, cfg *config.AppConfig, storage *stubStorage) *Server {
	t.Helper()

	logger := zap.NewNop().Sugar()
	srv, err := New(cfg, logger, storage)
	require.NoError(t, err)

	return srv
}

func validAuthCookie(t *testing.T, cfg *config.AppConfig, userID int64) *http.Cookie {
	t.Helper()

	authSvc, err := service.NewAuthService(cfg.SecretKey, time.Hour)
	require.NoError(t, err)

	token, err := authSvc.BuildJWTString(userID)
	require.NoError(t, err)

	return &http.Cookie{Name: "token", Value: token}
}

// --- public routes ---

func TestServer_PublicRoutes_NoAuthRequired(t *testing.T) {
	srv := newTestServer(t, &stubStorage{})

	tests := []struct {
		name   string
		method string
		path   string
	}{
		{"register", http.MethodPost, "/api/user/register"},
		{"login", http.MethodPost, "/api/user/login"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader("{}"))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			srv.router.ServeHTTP(rec, req)

			// No token was sent; a public route must not answer 401.
			assert.NotEqual(t, http.StatusUnauthorized, rec.Code)
		})
	}
}

// --- private routes ---

func TestServer_PrivateRoutes_RejectMissingToken(t *testing.T) {
	srv := newTestServer(t, &stubStorage{})

	routes := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/user/orders"},
		{http.MethodGet, "/api/user/orders"},
		{http.MethodGet, "/api/user/balance"},
		{http.MethodPost, "/api/user/balance/withdraw"},
		{http.MethodGet, "/api/user/withdrawals"},
	}

	for _, rt := range routes {
		t.Run(rt.method+" "+rt.path, func(t *testing.T) {
			req := httptest.NewRequest(rt.method, rt.path, nil)
			rec := httptest.NewRecorder()

			srv.router.ServeHTTP(rec, req)

			assert.Equal(t, http.StatusUnauthorized, rec.Code)
		})
	}
}

func TestServer_PrivateRoute_ReachesHandlerWithValidToken(t *testing.T) {
	cfg := &config.AppConfig{Addr: "localhost:0", SecretKey: "test-secret-key-for-server-tests"}
	storage := &stubStorage{
		getOrdersFn: func(ctx context.Context, userID int64) ([]repo.Order, error) {
			return nil, nil // empty -> handler should answer 204, not 401
		},
	}
	srv := newTestServerWithConfig(t, cfg, storage)

	req := httptest.NewRequest(http.MethodGet, "/api/user/orders", nil)
	req.AddCookie(validAuthCookie(t, cfg, 42))
	rec := httptest.NewRecorder()

	srv.router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
}

// --- routing ---

func TestServer_UnknownRoute_Returns404(t *testing.T) {
	srv := newTestServer(t, &stubStorage{})

	req := httptest.NewRequest(http.MethodGet, "/api/does/not/exist", nil)
	rec := httptest.NewRecorder()

	srv.router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// --- gzip wiring ---

func TestServer_GZIPMiddleware_CompressesResponseWhenAccepted(t *testing.T) {
	cfg := &config.AppConfig{Addr: "localhost:0", SecretKey: "test-secret-key-for-server-tests"}
	storage := &stubStorage{
		getOrdersFn: func(ctx context.Context, userID int64) ([]repo.Order, error) {
			return []repo.Order{{Number: "12345678903", Status: "NEW"}}, nil
		},
	}
	srv := newTestServerWithConfig(t, cfg, storage)

	req := httptest.NewRequest(http.MethodGet, "/api/user/orders", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	req.AddCookie(validAuthCookie(t, cfg, 42))
	rec := httptest.NewRecorder()

	srv.router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "gzip", rec.Header().Get("Content-Encoding"))

	gz, err := gzip.NewReader(rec.Body)
	require.NoError(t, err)
	defer gz.Close()

	body, err := io.ReadAll(gz)
	require.NoError(t, err)
	assert.Contains(t, string(body), "12345678903")
}

// --- lifecycle ---

func TestServer_CloseDB_ClosesStorage(t *testing.T) {
	storage := &stubStorage{}
	srv := newTestServer(t, storage)

	require.NoError(t, srv.CloseDB())
	assert.True(t, storage.closeCalled)
}
