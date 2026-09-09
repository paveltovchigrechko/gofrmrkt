package accrual

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

var (
	ErrOrderNotRegistered = errors.New("order is not registered in the accrual system")
	ErrTooManyRequests    = errors.New("accrual system rate limit exceeded")
)

// OrderInfo mirrors the accrual system's own response shape from
// GET /api/orders/{number}. Its Status vocabulary (REGISTERED/PROCESSING/
// INVALID/PROCESSED) is the accrual system's own — distinct from the
// NEW/PROCESSING/INVALID/PROCESSED statuses gophermart exposes to its
// clients. Mapping between the two is the worker's job, not this client's.
type OrderInfo struct {
	Order   string   `json:"order"`
	Status  string   `json:"status"`
	Accrual *float64 `json:"accrual,omitempty"`
}

type Client struct {
	baseURL    string
	httpClient *http.Client
}

func NewClient(baseURL string) *Client {
	return &Client{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
}

// GetOrderInfo queries the accrual system for orderNumber's current status.
// A non-zero retryAfter (returned only alongside ErrTooManyRequests) tells
// the caller how long to pause ALL further accrual requests, not just this
// one order — 429 here is a system-wide rate limit, not a per-order error.
func (c *Client) GetOrderInfo(ctx context.Context, orderNumber string) (*OrderInfo, time.Duration, error) {
	url := fmt.Sprintf("%s/api/orders/%s", c.baseURL, orderNumber)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		var info OrderInfo
		if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
			return nil, 0, err
		}
		return &info, 0, nil

	case http.StatusNoContent:
		return nil, 0, ErrOrderNotRegistered

	case http.StatusTooManyRequests:
		return nil, parseRetryAfter(resp.Header.Get("Retry-After")), ErrTooManyRequests

	default:
		return nil, 0, fmt.Errorf("accrual system returned unexpected status %d", resp.StatusCode)
	}
}

func parseRetryAfter(header string) time.Duration {
	seconds, err := strconv.Atoi(header)
	if err != nil || seconds <= 0 {
		return time.Second
	}
	return time.Duration(seconds) * time.Second
}
