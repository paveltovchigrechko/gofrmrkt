package repo

import (
	"context"
	"time"
)

type Order struct {
	Number     string    `json:"number"`
	Status     string    `json:"status"`
	Accrual    *float64  `json:"accrual,omitempty"`
	UploadedAt time.Time `json:"uploaded_at"`
}

type Withdrawal struct {
	Order       string    `json:"order"`
	Sum         float64   `json:"sum"`
	ProcessedAt time.Time `json:"processed_at"`
}

// PendingOrder is a minimal projection used by the accrual-polling worker.
type PendingOrder struct {
	Number string
	UserID int64
}

type Storage interface {
	RegisterUser(ctx context.Context, login, passwordHash string) (int64, error)
	GetUserByLogin(ctx context.Context, login string) (userID int64, passwordHash string, err error)

	UploadOrder(ctx context.Context, userID int64, orderNumber string) error
	GetOrders(ctx context.Context, userID int64) ([]Order, error)

	GetBalance(ctx context.Context, userID int64) (current, withdrawn float64, err error)
	WithdrawBalance(ctx context.Context, userID int64, orderNumber string, sum float64) error
	GetWithdrawals(ctx context.Context, userID int64) ([]Withdrawal, error)

	// Used by the accrual-polling worker, not directly by HTTP handlers.
	GetPendingOrders(ctx context.Context, limit int) ([]PendingOrder, error)
	UpdateOrderStatus(ctx context.Context, orderNumber, status string, accrual *float64) error

	Close() error
}
