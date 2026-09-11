package worker

import (
	"context"
	"errors"
	"time"

	"github.com/paveltovchigrechko/gofrmrkt/internal/accrual"
	"github.com/paveltovchigrechko/gofrmrkt/internal/repo"
	"go.uber.org/zap"
)

const (
	defaultPollInterval = time.Second
	defaultBatchSize    = 100
)

// statusMap translates the accrual system's own status vocabulary into the
// status values gophermart exposes to its own clients via GET
// /api/user/orders. "REGISTERED" has no equivalent in gophermart's own
// enum (NEW/PROCESSING/INVALID/PROCESSED) — orders already start as NEW at
// upload time, so REGISTERED is treated as "accrual has accepted it and is
// working on it," i.e. PROCESSING.
var statusMap = map[string]string{
	"REGISTERED": "PROCESSING",
	"PROCESSING": "PROCESSING",
	"INVALID":    "INVALID",
	"PROCESSED":  "PROCESSED",
}

type AccrualWorker struct {
	storage  repo.Storage
	client   *accrual.Client
	logger   *zap.SugaredLogger
	interval time.Duration
	batch    int
}

func NewAccrualWorker(storage repo.Storage, client *accrual.Client, logger *zap.SugaredLogger) *AccrualWorker {
	return &AccrualWorker{
		storage:  storage,
		client:   client,
		logger:   logger,
		interval: defaultPollInterval,
		batch:    defaultBatchSize,
	}
}

// Run polls pending orders until ctx is canceled. Intended to be launched
// in its own goroutine by the caller.
func (w *AccrualWorker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.pollOnce(ctx)
		}
	}
}

func (w *AccrualWorker) pollOnce(ctx context.Context) {
	orders, err := w.storage.GetPendingOrders(ctx, w.batch)
	if err != nil {
		w.logger.Errorw("accrual worker: fetch pending orders", "error", err)
		return
	}

	for _, order := range orders {
		info, retryAfter, err := w.client.GetOrderInfo(ctx, order.Number)

		switch {
		case err == nil:
			w.applyOrderInfo(ctx, info)

		case errors.Is(err, accrual.ErrTooManyRequests):
			w.logger.Warnw("accrual worker: rate limited, pausing batch", "retry_after", retryAfter)
			select {
			case <-ctx.Done():
			case <-time.After(retryAfter):
			}
			return // stop this batch entirely; resume from the next tick

		case errors.Is(err, accrual.ErrOrderNotRegistered):
			continue // not yet known to accrual; try again next poll

		default:
			w.logger.Errorw("accrual worker: query order", "order", order.Number, "error", err)
		}
	}
}

func (w *AccrualWorker) applyOrderInfo(ctx context.Context, info *accrual.OrderInfo) {
	status, ok := statusMap[info.Status]
	if !ok {
		w.logger.Warnw("accrual worker: unrecognized status from accrual system", "status", info.Status)
		return
	}

	if err := w.storage.UpdateOrderStatus(ctx, info.Order, status, info.Accrual); err != nil {
		w.logger.Errorw("accrual worker: update order status", "order", info.Order, "error", err)
	}
}
