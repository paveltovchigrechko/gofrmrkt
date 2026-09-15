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

// --- mock storage (unchanged) ---

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

// --- stub accrual client ---

type stubAccrualClient struct {
	getOrderInfoFn func(ctx context.Context, orderNumber string) (*accrual.OrderInfo, time.Duration, error)
}

func (s *stubAccrualClient) GetOrderInfo(ctx context.Context, orderNumber string) (*accrual.OrderInfo, time.Duration, error) {
	return s.getOrderInfoFn(ctx, orderNumber)
}

// neverCalledClient fails the test immediately if the worker ever queries
// the accrual system — for scenarios where it shouldn't reach that far.
func neverCalledClient(t *testing.T) *stubAccrualClient {
	t.Helper()
	return &stubAccrualClient{
		getOrderInfoFn: func(ctx context.Context, orderNumber string) (*accrual.OrderInfo, time.Duration, error) {
			t.Fatalf("accrual client should not have been called, got order %q", orderNumber)
			return nil, 0, nil
		},
	}
}

// mapAccrualClient dispatches by order number, failing the test on any
// order it wasn't told to expect.
func mapAccrualClient(t *testing.T, responses map[string]func() (*accrual.OrderInfo, time.Duration, error)) *stubAccrualClient {
	t.Helper()
	return &stubAccrualClient{
		getOrderInfoFn: func(ctx context.Context, orderNumber string) (*accrual.OrderInfo, time.Duration, error) {
			respond, ok := responses[orderNumber]
			if !ok {
				t.Fatalf("unexpected accrual lookup for order %q", orderNumber)
			}
			return respond()
		},
	}
}

// --- test helper ---

func newTestWorker(storage repo.Storage, client AccrualClient) *AccrualWorker {
	return &AccrualWorker{
		storage:  storage,
		client:   client,
		logger:   zap.NewNop().Sugar(),
		interval: 10 * time.Millisecond,
		batch:    10,
	}
}

func floatPtr(v float64) *float64 { return &v }

// --- pollOnce ---

func TestPollOnce_GetPendingOrdersError_NoClientCalls(t *testing.T) {
	storage := &mockStorage{
		getPendingOrdersFn: func(ctx context.Context, limit int) ([]repo.PendingOrder, error) {
			return nil, errors.New("db down")
		},
	}
	w := newTestWorker(storage, neverCalledClient(t))

	w.pollOnce(context.Background()) // must not panic, must not call the client
}

func TestPollOnce_NoPendingOrders_NoClientCalls(t *testing.T) {
	storage := &mockStorage{
		getPendingOrdersFn: func(ctx context.Context, limit int) ([]repo.PendingOrder, error) {
			return nil, nil
		},
	}
	w := newTestWorker(storage, neverCalledClient(t))

	w.pollOnce(context.Background())
}

func TestPollOnce_Processed_UpdatesStatusWithAccrual(t *testing.T) {
	client := &stubAccrualClient{
		getOrderInfoFn: func(ctx context.Context, orderNumber string) (*accrual.OrderInfo, time.Duration, error) {
			return &accrual.OrderInfo{Order: "123", Status: "PROCESSED", Accrual: floatPtr(500)}, 0, nil
		},
	}
	storage := &mockStorage{
		getPendingOrdersFn: func(ctx context.Context, limit int) ([]repo.PendingOrder, error) {
			return []repo.PendingOrder{{Number: "123", UserID: 1}}, nil
		},
	}
	w := newTestWorker(storage, client)

	w.pollOnce(context.Background())

	calls := storage.calls()
	require.Len(t, calls, 1)
	assert.Equal(t, "123", calls[0].orderNumber)
	assert.Equal(t, "PROCESSED", calls[0].status)
	require.NotNil(t, calls[0].accrual)
	assert.Equal(t, 500.0, *calls[0].accrual)
}

func TestPollOnce_Registered_MapsToProcessing(t *testing.T) {
	client := &stubAccrualClient{
		getOrderInfoFn: func(ctx context.Context, orderNumber string) (*accrual.OrderInfo, time.Duration, error) {
			return &accrual.OrderInfo{Order: "123", Status: "REGISTERED"}, 0, nil
		},
	}
	storage := &mockStorage{
		getPendingOrdersFn: func(ctx context.Context, limit int) ([]repo.PendingOrder, error) {
			return []repo.PendingOrder{{Number: "123", UserID: 1}}, nil
		},
	}
	w := newTestWorker(storage, client)

	w.pollOnce(context.Background())

	calls := storage.calls()
	require.Len(t, calls, 1)
	assert.Equal(t, "PROCESSING", calls[0].status)
	assert.Nil(t, calls[0].accrual)
}

func TestPollOnce_UnknownStatus_DoesNotUpdate(t *testing.T) {
	client := &stubAccrualClient{
		getOrderInfoFn: func(ctx context.Context, orderNumber string) (*accrual.OrderInfo, time.Duration, error) {
			return &accrual.OrderInfo{Order: "123", Status: "BOGUS"}, 0, nil
		},
	}
	storage := &mockStorage{
		getPendingOrdersFn: func(ctx context.Context, limit int) ([]repo.PendingOrder, error) {
			return []repo.PendingOrder{{Number: "123", UserID: 1}}, nil
		},
	}
	w := newTestWorker(storage, client)

	w.pollOnce(context.Background())

	assert.Empty(t, storage.calls())
}

