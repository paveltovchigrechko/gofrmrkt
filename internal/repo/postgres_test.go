package repo

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newMockPostgres(t *testing.T) (*Postgres, sqlmock.Sqlmock) {
	t.Helper()

	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)

	t.Cleanup(func() {
		db.Close()
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	return &Postgres{database: db}, mock
}

func uniqueViolationErr() error {
	return &pgconn.PgError{Code: pgerrcode.UniqueViolation}
}

// --- RegisterUser ---

func TestPostgres_RegisterUser_Success(t *testing.T) {
	pg, mock := newMockPostgres(t)

	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO users (login, password_hash)`)).
		WithArgs("alice", "hash").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(1)))

	id, err := pg.RegisterUser(context.Background(), "alice", "hash")
	require.NoError(t, err)
	assert.Equal(t, int64(1), id)
}

func TestPostgres_RegisterUser_LoginExists(t *testing.T) {
	pg, mock := newMockPostgres(t)

	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO users (login, password_hash)`)).
		WithArgs("alice", "hash").
		WillReturnError(uniqueViolationErr())

	_, err := pg.RegisterUser(context.Background(), "alice", "hash")
	assert.ErrorIs(t, err, ErrUserLoginExist)
}

func TestPostgres_RegisterUser_OtherDBError(t *testing.T) {
	pg, mock := newMockPostgres(t)

	dbErr := errors.New("connection reset")
	mock.ExpectQuery(regexp.QuoteMeta(`INSERT INTO users (login, password_hash)`)).
		WithArgs("alice", "hash").
		WillReturnError(dbErr)

	_, err := pg.RegisterUser(context.Background(), "alice", "hash")
	assert.ErrorIs(t, err, dbErr)
	assert.NotErrorIs(t, err, ErrUserLoginExist)
}

// --- GetUserByLogin ---

func TestPostgres_GetUserByLogin_Success(t *testing.T) {
	pg, mock := newMockPostgres(t)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, password_hash`)).
		WithArgs("alice").
		WillReturnRows(sqlmock.NewRows([]string{"id", "password_hash"}).AddRow(int64(1), "hash"))

	id, hash, err := pg.GetUserByLogin(context.Background(), "alice")
	require.NoError(t, err)
	assert.Equal(t, int64(1), id)
	assert.Equal(t, "hash", hash)
}

func TestPostgres_GetUserByLogin_NotFound(t *testing.T) {
	pg, mock := newMockPostgres(t)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT id, password_hash`)).
		WithArgs("ghost").
		WillReturnError(sql.ErrNoRows)

	_, _, err := pg.GetUserByLogin(context.Background(), "ghost")
	assert.ErrorIs(t, err, ErrUserNotFound)
}

// --- UploadOrder ---

func TestPostgres_UploadOrder_New(t *testing.T) {
	pg, mock := newMockPostgres(t)

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO orders (number, user_id)`)).
		WithArgs("12345", int64(1)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := pg.UploadOrder(context.Background(), 1, "12345")
	assert.NoError(t, err)
}

func TestPostgres_UploadOrder_SameUser(t *testing.T) {
	pg, mock := newMockPostgres(t)

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO orders (number, user_id)`)).
		WithArgs("12345", int64(1)).
		WillReturnError(uniqueViolationErr())

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT user_id FROM orders WHERE number = $1`)).
		WithArgs("12345").
		WillReturnRows(sqlmock.NewRows([]string{"user_id"}).AddRow(int64(1)))

	err := pg.UploadOrder(context.Background(), 1, "12345")
	assert.ErrorIs(t, err, ErrOrderAlreadyUploaded)
}

func TestPostgres_UploadOrder_DifferentUser(t *testing.T) {
	pg, mock := newMockPostgres(t)

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO orders (number, user_id)`)).
		WithArgs("12345", int64(2)).
		WillReturnError(uniqueViolationErr())

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT user_id FROM orders WHERE number = $1`)).
		WithArgs("12345").
		WillReturnRows(sqlmock.NewRows([]string{"user_id"}).AddRow(int64(1)))

	err := pg.UploadOrder(context.Background(), 2, "12345")
	assert.ErrorIs(t, err, ErrOrderOwnedByAnotherUser)
}

func TestPostgres_UploadOrder_OwnerLookupFails(t *testing.T) {
	pg, mock := newMockPostgres(t)

	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO orders (number, user_id)`)).
		WithArgs("12345", int64(1)).
		WillReturnError(uniqueViolationErr())

	lookupErr := errors.New("lookup failed")
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT user_id FROM orders WHERE number = $1`)).
		WithArgs("12345").
		WillReturnError(lookupErr)

	err := pg.UploadOrder(context.Background(), 1, "12345")
	assert.ErrorIs(t, err, lookupErr)
}

func TestPostgres_UploadOrder_OtherDBError(t *testing.T) {
	pg, mock := newMockPostgres(t)

	dbErr := errors.New("connection reset")
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO orders (number, user_id)`)).
		WithArgs("12345", int64(1)).
		WillReturnError(dbErr)

	err := pg.UploadOrder(context.Background(), 1, "12345")
	assert.ErrorIs(t, err, dbErr)
}

