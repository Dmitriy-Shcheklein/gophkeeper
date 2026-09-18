// Package postgres provides PostgreSQL implementations of the repository
// interfaces defined in the repository package.
//
// Storage owns the connection pool and hands out repository
// implementations bound to it:
//
//	st, err := postgres.New(ctx, dsn)
//	...
//	defer st.Close()
//	err = st.Users().GetByLogin(ctx, login)
package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dmitriy/gophkeeper/internal/server/repository"
)

// Compile-time interface conformance checks.
var (
	_ repository.UserRepository  = (*userRepository)(nil)
	_ repository.EntryRepository = (*entryRepository)(nil)
)

// Storage is a PostgreSQL-backed storage owning a connection pool. It is
// safe for concurrent use.
type Storage struct {
	pool    *pgxpool.Pool
	users   *userRepository
	entries *entryRepository
}

// New creates a Storage connecting to the given DSN and verifies the
// connection with a ping.
func New(ctx context.Context, dsn string) (*Storage, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("postgres: create pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres: ping: %w", err)
	}
	return &Storage{
		pool:    pool,
		users:   &userRepository{pool: pool},
		entries: &entryRepository{pool: pool},
	}, nil
}

// Close releases all pool resources.
func (s *Storage) Close() {
	s.pool.Close()
}

// Users returns the UserRepository bound to this storage.
func (s *Storage) Users() repository.UserRepository {
	return s.users
}

// Entries returns the EntryRepository bound to this storage.
func (s *Storage) Entries() repository.EntryRepository {
	return s.entries
}