func TestPollOnce_NotRegistered_DoesNotUpdate(t *testing.T) {
	client := &stubAccrualClient{
		getOrderInfoFn: func(ctx context.Context, orderNumber string) (*accrual.OrderInfo, time.Duration, error) {
			return nil, 0, accrual.ErrOrderNotRegistered
		},
	}
	storage := &mockStorage{
		getPendingOrdersFn: func(ctx context.Context, limit int) ([]repo.PendingOrder, error) {
			return []repo.PendingOrder{{Number: "123", UserID: 1}}, nil
		},
	}
	w := newTestWorker(storage, client)

	w.pollOnce(context.Background())

	assert.Empty(t, storage.calls())
}

func TestPollOnce_UpdateStatusError_DoesNotPanic(t *testing.T) {
	client := &stubAccrualClient{
		getOrderInfoFn: func(ctx context.Context, orderNumber string) (*accrual.OrderInfo, time.Duration, error) {
			return &accrual.OrderInfo{Order: "123", Status: "PROCESSED", Accrual: floatPtr(100)}, 0, nil
		},
	}
	storage := &mockStorage{
		getPendingOrdersFn: func(ctx context.Context, limit int) ([]repo.PendingOrder, error) {
			return []repo.PendingOrder{{Number: "123", UserID: 1}}, nil
		},
		updateErr: errors.New("db write failed"),
	}
	w := newTestWorker(storage, client)

	w.pollOnce(context.Background()) // just must not panic
	assert.Len(t, storage.calls(), 1)
}

func TestPollOnce_MultipleOrders_ProcessesAllWhenNoRateLimit(t *testing.T) {
	client := mapAccrualClient(t, map[string]func() (*accrual.OrderInfo, time.Duration, error){
		"111": func() (*accrual.OrderInfo, time.Duration, error) {
			return &accrual.OrderInfo{Order: "111", Status: "PROCESSED", Accrual: floatPtr(100)}, 0, nil
		},
		"222": func() (*accrual.OrderInfo, time.Duration, error) {
			return &accrual.OrderInfo{Order: "222", Status: "INVALID"}, 0, nil
		},
	})
	storage := &mockStorage{
		getPendingOrdersFn: func(ctx context.Context, limit int) ([]repo.PendingOrder, error) {
			return []repo.PendingOrder{{Number: "111"}, {Number: "222"}}, nil
		},
	}
	w := newTestWorker(storage, client)

	w.pollOnce(context.Background())

	calls := storage.calls()
	require.Len(t, calls, 2)
	assert.Equal(t, "111", calls[0].orderNumber)
	assert.Equal(t, "222", calls[1].orderNumber)
	assert.Equal(t, "INVALID", calls[1].status)
}

func TestPollOnce_RateLimited_StopsBatchAndReturnsEarlyOnCancellation(t *testing.T) {
	// Retry-After is large (10s) to prove the test relies on context
	// cancellation to return quickly, not on the wait completing.
	client := mapAccrualClient(t, map[string]func() (*accrual.OrderInfo, time.Duration, error){
		"111": func() (*accrual.OrderInfo, time.Duration, error) {
			return nil, 10 * time.Second, accrual.ErrTooManyRequests
		},
		// "222" intentionally absent: mapAccrualClient fails the test if
		// the worker ever queries it after the 429 on "111".
	})
	storage := &mockStorage{
		getPendingOrdersFn: func(ctx context.Context, limit int) ([]repo.PendingOrder, error) {
			return []repo.PendingOrder{{Number: "111"}, {Number: "222"}}, nil
		},
	}
	w := newTestWorker(storage, client)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	start := time.Now()
	w.pollOnce(ctx)
	elapsed := time.Since(start)

	assert.Less(t, elapsed, 500*time.Millisecond, "pollOnce should return promptly on context cancellation")
	assert.Empty(t, storage.calls())
}

// TestPollOnce_Integration_RealAccrualClient is the one test in this file
// using the real *accrual.Client against a real httptest.Server — proving
// the interface substitution above didn't silently break the true wiring,
// which the stub-based tests above can't verify by themselves.
func TestPollOnce_Integration_RealAccrualClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/orders/123", r.URL.Path)
		w.Write([]byte(`{"order":"123","status":"PROCESSED","accrual":500}`))
	}))
	defer server.Close()

	realClient := accrual.NewClient(server.URL, zap.NewNop().Sugar())
	storage := &mockStorage{
		getPendingOrdersFn: func(ctx context.Context, limit int) ([]repo.PendingOrder, error) {
			return []repo.PendingOrder{{Number: "123", UserID: 1}}, nil
		},
	}
	w := newTestWorker(storage, realClient)

	w.pollOnce(context.Background())

	calls := storage.calls()
	require.Len(t, calls, 1)
	assert.Equal(t, "PROCESSED", calls[0].status)
}

// --- Run ---

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
		client:   neverCalledClient(t),
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
