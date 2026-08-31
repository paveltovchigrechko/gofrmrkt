package repo

// import (
// 	"context"
// 	"errors"
// 	"testing"

// 	"github.com/DATA-DOG/go-sqlmock"
// 	"github.com/jackc/pgerrcode"
// 	"github.com/jackc/pgx/v5/pgconn"
// 	"github.com/stretchr/testify/assert"
// 	"github.com/stretchr/testify/require"
// )

// func TestRegisterUser(t *testing.T) {
// 	t.Run("successful registration", func(t *testing.T) {
// 		mockDB, mock, err := sqlmock.New()
// 		require.NoError(t, err)
// 		defer mockDB.Close()

// 		pg := &Postgres{database: mockDB}

// 		login := "pavel"
// 		password := "secret123"

// 		// Expect INSERT query with any hashed password matching the login
// 		mock.ExpectExec(`INSERT INTO users`).
// 			WithArgs(login, sqlmock.AnyArg()).
// 			WillReturnResult(sqlmock.NewResult(1, 1))

// 		err = pg.RegisterUser(context.Background(), login, password)

// 		assert.NoError(t, err)
// 		assert.NoError(t, mock.ExpectationsWereMet())
// 	})

// 	t.Run("duplicate login error", func(t *testing.T) {
// 		mockDB, mock, err := sqlmock.New()
// 		require.NoError(t, err)
// 		defer mockDB.Close()

// 		pg := &Postgres{database: mockDB}

// 		login := "existing_user"
// 		password := "secret123"

// 		pgErr := &pgconn.PgError{
// 			Code: pgerrcode.UniqueViolation,
// 		}

// 		mock.ExpectExec(`INSERT INTO users`).
// 			WithArgs(login, sqlmock.AnyArg()).
// 			WillReturnError(pgErr)

// 		err = pg.RegisterUser(context.Background(), login, password)

// 		assert.ErrorIs(t, err, ErrUserLoginExist)
// 		assert.NoError(t, mock.ExpectationsWereMet())
// 	})

// 	t.Run("unexpected database error", func(t *testing.T) {
// 		mockDB, mock, err := sqlmock.New()
// 		require.NoError(t, err)
// 		defer mockDB.Close()

// 		pg := &Postgres{database: mockDB}

// 		expectedErr := errors.New("db connection failure")

// 		mock.ExpectExec(`INSERT INTO users`).
// 			WithArgs("user1", sqlmock.AnyArg()).
// 			WillReturnError(expectedErr)

// 		err = pg.RegisterUser(context.Background(), "user1", "password")

// 		assert.ErrorIs(t, err, expectedErr)
// 		assert.NoError(t, mock.ExpectationsWereMet())
// 	})
// }
