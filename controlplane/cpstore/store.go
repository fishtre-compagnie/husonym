// Package cpstore is the storage of the control plane.
package cpstore

import (
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store keeps what the control plane knows in its PostgreSQL database.
type Store struct {
	pool *pgxpool.Pool
}

// New returns a Store over pool.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}
