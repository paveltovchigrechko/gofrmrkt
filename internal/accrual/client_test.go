package accrual

import (
	"context"
	"net/http"
	"net/http/httptest"
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

func TestGetOrderInfo_UnexpectedStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	info, _, err := client.GetOrderInfo(context.Background(), "123")

	require.Error(t, err)
	assert.Nil(t, info)
	assert.Contains(t, err.Error(), "500")
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
