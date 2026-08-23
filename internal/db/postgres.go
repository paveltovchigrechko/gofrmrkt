package db

import (
	"database/sql"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

const (
	databaseDriver = "postgres"
	migrationPath  = "file://migrations"
)

func OpenDB(databaseDSN string) (*sql.DB, error) {
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

func RunMigrations(db *sql.DB) error {
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