// --- GetOrders ---

func TestPostgres_GetOrders_MixedAccrual(t *testing.T) {
	pg, mock := newMockPostgres(t)

	uploadedAt := time.Now()
	rows := sqlmock.NewRows([]string{"number", "status", "accrual", "uploaded_at"}).
		AddRow("111", "PROCESSED", sql.NullFloat64{Float64: 500, Valid: true}, uploadedAt).
		AddRow("222", "PROCESSING", sql.NullFloat64{Valid: false}, uploadedAt)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT number, status, accrual, uploaded_at`)).
		WithArgs(int64(1)).
		WillReturnRows(rows)

	orders, err := pg.GetOrders(context.Background(), 1)
	require.NoError(t, err)
	require.Len(t, orders, 2)

	require.NotNil(t, orders[0].Accrual)
	assert.Equal(t, 500.0, *orders[0].Accrual)
	assert.Nil(t, orders[1].Accrual)
}

func TestPostgres_GetOrders_Empty(t *testing.T) {
	pg, mock := newMockPostgres(t)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT number, status, accrual, uploaded_at`)).
		WithArgs(int64(1)).
		WillReturnRows(sqlmock.NewRows([]string{"number", "status", "accrual", "uploaded_at"}))

	orders, err := pg.GetOrders(context.Background(), 1)
	require.NoError(t, err)
	assert.Empty(t, orders)
}

func TestPostgres_GetOrders_QueryError(t *testing.T) {
	pg, mock := newMockPostgres(t)

	dbErr := errors.New("query failed")
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT number, status, accrual, uploaded_at`)).
		WithArgs(int64(1)).
		WillReturnError(dbErr)

	_, err := pg.GetOrders(context.Background(), 1)
	assert.ErrorIs(t, err, dbErr)
}

// --- GetBalance ---

func TestPostgres_GetBalance_Success(t *testing.T) {
	pg, mock := newMockPostgres(t)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
		WithArgs(int64(1)).
		WillReturnRows(sqlmock.NewRows([]string{"current", "withdrawn"}).AddRow(458.5, 42.0))

	current, withdrawn, err := pg.GetBalance(context.Background(), 1)
	require.NoError(t, err)
	assert.Equal(t, 458.5, current)
	assert.Equal(t, 42.0, withdrawn)
}

func TestPostgres_GetBalance_Error(t *testing.T) {
	pg, mock := newMockPostgres(t)

	dbErr := errors.New("query failed")
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
		WithArgs(int64(1)).
		WillReturnError(dbErr)

	_, _, err := pg.GetBalance(context.Background(), 1)
	assert.ErrorIs(t, err, dbErr)
}

// --- WithdrawBalance ---

func TestPostgres_WithdrawBalance_Success(t *testing.T) {
	pg, mock := newMockPostgres(t)

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`SELECT id FROM users WHERE id = $1 FOR UPDATE`)).
		WithArgs(int64(1)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
		WithArgs(int64(1)).
		WillReturnRows(sqlmock.NewRows([]string{"current", "withdrawn"}).AddRow(1000.0, 0.0))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO withdrawals (user_id, order_number, sum)`)).
		WithArgs(int64(1), "12345", 500.0).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	err := pg.WithdrawBalance(context.Background(), 1, "12345", 500.0)
	assert.NoError(t, err)
}

func TestPostgres_WithdrawBalance_InsufficientFunds(t *testing.T) {
	pg, mock := newMockPostgres(t)

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`SELECT id FROM users WHERE id = $1 FOR UPDATE`)).
		WithArgs(int64(1)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
		WithArgs(int64(1)).
		WillReturnRows(sqlmock.NewRows([]string{"current", "withdrawn"}).AddRow(100.0, 0.0))
	mock.ExpectRollback()

	err := pg.WithdrawBalance(context.Background(), 1, "12345", 500.0)
	assert.ErrorIs(t, err, ErrInsufficientBalance)
}

func TestPostgres_WithdrawBalance_LockFails(t *testing.T) {
	pg, mock := newMockPostgres(t)

	dbErr := errors.New("lock timeout")
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`SELECT id FROM users WHERE id = $1 FOR UPDATE`)).
		WithArgs(int64(1)).
		WillReturnError(dbErr)
	mock.ExpectRollback()

	err := pg.WithdrawBalance(context.Background(), 1, "12345", 500.0)
	assert.ErrorIs(t, err, dbErr)
}

