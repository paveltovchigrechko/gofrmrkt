package db

import "database/sql"

type PostgresStorage struct {
	database *sql.DB
}

func NewPostgresStorage(db *sql.DB) *PostgresStorage {
	return &PostgresStorage{
		database: db,
	}
}
