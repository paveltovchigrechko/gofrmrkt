package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/paveltovchigrechko/gofrmrkt/internal/middleware"
	"github.com/paveltovchigrechko/gofrmrkt/internal/repo"
	"github.com/paveltovchigrechko/gofrmrkt/internal/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type mockStorage struct {
	registerUserFn    func(ctx context.Context, login, passwordHash string) (int64, error)
	getUserByLoginFn  func(ctx context.Context, login string) (int64, string, error)
	uploadOrderFn     func(ctx context.Context, userID int64, orderNumber string) error
	getOrdersFn       func(ctx context.Context, userID int64) ([]repo.Order, error)
	getBalanceFn      func(ctx context.Context, userID int64) (float64, float64, error)
	withdrawBalanceFn func(ctx context.Context, userID int64, orderNumber string, sum float64) error
	getWithdrawalsFn  func(ctx context.Context, userID int64) ([]repo.Withdrawal, error)
}

func (m *mockStorage) RegisterUser(ctx context.Context, login, passwordHash string) (int64, error) {
	return m.registerUserFn(ctx, login, passwordHash)
}
func (m *mockStorage) GetUserByLogin(ctx context.Context, login string) (int64, string, error) {
	return m.getUserByLoginFn(ctx, login)
}
func (m *mockStorage) UploadOrder(ctx context.Context, userID int64, orderNumber string) error {
	return m.uploadOrderFn(ctx, userID, orderNumber)
}
func (m *mockStorage) GetOrders(ctx context.Context, userID int64) ([]repo.Order, error) {
	return m.getOrdersFn(ctx, userID)
}
func (m *mockStorage) GetBalance(ctx context.Context, userID int64) (float64, float64, error) {
	return m.getBalanceFn(ctx, userID)
}
func (m *mockStorage) WithdrawBalance(ctx context.Context, userID int64, orderNumber string, sum float64) error {
	return m.withdrawBalanceFn(ctx, userID, orderNumber, sum)
}
func (m *mockStorage) GetWithdrawals(ctx context.Context, userID int64) ([]repo.Withdrawal, error) {
	return m.getWithdrawalsFn(ctx, userID)
}
func (m *mockStorage) GetPendingOrders(ctx context.Context, limit int) ([]repo.PendingOrder, error) {
	return nil, nil
}
func (m *mockStorage) UpdateOrderStatus(ctx context.Context, orderNumber, status string, accrual *float64) error {
	return nil
}
func (m *mockStorage) Close() error { return nil }

func newTestHandler(t *testing.T, storage *mockStorage) *AppHandler {
	t.Helper()

	authSvc, err := service.NewAuthService("test-secret", time.Hour)
	require.NoError(t, err)

	return New(storage, zap.NewNop().Sugar(), authSvc)
}

func jsonRequest(method, path, body string) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

func authedRequest(req *http.Request, userID int64) *http.Request {
	return req.WithContext(middleware.ContextWithUserID(req.Context(), userID))
}

type errReader struct{}

func (errReader) Read(p []byte) (int, error) { return 0, errors.New("read failure") }

func TestRegisterUser_Success(t *testing.T) {
	h := newTestHandler(t, &mockStorage{
		registerUserFn: func(ctx context.Context, login, passwordHash string) (int64, error) {
			return 1, nil
		},
	})

	req := jsonRequest(http.MethodPost, "/api/user/register", `{"login":"alice","password":"secret123"}`)
	rec := httptest.NewRecorder()

	h.RegisterUser(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, rec.Result().Cookies(), 1)
	assert.Equal(t, authCookieName, rec.Result().Cookies()[0].Name)
}

