package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dmitriy/gophkeeper/internal/server/model"
)

// uniqueViolation is the PostgreSQL error code for a unique constraint
// violation (SQLSTATE 23505).
const uniqueViolation = "23505"

// userRepository is a PostgreSQL implementation of repository.UserRepository.
type userRepository struct {
	pool *pgxpool.Pool
}

// Create inserts a new user, filling the generated ID and creation
// timestamp back into user.
func (r *userRepository) Create(ctx context.Context, user *model.User) error {
	const q = `
		INSERT INTO users (login, pass_hash)
		VALUES ($1, $2)
		RETURNING id::text, created_at`

	err := r.pool.QueryRow(ctx, q, user.Login, user.PassHash).
		Scan(&user.ID, &user.CreatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
			return fmt.Errorf("postgres: create user (login %q): %w", user.Login, model.ErrAlreadyExists)
		}
		return fmt.Errorf("postgres: create user: %w", err)
	}
	return nil
}

// GetByLogin returns the user with the given login.
func (r *userRepository) GetByLogin(ctx context.Context, login string) (*model.User, error) {
	const q = `
		SELECT id::text, login, pass_hash, created_at
		FROM users
		WHERE login = $1`

	user, err := scanUser(r.pool.QueryRow(ctx, q, login))
	if err != nil {
		return nil, fmt.Errorf("postgres: get user by login %q: %w", login, mapNoRows(err))
	}
	return user, nil
}

// GetByID returns the user with the given identifier.
func (r *userRepository) GetByID(ctx context.Context, id string) (*model.User, error) {
	const q = `
		SELECT id::text, login, pass_hash, created_at
		FROM users
		WHERE id = $1`

	user, err := scanUser(r.pool.QueryRow(ctx, q, id))
	if err != nil {
		return nil, fmt.Errorf("postgres: get user by id %q: %w", id, mapNoRows(err))
	}
	return user, nil
}

// scanUser scans a single user row returned by one of the queries above.
func scanUser(row pgx.Row) (*model.User, error) {
	var user model.User
	err := row.Scan(&user.ID, &user.Login, &user.PassHash, &user.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// mapNoRows converts pgx.ErrNoRows to model.ErrNotFound, leaving any
// other error untouched.
func mapNoRows(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return model.ErrNotFound
	}
	return err
}
