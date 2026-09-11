package worker

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/paveltovchigrechko/gofrmrkt/internal/accrual"
	"github.com/paveltovchigrechko/gofrmrkt/internal/repo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type mockStorage struct {
	mu sync.Mutex

	getPendingOrdersFn func(ctx context.Context, limit int) ([]repo.PendingOrder, error)

	updateCalls []updateCall
	updateErr   error
}

type updateCall struct {
	orderNumber string
	status      string
	accrual     *float64
}

func (m *mockStorage) GetPendingOrders(ctx context.Context, limit int) ([]repo.PendingOrder, error) {
	return m.getPendingOrdersFn(ctx, limit)
}

func (m *mockStorage) UpdateOrderStatus(ctx context.Context, orderNumber, status string, accrual *float64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.updateCalls = append(m.updateCalls, updateCall{orderNumber, status, accrual})
	return m.updateErr
}

func (m *mockStorage) calls() []updateCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]updateCall(nil), m.updateCalls...)
}

func (m *mockStorage) RegisterUser(ctx context.Context, login, passwordHash string) (int64, error) {
	return 0, nil
}
func (m *mockStorage) GetUserByLogin(ctx context.Context, login string) (int64, string, error) {
	return 0, "", nil
}
func (m *mockStorage) UploadOrder(ctx context.Context, userID int64, orderNumber string) error {
	return nil
}
func (m *mockStorage) GetOrders(ctx context.Context, userID int64) ([]repo.Order, error) {
	return nil, nil
}
func (m *mockStorage) GetBalance(ctx context.Context, userID int64) (float64, float64, error) {
	return 0, 0, nil
}
func (m *mockStorage) WithdrawBalance(ctx context.Context, userID int64, orderNumber string, sum float64) error {
	return nil
}
func (m *mockStorage) GetWithdrawals(ctx context.Context, userID int64) ([]repo.Withdrawal, error) {
	return nil, nil
}
func (m *mockStorage) Close() error { return nil }

func newTestWorker(storage repo.Storage, accrualBaseURL string) *AccrualWorker {
	return &AccrualWorker{
		storage:  storage,
		client:   accrual.NewClient(accrualBaseURL),
		logger:   zap.NewNop().Sugar(),
		interval: 10 * time.Millisecond,
		batch:    10,
	}
}

func pathAccrualServer(t *testing.T, responses map[string]func(w http.ResponseWriter)) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	hit := map[string]bool{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hit[r.URL.Path] = true
		mu.Unlock()

		respond, ok := responses[r.URL.Path]
		if !ok {
			t.Errorf("unexpected request to %s", r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		respond(w)
	}))

	t.Cleanup(server.Close)
	return server
}

func neverCalledServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("accrual system should not have been called, got request to %s", r.URL.Path)
	}))
	t.Cleanup(server.Close)
	return server
}

func TestPollOnce_GetPendingOrdersError_NoClientCalls(t *testing.T) {
	server := neverCalledServer(t)
	storage := &mockStorage{
		getPendingOrdersFn: func(ctx context.Context, limit int) ([]repo.PendingOrder, error) {
			return nil, errors.New("db down")
		},
	}
	w := newTestWorker(storage, server.URL)

	w.pollOnce(context.Background())
}

func TestPollOnce_NoPendingOrders_NoClientCalls(t *testing.T) {
	server := neverCalledServer(t)
	storage := &mockStorage{
		getPendingOrdersFn: func(ctx context.Context, limit int) ([]repo.PendingOrder, error) {
			return nil, nil
		},
	}
	w := newTestWorker(storage, server.URL)

	w.pollOnce(context.Background())
}

func TestPollOnce_Processed_UpdatesStatusWithAccrual(t *testing.T) {
	server := pathAccrualServer(t, map[string]func(w http.ResponseWriter){
		"/api/orders/123": func(w http.ResponseWriter) {
			w.Write([]byte(`{"order":"123","status":"PROCESSED","accrual":500}`))
		},
	})
	storage := &mockStorage{
		getPendingOrdersFn: func(ctx context.Context, limit int) ([]repo.PendingOrder, error) {
			return []repo.PendingOrder{{Number: "123", UserID: 1}}, nil
		},
	}
	w := newTestWorker(storage, server.URL)

	w.pollOnce(context.Background())

	calls := storage.calls()
	require.Len(t, calls, 1)
	assert.Equal(t, "123", calls[0].orderNumber)
	assert.Equal(t, "PROCESSED", calls[0].status)
	require.NotNil(t, calls[0].accrual)
	assert.Equal(t, 500.0, *calls[0].accrual)
}

func TestPollOnce_Registered_MapsToProcessing(t *testing.T) {
	server := pathAccrualServer(t, map[string]func(w http.ResponseWriter){
		"/api/orders/123": func(w http.ResponseWriter) {
			w.Write([]byte(`{"order":"123","status":"REGISTERED"}`))
		},
	})
	storage := &mockStorage{
		getPendingOrdersFn: func(ctx context.Context, limit int) ([]repo.PendingOrder, error) {
			return []repo.PendingOrder{{Number: "123", UserID: 1}}, nil
		},
	}
	w := newTestWorker(storage, server.URL)

	w.pollOnce(context.Background())

	calls := storage.calls()
	require.Len(t, calls, 1)
	assert.Equal(t, "PROCESSING", calls[0].status)
	assert.Nil(t, calls[0].accrual)
}