func TestRegisterUser_WrongContentType(t *testing.T) {
	h := newTestHandler(t, &mockStorage{})

	req := httptest.NewRequest(http.MethodPost, "/api/user/register", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()

	h.RegisterUser(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestRegisterUser_InvalidJSON(t *testing.T) {
	h := newTestHandler(t, &mockStorage{})

	req := jsonRequest(http.MethodPost, "/api/user/register", `not json`)
	rec := httptest.NewRecorder()

	h.RegisterUser(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestRegisterUser_EmptyCredentials(t *testing.T) {
	h := newTestHandler(t, &mockStorage{})

	req := jsonRequest(http.MethodPost, "/api/user/register", `{"login":"","password":"secret"}`)
	rec := httptest.NewRecorder()

	h.RegisterUser(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestRegisterUser_PasswordTooLongForBcrypt(t *testing.T) {
	h := newTestHandler(t, &mockStorage{})

	longPassword := strings.Repeat("a", 100)
	req := jsonRequest(http.MethodPost, "/api/user/register", `{"login":"alice","password":"`+longPassword+`"}`)
	rec := httptest.NewRecorder()

	h.RegisterUser(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestRegisterUser_LoginAlreadyExists(t *testing.T) {
	h := newTestHandler(t, &mockStorage{
		registerUserFn: func(ctx context.Context, login, passwordHash string) (int64, error) {
			return 0, repo.ErrUserLoginExist
		},
	})

	req := jsonRequest(http.MethodPost, "/api/user/register", `{"login":"alice","password":"secret123"}`)
	rec := httptest.NewRecorder()

	h.RegisterUser(rec, req)

	assert.Equal(t, http.StatusConflict, rec.Code)
}

func TestRegisterUser_OtherDBError(t *testing.T) {
	h := newTestHandler(t, &mockStorage{
		registerUserFn: func(ctx context.Context, login, passwordHash string) (int64, error) {
			return 0, errors.New("db down")
		},
	})

	req := jsonRequest(http.MethodPost, "/api/user/register", `{"login":"alice","password":"secret123"}`)
	rec := httptest.NewRecorder()

	h.RegisterUser(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestAuthenticateUser_Success(t *testing.T) {
	hash, err := service.HashPassword("correct-password")
	require.NoError(t, err)

	h := newTestHandler(t, &mockStorage{
		getUserByLoginFn: func(ctx context.Context, login string) (int64, string, error) {
			return 1, hash, nil
		},
	})

	req := jsonRequest(http.MethodPost, "/api/user/login", `{"login":"alice","password":"correct-password"}`)
	rec := httptest.NewRecorder()

	h.AuthenticateUser(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, rec.Result().Cookies(), 1)
}

func TestAuthenticateUser_WrongContentType(t *testing.T) {
	h := newTestHandler(t, &mockStorage{})

	req := httptest.NewRequest(http.MethodPost, "/api/user/login", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()

	h.AuthenticateUser(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestAuthenticateUser_InvalidJSON(t *testing.T) {
	h := newTestHandler(t, &mockStorage{})

	req := jsonRequest(http.MethodPost, "/api/user/login", `not json`)
	rec := httptest.NewRecorder()

	h.AuthenticateUser(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestAuthenticateUser_UnknownLogin(t *testing.T) {
	h := newTestHandler(t, &mockStorage{
		getUserByLoginFn: func(ctx context.Context, login string) (int64, string, error) {
			return 0, "", repo.ErrUserNotFound
		},
	})

	req := jsonRequest(http.MethodPost, "/api/user/login", `{"login":"ghost","password":"whatever"}`)
	rec := httptest.NewRecorder()

	h.AuthenticateUser(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestAuthenticateUser_WrongPassword(t *testing.T) {
	hash, err := service.HashPassword("correct-password")
	require.NoError(t, err)

	h := newTestHandler(t, &mockStorage{
		getUserByLoginFn: func(ctx context.Context, login string) (int64, string, error) {
			return 1, hash, nil
		},
	})

	req := jsonRequest(http.MethodPost, "/api/user/login", `{"login":"alice","password":"wrong-password"}`)
	rec := httptest.NewRecorder()

	h.AuthenticateUser(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestAuthenticateUser_OtherDBError(t *testing.T) {
	h := newTestHandler(t, &mockStorage{
		getUserByLoginFn: func(ctx context.Context, login string) (int64, string, error) {
			return 0, "", errors.New("db down")
		},
	})

	req := jsonRequest(http.MethodPost, "/api/user/login", `{"login":"alice","password":"whatever"}`)
	rec := httptest.NewRecorder()

	h.AuthenticateUser(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestUploadOrder_MissingUserID(t *testing.T) {
	h := newTestHandler(t, &mockStorage{})

	req := httptest.NewRequest(http.MethodPost, "/api/user/orders", strings.NewReader("12345678903"))
	rec := httptest.NewRecorder()

	h.UploadOrder(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestUploadOrder_BodyReadError(t *testing.T) {
	h := newTestHandler(t, &mockStorage{})

	req := authedRequest(httptest.NewRequest(http.MethodPost, "/api/user/orders", errReader{}), 1)
	rec := httptest.NewRecorder()

	h.UploadOrder(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestUploadOrder_EmptyBody(t *testing.T) {
	h := newTestHandler(t, &mockStorage{})

	req := authedRequest(httptest.NewRequest(http.MethodPost, "/api/user/orders", strings.NewReader("   ")), 1)
	rec := httptest.NewRecorder()

	h.UploadOrder(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestUploadOrder_InvalidLuhn(t *testing.T) {
	h := newTestHandler(t, &mockStorage{})

	req := authedRequest(httptest.NewRequest(http.MethodPost, "/api/user/orders", strings.NewReader("1234567890")), 1)
	rec := httptest.NewRecorder()

	h.UploadOrder(rec, req)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestUploadOrder_New(t *testing.T) {
	h := newTestHandler(t, &mockStorage{
		uploadOrderFn: func(ctx context.Context, userID int64, orderNumber string) error {
			return nil
		},
	})

	req := authedRequest(httptest.NewRequest(http.MethodPost, "/api/user/orders", strings.NewReader("12345678903")), 1)
	rec := httptest.NewRecorder()

	h.UploadOrder(rec, req)

	assert.Equal(t, http.StatusAccepted, rec.Code)
}

func TestUploadOrder_AlreadyUploadedBySameUser(t *testing.T) {
	h := newTestHandler(t, &mockStorage{
		uploadOrderFn: func(ctx context.Context, userID int64, orderNumber string) error {
			return repo.ErrOrderAlreadyUploaded
		},
	})

	req := authedRequest(httptest.NewRequest(http.MethodPost, "/api/user/orders", strings.NewReader("12345678903")), 1)
	rec := httptest.NewRecorder()

	h.UploadOrder(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestUploadOrder_OwnedByAnotherUser(t *testing.T) {
	h := newTestHandler(t, &mockStorage{
		uploadOrderFn: func(ctx context.Context, userID int64, orderNumber string) error {
			return repo.ErrOrderOwnedByAnotherUser
		},
	})

	req := authedRequest(httptest.NewRequest(http.MethodPost, "/api/user/orders", strings.NewReader("12345678903")), 1)
	rec := httptest.NewRecorder()

	h.UploadOrder(rec, req)

	assert.Equal(t, http.StatusConflict, rec.Code)
}

func TestUploadOrder_OtherDBError(t *testing.T) {
	h := newTestHandler(t, &mockStorage{
		uploadOrderFn: func(ctx context.Context, userID int64, orderNumber string) error {
			return errors.New("db down")
		},
	})

	req := authedRequest(httptest.NewRequest(http.MethodPost, "/api/user/orders", strings.NewReader("12345678903")), 1)
	rec := httptest.NewRecorder()

	h.UploadOrder(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestGetOrders_MissingUserID(t *testing.T) {
	h := newTestHandler(t, &mockStorage{})

	req := httptest.NewRequest(http.MethodGet, "/api/user/orders", nil)
	rec := httptest.NewRecorder()

	h.GetOrders(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestGetOrders_DBError(t *testing.T) {
	h := newTestHandler(t, &mockStorage{
		getOrdersFn: func(ctx context.Context, userID int64) ([]repo.Order, error) {
			return nil, errors.New("db down")
		},
	})

	req := authedRequest(httptest.NewRequest(http.MethodGet, "/api/user/orders", nil), 1)
	rec := httptest.NewRecorder()

	h.GetOrders(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestGetOrders_Empty(t *testing.T) {
	h := newTestHandler(t, &mockStorage{
		getOrdersFn: func(ctx context.Context, userID int64) ([]repo.Order, error) {
			return nil, nil
		},
	})

	req := authedRequest(httptest.NewRequest(http.MethodGet, "/api/user/orders", nil), 1)
	rec := httptest.NewRecorder()

	h.GetOrders(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
}

func TestGetOrders_NonEmpty(t *testing.T) {
	h := newTestHandler(t, &mockStorage{
		getOrdersFn: func(ctx context.Context, userID int64) ([]repo.Order, error) {
			return []repo.Order{{Number: "12345678903", Status: "NEW"}}, nil
		},
	})

	req := authedRequest(httptest.NewRequest(http.MethodGet, "/api/user/orders", nil), 1)
	rec := httptest.NewRecorder()

	h.GetOrders(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "12345678903")
}

func TestGetBalance_MissingUserID(t *testing.T) {
	h := newTestHandler(t, &mockStorage{})

	req := httptest.NewRequest(http.MethodGet, "/api/user/balance", nil)
	rec := httptest.NewRecorder()

	h.GetBalance(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestGetBalance_DBError(t *testing.T) {
	h := newTestHandler(t, &mockStorage{
		getBalanceFn: func(ctx context.Context, userID int64) (float64, float64, error) {
			return 0, 0, errors.New("db down")
		},
	})

	req := authedRequest(httptest.NewRequest(http.MethodGet, "/api/user/balance", nil), 1)
	rec := httptest.NewRecorder()

	h.GetBalance(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestGetBalance_Success(t *testing.T) {
	h := newTestHandler(t, &mockStorage{
		getBalanceFn: func(ctx context.Context, userID int64) (float64, float64, error) {
			return 500.5, 42, nil
		},
	})

	req := authedRequest(httptest.NewRequest(http.MethodGet, "/api/user/balance", nil), 1)
	rec := httptest.NewRecorder()

	h.GetBalance(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "500.5")
}

func TestWithdrawBalance_MissingUserID(t *testing.T) {
	h := newTestHandler(t, &mockStorage{})

	req := jsonRequest(http.MethodPost, "/api/user/balance/withdraw", `{"order":"12345678903","sum":100}`)
	rec := httptest.NewRecorder()

	h.WithdrawBalance(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestWithdrawBalance_WrongContentType(t *testing.T) {
	h := newTestHandler(t, &mockStorage{})

	req := authedRequest(httptest.NewRequest(http.MethodPost, "/api/user/balance/withdraw", strings.NewReader(`{}`)), 1)
	rec := httptest.NewRecorder()

	h.WithdrawBalance(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestWithdrawBalance_InvalidJSON(t *testing.T) {
	h := newTestHandler(t, &mockStorage{})

	req := authedRequest(jsonRequest(http.MethodPost, "/api/user/balance/withdraw", `not json`), 1)
	rec := httptest.NewRecorder()

	h.WithdrawBalance(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestWithdrawBalance_InvalidOrderNumber(t *testing.T) {
	h := newTestHandler(t, &mockStorage{})

	req := authedRequest(jsonRequest(http.MethodPost, "/api/user/balance/withdraw", `{"order":"1234567890","sum":100}`), 1)
	rec := httptest.NewRecorder()

	h.WithdrawBalance(rec, req)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestWithdrawBalance_NonPositiveSum(t *testing.T) {
	h := newTestHandler(t, &mockStorage{})

	req := authedRequest(jsonRequest(http.MethodPost, "/api/user/balance/withdraw", `{"order":"12345678903","sum":0}`), 1)
	rec := httptest.NewRecorder()

	h.WithdrawBalance(rec, req)

	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestWithdrawBalance_Success(t *testing.T) {
	h := newTestHandler(t, &mockStorage{
		withdrawBalanceFn: func(ctx context.Context, userID int64, orderNumber string, sum float64) error {
			return nil
		},
	})

	req := authedRequest(jsonRequest(http.MethodPost, "/api/user/balance/withdraw", `{"order":"12345678903","sum":100}`), 1)
	rec := httptest.NewRecorder()

	h.WithdrawBalance(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestWithdrawBalance_InsufficientFunds(t *testing.T) {
	h := newTestHandler(t, &mockStorage{
		withdrawBalanceFn: func(ctx context.Context, userID int64, orderNumber string, sum float64) error {
			return repo.ErrInsufficientBalance
		},
	})

	req := authedRequest(jsonRequest(http.MethodPost, "/api/user/balance/withdraw", `{"order":"12345678903","sum":100}`), 1)
	rec := httptest.NewRecorder()

	h.WithdrawBalance(rec, req)

	assert.Equal(t, http.StatusPaymentRequired, rec.Code)
}

func TestWithdrawBalance_OtherDBError(t *testing.T) {
	h := newTestHandler(t, &mockStorage{
		withdrawBalanceFn: func(ctx context.Context, userID int64, orderNumber string, sum float64) error {
			return errors.New("db down")
		},
	})

	req := authedRequest(jsonRequest(http.MethodPost, "/api/user/balance/withdraw", `{"order":"12345678903","sum":100}`), 1)
	rec := httptest.NewRecorder()

	h.WithdrawBalance(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestGetWithdrawals_MissingUserID(t *testing.T) {
	h := newTestHandler(t, &mockStorage{})

	req := httptest.NewRequest(http.MethodGet, "/api/user/withdrawals", nil)
	rec := httptest.NewRecorder()

	h.GetWithdrawals(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestGetWithdrawals_DBError(t *testing.T) {
	h := newTestHandler(t, &mockStorage{
		getWithdrawalsFn: func(ctx context.Context, userID int64) ([]repo.Withdrawal, error) {
			return nil, errors.New("db down")
		},
	})

	req := authedRequest(httptest.NewRequest(http.MethodGet, "/api/user/withdrawals", nil), 1)
	rec := httptest.NewRecorder()

	h.GetWithdrawals(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestGetWithdrawals_Empty(t *testing.T) {
	h := newTestHandler(t, &mockStorage{
		getWithdrawalsFn: func(ctx context.Context, userID int64) ([]repo.Withdrawal, error) {
			return nil, nil
		},
	})

	req := authedRequest(httptest.NewRequest(http.MethodGet, "/api/user/withdrawals", nil), 1)
	rec := httptest.NewRecorder()

	h.GetWithdrawals(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
}

func TestGetWithdrawals_NonEmpty(t *testing.T) {
	h := newTestHandler(t, &mockStorage{
		getWithdrawalsFn: func(ctx context.Context, userID int64) ([]repo.Withdrawal, error) {
			return []repo.Withdrawal{{Order: "12345678903", Sum: 100}}, nil
		},
	})

	req := authedRequest(httptest.NewRequest(http.MethodGet, "/api/user/withdrawals", nil), 1)
	rec := httptest.NewRecorder()

	h.GetWithdrawals(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "12345678903")
}
