package repo

import (
	"context"
	"database/sql"
	"errors"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrUserLoginExist = errors.New("user with this login already exists")
	ErrUserNotFound   = errors.New("user not found")
)

type Postgres struct {
	database *sql.DB
}

func NewPostgresDB(databaseDSN string) (*Postgres, error) {
	postgres, err := openDB(databaseDSN)
	if err != nil {
		return nil, err
	}
	err = runMigrations(postgres)
	if err != nil {
		postgres.Close()
		return nil, err
	}

	return &Postgres{
		database: postgres,
	}, nil
}

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

func (db *Postgres) Close() error {
	return db.database.Close()
}

const (
	databaseDriver = "postgres"
	migrationPath  = "file://migrations"
)

func openDB(databaseDSN string) (*sql.DB, error) {
	db, err := sql.Open(databaseDriver, databaseDSN)
	if err != nil {
		return nil, err
	}

	// Ping database
	if err = db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return db, err
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

	err = m.Up()
	if err != nil && err != migrate.ErrNoChange { // ErrNoChange means the schema already exists.
		return err
	}

	return nil
}
