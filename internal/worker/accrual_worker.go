package worker

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/paveltovchigrechko/gofrmrkt/internal/accrual"
	"github.com/paveltovchigrechko/gofrmrkt/internal/repo"
	"go.uber.org/zap"
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

const (
	defaultPollInterval   = time.Second
	defaultBatchSize      = 100
	defaultWorkerPoolSize = 5 // not specified by the review comment or spec — a starting point, tune against the real accrual system's actual capacity
)

type AccrualWorker struct {
	storage  repo.Storage
	client   AccrualClient
	logger   *zap.SugaredLogger
	interval time.Duration
	batch    int
	poolSize int
	gate     *rateLimitGate
}

func NewAccrualWorker(storage repo.Storage, client AccrualClient, logger *zap.SugaredLogger) *AccrualWorker {
	return &AccrualWorker{
		storage:  storage,
		client:   client,
		logger:   logger,
		interval: defaultPollInterval,
		batch:    defaultBatchSize,
		poolSize: defaultWorkerPoolSize,
		gate:     &rateLimitGate{},
	}
}

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
	if len(orders) == 0 {
		return
	}

	jobs := make(chan repo.PendingOrder, len(orders))
	for _, o := range orders {
		jobs <- o
	}
	close(jobs)

	poolSize := w.poolSize
	if poolSize > len(orders) {
		poolSize = len(orders)
	}

	var wg sync.WaitGroup
	wg.Add(poolSize)
	for i := 0; i < poolSize; i++ {
		go func() {
			defer wg.Done()
			for order := range jobs {
				w.gate.wait(ctx)
				if ctx.Err() != nil {
					return
				}
				w.processOrder(ctx, order)
			}
		}()
	}
	wg.Wait()
}

func (w *AccrualWorker) processOrder(ctx context.Context, order repo.PendingOrder) {
	info, retryAfter, err := w.client.GetOrderInfo(ctx, order.Number)

	switch {
	case err == nil:
		w.applyOrderInfo(ctx, info)

	case errors.Is(err, accrual.ErrTooManyRequests):
		w.logger.Warnw("accrual worker: rate limited, pausing pool", "retry_after", retryAfter)
		w.gate.pause(retryAfter)

	case errors.Is(err, accrual.ErrOrderNotRegistered):
		// not yet known to accrual; try again next poll

	default:
		w.logger.Errorw("accrual worker: query order", "order", order.Number, "error", err)
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

type AccrualClient interface {
	GetOrderInfo(ctx context.Context, orderNumber string) (*accrual.OrderInfo, time.Duration, error)
}

var _ AccrualClient = (*accrual.Client)(nil)

type rateLimitGate struct {
	mu       sync.Mutex
	resumeAt time.Time
}

func (g *rateLimitGate) wait(ctx context.Context) {
	for {
		g.mu.Lock()
		resumeAt := g.resumeAt
		g.mu.Unlock()

		remaining := time.Until(resumeAt)
		if remaining <= 0 {
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(remaining):
			// loop: another worker may have extended the pause meanwhile
		}
	}
}

// pause extends the shared pause window to at least now+d, without
// shortening a longer pause already set by another worker's 429.
func (g *rateLimitGate) pause(d time.Duration) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if candidate := time.Now().Add(d); candidate.After(g.resumeAt) {
		g.resumeAt = candidate
	}
}