func TestPostgres_WithdrawBalance_InsertFails(t *testing.T) {
	pg, mock := newMockPostgres(t)

	dbErr := errors.New("insert failed")
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`SELECT id FROM users WHERE id = $1 FOR UPDATE`)).
		WithArgs(int64(1)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT`)).
		WithArgs(int64(1)).
		WillReturnRows(sqlmock.NewRows([]string{"current", "withdrawn"}).AddRow(1000.0, 0.0))
	mock.ExpectExec(regexp.QuoteMeta(`INSERT INTO withdrawals (user_id, order_number, sum)`)).
		WithArgs(int64(1), "12345", 500.0).
		WillReturnError(dbErr)
	mock.ExpectRollback()

	err := pg.WithdrawBalance(context.Background(), 1, "12345", 500.0)
	assert.ErrorIs(t, err, dbErr)
}

// --- GetWithdrawals ---

func TestPostgres_GetWithdrawals_Success(t *testing.T) {
	pg, mock := newMockPostgres(t)

	processedAt := time.Now()
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT order_number, sum, processed_at`)).
		WithArgs(int64(1)).
		WillReturnRows(sqlmock.NewRows([]string{"order_number", "sum", "processed_at"}).
			AddRow("12345", 500.0, processedAt))

	withdrawals, err := pg.GetWithdrawals(context.Background(), 1)
	require.NoError(t, err)
	require.Len(t, withdrawals, 1)
	assert.Equal(t, "12345", withdrawals[0].Order)
	assert.Equal(t, 500.0, withdrawals[0].Sum)
}

func TestPostgres_GetWithdrawals_Empty(t *testing.T) {
	pg, mock := newMockPostgres(t)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT order_number, sum, processed_at`)).
		WithArgs(int64(1)).
		WillReturnRows(sqlmock.NewRows([]string{"order_number", "sum", "processed_at"}))

	withdrawals, err := pg.GetWithdrawals(context.Background(), 1)
	require.NoError(t, err)
	assert.Empty(t, withdrawals)
}

func TestPostgres_GetWithdrawals_Error(t *testing.T) {
	pg, mock := newMockPostgres(t)

	dbErr := errors.New("query failed")
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT order_number, sum, processed_at`)).
		WithArgs(int64(1)).
		WillReturnError(dbErr)

	_, err := pg.GetWithdrawals(context.Background(), 1)
	assert.ErrorIs(t, err, dbErr)
}

// --- GetPendingOrders ---

func TestPostgres_GetPendingOrders_Success(t *testing.T) {
	pg, mock := newMockPostgres(t)

	mock.ExpectQuery(regexp.QuoteMeta(`SELECT number, user_id`)).
		WithArgs(10).
		WillReturnRows(sqlmock.NewRows([]string{"number", "user_id"}).
			AddRow("111", int64(1)).
			AddRow("222", int64(2)))

	orders, err := pg.GetPendingOrders(context.Background(), 10)
	require.NoError(t, err)
	require.Len(t, orders, 2)
	assert.Equal(t, "111", orders[0].Number)
	assert.Equal(t, int64(2), orders[1].UserID)
}

func TestPostgres_GetPendingOrders_Error(t *testing.T) {
	pg, mock := newMockPostgres(t)

	dbErr := errors.New("query failed")
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT number, user_id`)).
		WithArgs(10).
		WillReturnError(dbErr)

	_, err := pg.GetPendingOrders(context.Background(), 10)
	assert.ErrorIs(t, err, dbErr)
}

// --- UpdateOrderStatus ---

func TestPostgres_UpdateOrderStatus_WithAccrual(t *testing.T) {
	pg, mock := newMockPostgres(t)

	accrual := 500.0
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE orders`)).
		WithArgs("PROCESSED", &accrual, "12345").
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := pg.UpdateOrderStatus(context.Background(), "12345", "PROCESSED", &accrual)
	assert.NoError(t, err)
}

func TestPostgres_UpdateOrderStatus_NoAccrual(t *testing.T) {
	pg, mock := newMockPostgres(t)

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE orders`)).
		WithArgs("INVALID", nil, "12345").
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := pg.UpdateOrderStatus(context.Background(), "12345", "INVALID", nil)
	assert.NoError(t, err)
}

func TestPostgres_UpdateOrderStatus_Error(t *testing.T) {
	pg, mock := newMockPostgres(t)

	dbErr := errors.New("update failed")
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE orders`)).
		WithArgs("PROCESSED", (*float64)(nil), "12345").
		WillReturnError(dbErr)

	err := pg.UpdateOrderStatus(context.Background(), "12345", "PROCESSED", nil)
	assert.ErrorIs(t, err, dbErr)
}
