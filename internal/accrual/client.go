package accrual

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/paveltovchigrechko/gofrmrkt/internal/service/retry"
)

var (
	ErrOrderNotRegistered      = errors.New("order is not registered in the accrual system")
	ErrTooManyRequests         = errors.New("accrual system rate limit exceeded")
	errTransientAccrualFailure = errors.New("transient accrual system failure")
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
// Network failures and 5xx responses are retried internally with backoff
// before being surfaced, since they're usually transient; other outcomes
// (ErrOrderNotRegistered, ErrTooManyRequests, a non-5xx unexpected status,
// or a malformed response body) are returned immediately.
//
// A non-zero retryAfter (returned only alongside ErrTooManyRequests) tells
// the caller how long to pause ALL further accrual requests, not just this
// one order — 429 here is a system-wide rate limit, not a per-order error.
func (c *Client) GetOrderInfo(ctx context.Context, orderNumber string) (*OrderInfo, time.Duration, error) {
	url := fmt.Sprintf("%s/api/orders/%s", c.baseURL, orderNumber)

	var (
		info       *OrderInfo
		retryAfter time.Duration
	)

	err := retry.Do(ctx, isRetriableAccrualError, func() error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err // malformed request: deterministic, not transient
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("%w: %v", errTransientAccrualFailure, err)
		}
		defer resp.Body.Close()

		switch {
		case resp.StatusCode == http.StatusOK:
			var decoded OrderInfo
			if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
				return err // malformed body isn't a transient server condition
			}
			info = &decoded
			return nil

		case resp.StatusCode == http.StatusNoContent:
			return ErrOrderNotRegistered

		case resp.StatusCode == http.StatusTooManyRequests:
			retryAfter = parseRetryAfter(resp.Header.Get("Retry-After"))
			return ErrTooManyRequests

		case resp.StatusCode >= 500 && resp.StatusCode <= 599:
			return fmt.Errorf("%w: accrual system returned status %d", errTransientAccrualFailure, resp.StatusCode)

		default:
			return fmt.Errorf("accrual system returned unexpected status %d", resp.StatusCode)
		}
	})

	return info, retryAfter, err
}

func isRetriableAccrualError(err error) bool {
	return errors.Is(err, errTransientAccrualFailure)
}

func parseRetryAfter(header string) time.Duration {
	seconds, err := strconv.Atoi(header)
	if err != nil || seconds <= 0 {
		return time.Second
	}
	return time.Duration(seconds) * time.Second
}
