package accrual

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetOrderInfo_Success_WithAccrual(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/orders/123", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"order":"123","status":"PROCESSED","accrual":500}`))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	info, retryAfter, err := client.GetOrderInfo(context.Background(), "123")

	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, "123", info.Order)
	assert.Equal(t, "PROCESSED", info.Status)
	require.NotNil(t, info.Accrual)
	assert.Equal(t, 500.0, *info.Accrual)
	assert.Zero(t, retryAfter)
}

func TestGetOrderInfo_Success_NoAccrualField(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"order":"123","status":"REGISTERED"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	info, _, err := client.GetOrderInfo(context.Background(), "123")

	require.NoError(t, err)
	assert.Equal(t, "REGISTERED", info.Status)
	assert.Nil(t, info.Accrual)
}

func TestGetOrderInfo_NotRegistered(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	info, retryAfter, err := client.GetOrderInfo(context.Background(), "123")

	assert.ErrorIs(t, err, ErrOrderNotRegistered)
	assert.Nil(t, info)
	assert.Zero(t, retryAfter)
}

func TestGetOrderInfo_RateLimited_WithRetryAfterHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "5")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	_, retryAfter, err := client.GetOrderInfo(context.Background(), "123")

	assert.ErrorIs(t, err, ErrTooManyRequests)
	assert.Equal(t, 5*time.Second, retryAfter)
}

func TestGetOrderInfo_RateLimited_MissingHeader_DefaultsToOneSecond(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	_, retryAfter, err := client.GetOrderInfo(context.Background(), "123")

	assert.ErrorIs(t, err, ErrTooManyRequests)
	assert.Equal(t, time.Second, retryAfter)
}

func TestGetOrderInfo_RateLimited_MalformedHeader_DefaultsToOneSecond(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "not-a-number")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	_, retryAfter, err := client.GetOrderInfo(context.Background(), "123")

	assert.ErrorIs(t, err, ErrTooManyRequests)
	assert.Equal(t, time.Second, retryAfter)
}

func TestGetOrderInfo_UnexpectedStatus_NotRetried(t *testing.T) {
	var callCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	info, _, err := client.GetOrderInfo(context.Background(), "123")

	require.Error(t, err)
	assert.Nil(t, info)
	assert.Equal(t, int32(1), atomic.LoadInt32(&callCount), "a non-5xx status must not be retried")
}

func TestGetOrderInfo_ServerError_RetriesThenFails(t *testing.T) {
	var callCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	info, _, err := client.GetOrderInfo(context.Background(), "123")

	require.Error(t, err)
	assert.Nil(t, info)
	assert.Contains(t, err.Error(), "500")
	assert.Equal(t, int32(4), atomic.LoadInt32(&callCount), "expected 1 initial attempt + 3 retries")
}

func TestGetOrderInfo_ServerError_RetriesThenSucceeds(t *testing.T) {
	var callCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&callCount, 1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Write([]byte(`{"order":"123","status":"PROCESSED","accrual":100}`))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	info, _, err := client.GetOrderInfo(context.Background(), "123")

	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, int32(2), atomic.LoadInt32(&callCount))
}

func TestIsRetriableAccrualError(t *testing.T) {
	assert.False(t, isRetriableAccrualError(nil))
	assert.False(t, isRetriableAccrualError(errors.New("unrelated")))
	assert.False(t, isRetriableAccrualError(ErrOrderNotRegistered))
	assert.False(t, isRetriableAccrualError(ErrTooManyRequests))
	assert.True(t, isRetriableAccrualError(errTransientAccrualFailure))
	assert.True(t, isRetriableAccrualError(fmt.Errorf("wrapped: %w", errTransientAccrualFailure)))
}

func TestGetOrderInfo_MalformedJSONBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`not json`))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	info, _, err := client.GetOrderInfo(context.Background(), "123")

	require.Error(t, err)
	assert.Nil(t, info)
}

func TestGetOrderInfo_NetworkError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	server.Close()

	client := NewClient(server.URL)
	info, retryAfter, err := client.GetOrderInfo(context.Background(), "123")

	require.Error(t, err)
	assert.Nil(t, info)
	assert.Zero(t, retryAfter)
}

func TestGetOrderInfo_RequestConstructionFailure(t *testing.T) {
	client := NewClient("http://example.com")
	info, _, err := client.GetOrderInfo(context.Background(), "123\n456")

	require.Error(t, err)
	assert.Nil(t, info)
}

func TestParseRetryAfter(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   time.Duration
	}{
		{"valid positive seconds", "5", 5 * time.Second},
		{"empty header", "", time.Second},
		{"non-numeric header", "abc", time.Second},
		{"zero", "0", time.Second},
		{"negative", "-3", time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, parseRetryAfter(tt.header))
		})
	}
}