func TestPollOnce_UnknownStatus_DoesNotUpdate(t *testing.T) {
	server := pathAccrualServer(t, map[string]func(w http.ResponseWriter){
		"/api/orders/123": func(w http.ResponseWriter) {
			w.Write([]byte(`{"order":"123","status":"BOGUS"}`))
		},
	})
	storage := &mockStorage{
		getPendingOrdersFn: func(ctx context.Context, limit int) ([]repo.PendingOrder, error) {
			return []repo.PendingOrder{{Number: "123", UserID: 1}}, nil
		},
	}
	w := newTestWorker(storage, server.URL)

	w.pollOnce(context.Background())

	assert.Empty(t, storage.calls())
}

func TestPollOnce_NotRegistered_DoesNotUpdate(t *testing.T) {
	server := pathAccrualServer(t, map[string]func(w http.ResponseWriter){
		"/api/orders/123": func(w http.ResponseWriter) {
			w.WriteHeader(http.StatusNoContent)
		},
	})
	storage := &mockStorage{
		getPendingOrdersFn: func(ctx context.Context, limit int) ([]repo.PendingOrder, error) {
			return []repo.PendingOrder{{Number: "123", UserID: 1}}, nil
		},
	}
	w := newTestWorker(storage, server.URL)

	w.pollOnce(context.Background())

	assert.Empty(t, storage.calls())
}

func TestPollOnce_UpdateStatusError_DoesNotPanic(t *testing.T) {
	server := pathAccrualServer(t, map[string]func(w http.ResponseWriter){
		"/api/orders/123": func(w http.ResponseWriter) {
			w.Write([]byte(`{"order":"123","status":"PROCESSED","accrual":100}`))
		},
	})
	storage := &mockStorage{
		getPendingOrdersFn: func(ctx context.Context, limit int) ([]repo.PendingOrder, error) {
			return []repo.PendingOrder{{Number: "123", UserID: 1}}, nil
		},
		updateErr: errors.New("db write failed"),
	}
	w := newTestWorker(storage, server.URL)

	w.pollOnce(context.Background()) // just must not panic
	assert.Len(t, storage.calls(), 1)
}

func TestPollOnce_MultipleOrders_ProcessesAllWhenNoRateLimit(t *testing.T) {
	server := pathAccrualServer(t, map[string]func(w http.ResponseWriter){
		"/api/orders/111": func(w http.ResponseWriter) {
			w.Write([]byte(`{"order":"111","status":"PROCESSED","accrual":100}`))
		},
		"/api/orders/222": func(w http.ResponseWriter) {
			w.Write([]byte(`{"order":"222","status":"INVALID"}`))
		},
	})
	storage := &mockStorage{
		getPendingOrdersFn: func(ctx context.Context, limit int) ([]repo.PendingOrder, error) {
			return []repo.PendingOrder{{Number: "111"}, {Number: "222"}}, nil
		},
	}
	w := newTestWorker(storage, server.URL)

	w.pollOnce(context.Background())

	calls := storage.calls()
	require.Len(t, calls, 2)
	assert.Equal(t, "111", calls[0].orderNumber)
	assert.Equal(t, "222", calls[1].orderNumber)
	assert.Equal(t, "INVALID", calls[1].status)
}

func TestPollOnce_RateLimited_StopsBatchAndReturnsEarlyOnCancellation(t *testing.T) {
	server := pathAccrualServer(t, map[string]func(w http.ResponseWriter){
		"/api/orders/111": func(w http.ResponseWriter) {
			w.Header().Set("Retry-After", "10")
			w.WriteHeader(http.StatusTooManyRequests)
		},
	})
	storage := &mockStorage{
		getPendingOrdersFn: func(ctx context.Context, limit int) ([]repo.PendingOrder, error) {
			return []repo.PendingOrder{{Number: "111"}, {Number: "222"}}, nil
		},
	}
	w := newTestWorker(storage, server.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	start := time.Now()
	w.pollOnce(ctx)
	elapsed := time.Since(start)

	assert.Less(t, elapsed, 500*time.Millisecond, "pollOnce should return promptly on context cancellation, not wait out the full Retry-After")
	assert.Empty(t, storage.calls(), "no order should have its status updated when rate-limited")
}

func TestRun_PollsRepeatedlyUntilContextCanceled(t *testing.T) {
	var mu sync.Mutex
	pollCount := 0

	storage := &mockStorage{
		getPendingOrdersFn: func(ctx context.Context, limit int) ([]repo.PendingOrder, error) {
			mu.Lock()
			pollCount++
			mu.Unlock()
			return nil, nil
		},
	}

	w := &AccrualWorker{
		storage:  storage,
		client:   accrual.NewClient("http://unused.invalid"),
		logger:   zap.NewNop().Sugar(),
		interval: 10 * time.Millisecond,
		batch:    10,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 55*time.Millisecond)
	defer cancel()

	start := time.Now()
	w.Run(ctx)
	elapsed := time.Since(start)

	mu.Lock()
	count := pollCount
	mu.Unlock()

	assert.GreaterOrEqual(t, count, 2, "expected at least 2 polls in the given window")
	assert.Less(t, elapsed, 200*time.Millisecond, "Run should return promptly after context cancellation")
}
