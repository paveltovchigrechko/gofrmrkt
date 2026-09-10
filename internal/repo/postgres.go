package repo

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/paveltovchigrechko/gofrmrkt/internal/service/retry"
)

var (
	ErrUserLoginExist          = errors.New("user with this login already exists")
	ErrUserNotFound            = errors.New("user not found")
	ErrOrderAlreadyUploaded    = errors.New("order already uploaded by this user")
	ErrOrderOwnedByAnotherUser = errors.New("order already uploaded by another user")
	ErrInsufficientBalance     = errors.New("insufficient balance")
)

const (
	databaseDriver = "pgx"
	migrationLabel = "postgres"
	migrationPath  = "file://migrations"
)

// queryer lets shared query logic run against either *sql.DB or *sql.Tx.
type queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type Postgres struct {
	database *sql.DB
}

func NewPostgresDB(databaseDSN string) (*Postgres, error) {
	db, err := openDB(databaseDSN)
	if err != nil {
		return nil, err
	}

	if err := runMigrations(db); err != nil {
		db.Close()
		return nil, err
	}

	return &Postgres{database: db}, nil
}

func (db *Postgres) Close() error {
	return db.database.Close()
}

// --- users ---

func (db *Postgres) RegisterUser(ctx context.Context, login, passwordHash string) (int64, error) {
	query := `
		INSERT INTO users (login, password_hash)
		VALUES ($1, $2)
		RETURNING id
	`
	var userID int64
	err := db.database.QueryRowContext(ctx, query, login, passwordHash).Scan(&userID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation {
			return -1, ErrUserLoginExist
		}
		return -1, err
	}

	return userID, nil
}

func (db *Postgres) GetUserByLogin(ctx context.Context, login string) (int64, string, error) {
	query := `
		SELECT id, password_hash
		FROM users
		WHERE login = $1
	`
	var (
		userID       int64
		passwordHash string
	)
	err := db.database.QueryRowContext(ctx, query, login).Scan(&userID, &passwordHash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return -1, "", ErrUserNotFound
		}
		return -1, "", err
	}

	return userID, passwordHash, nil
}

// --- orders ---

func (db *Postgres) UploadOrder(ctx context.Context, userID int64, orderNumber string) error {
	query := `
		INSERT INTO orders (number, user_id)
		VALUES ($1, $2)
	`
	_, err := db.database.ExecContext(ctx, query, orderNumber, userID)
	if err == nil {
		return nil
	}

	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != pgerrcode.UniqueViolation {
		return err
	}

	ownerID, lookupErr := db.getOrderOwner(ctx, orderNumber)
	if lookupErr != nil {
		return lookupErr
	}
	if ownerID == userID {
		return ErrOrderAlreadyUploaded
	}
	return ErrOrderOwnedByAnotherUser
}

func (db *Postgres) getOrderOwner(ctx context.Context, orderNumber string) (int64, error) {
	var userID int64
	query := `SELECT user_id FROM orders WHERE number = $1`
	err := db.database.QueryRowContext(ctx, query, orderNumber).Scan(&userID)
	return userID, err
}

func (db *Postgres) GetOrders(ctx context.Context, userID int64) ([]Order, error) {
	query := `
		SELECT number, status, accrual, uploaded_at
		FROM orders
		WHERE user_id = $1
		ORDER BY uploaded_at DESC
	`
	rows, err := db.database.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var orders []Order
	for rows.Next() {
		var (
			o       Order
			accrual sql.NullFloat64
		)
		if err := rows.Scan(&o.Number, &o.Status, &accrual, &o.UploadedAt); err != nil {
			return nil, err
		}
		if accrual.Valid {
			v := accrual.Float64
			o.Accrual = &v
		}
		orders = append(orders, o)
	}

	return orders, rows.Err()
}

// --- balance / withdrawals ---

func getBalance(ctx context.Context, q queryer, userID int64) (current, withdrawn float64, err error) {
	query := `
		SELECT
			COALESCE((SELECT SUM(accrual) FROM orders WHERE user_id = $1), 0) -
			COALESCE((SELECT SUM(sum) FROM withdrawals WHERE user_id = $1), 0),
			COALESCE((SELECT SUM(sum) FROM withdrawals WHERE user_id = $1), 0)
	`
	err = q.QueryRowContext(ctx, query, userID).Scan(&current, &withdrawn)
	return current, withdrawn, err
}

func (db *Postgres) GetBalance(ctx context.Context, userID int64) (float64, float64, error) {
	return getBalance(ctx, db.database, userID)
}

func (db *Postgres) WithdrawBalance(ctx context.Context, userID int64, orderNumber string, sum float64) error {
	idempotencyKey, err := generateIdempotencyKey()
	if err != nil {
		return err
	}

	return retry.Do(ctx, isRetriableDBError, func() error {
		return db.attemptWithdraw(ctx, userID, orderNumber, sum, idempotencyKey)
	})
}

func (db *Postgres) GetWithdrawals(ctx context.Context, userID int64) ([]Withdrawal, error) {
	query := `
		SELECT order_number, sum, processed_at
		FROM withdrawals
		WHERE user_id = $1
		ORDER BY processed_at DESC
	`
	rows, err := db.database.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var withdrawals []Withdrawal
	for rows.Next() {
		var w Withdrawal
		if err := rows.Scan(&w.Order, &w.Sum, &w.ProcessedAt); err != nil {
			return nil, err
		}
		withdrawals = append(withdrawals, w)
	}

	return withdrawals, rows.Err()
}

// --- accrual-polling worker support ---

func (db *Postgres) GetPendingOrders(ctx context.Context, limit int) ([]PendingOrder, error) {
	query := `
		SELECT number, user_id
		FROM orders
		WHERE status IN ('NEW', 'PROCESSING')
		ORDER BY uploaded_at
		LIMIT $1
	`
	rows, err := db.database.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var orders []PendingOrder
	for rows.Next() {
		var o PendingOrder
		if err := rows.Scan(&o.Number, &o.UserID); err != nil {
			return nil, err
		}
		orders = append(orders, o)
	}

	return orders, rows.Err()
}

func (db *Postgres) UpdateOrderStatus(ctx context.Context, orderNumber, status string, accrual *float64) error {
	query := `
		UPDATE orders
		SET status = $1, accrual = $2, updated_at = NOW()
		WHERE number = $3
	`
	_, err := db.database.ExecContext(ctx, query, status, accrual, orderNumber)
	return err
}

func openDB(databaseDSN string) (*sql.DB, error) {
	db, err := sql.Open(databaseDriver, databaseDSN)
	if err != nil {
		return nil, err
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}

	return db, nil
}

func runMigrations(db *sql.DB) error {
	driver, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		return err
	}

	m, err := migrate.NewWithDatabaseInstance(migrationPath, databaseDriver, driver)
	if err != nil {
		return err
	}

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return err
	}

	return nil
}

func (db *Postgres) attemptWithdraw(ctx context.Context, userID int64, orderNumber string, sum float64, idempotencyKey string) error {
	tx, err := db.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Lock the user's row to serialize concurrent withdrawals against
	// this derived (non-row-backed) balance.
	if _, err := tx.ExecContext(ctx, `SELECT id FROM users WHERE id = $1 FOR UPDATE`, userID); err != nil {
		return err
	}

	// Idempotency check MUST come before the balance check. If a prior
	// attempt already committed, current balance already reflects that
	// withdrawal — checking balance first here would incorrectly re-evaluate
	// sufficiency against an already-adjusted balance.
	var alreadyApplied bool
	checkQuery := `SELECT EXISTS(SELECT 1 FROM withdrawals WHERE idempotency_key = $1)`
	if err := tx.QueryRowContext(ctx, checkQuery, idempotencyKey).Scan(&alreadyApplied); err != nil {
		return err
	}
	if alreadyApplied {
		return tx.Commit() // nothing to do; prior attempt already succeeded
	}

	current, _, err := getBalance(ctx, tx, userID)
	if err != nil {
		return err
	}
	if sum > current {
		return ErrInsufficientBalance
	}

	insertQuery := `
		INSERT INTO withdrawals (user_id, order_number, sum, idempotency_key)
		VALUES ($1, $2, $3, $4)
	`
	if _, err := tx.ExecContext(ctx, insertQuery, userID, orderNumber, sum, idempotencyKey); err != nil {
		return err
	}

	return tx.Commit()
}

func generateIdempotencyKey() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate idempotency key: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func isRetriableDBError(err error) bool {
	if err == nil {
		return false
	}

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && strings.HasPrefix(pgErr.Code, "08") {
		return true
	}

	var connErr *pgconn.ConnectError
	if errors.As(err, &connErr) {
		return true
	}

	var netErr *net.OpError
	if errors.As(err, &netErr) {
		return true
	}

	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, io.EOF)
}
