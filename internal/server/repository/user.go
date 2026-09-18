// Package repository defines storage interfaces of the GophKeeper server.
//
// Services depend on these interfaces only, which keeps them
// database-agnostic and easy to mock. Concrete implementations live in
// sub-packages (e.g. the postgres package).
package repository

import (
	"context"

	"github.com/dmitriy/gophkeeper/internal/server/model"
)

// UserRepository persists and retrieves users.
type UserRepository interface {
	// Create inserts a new user. The database generates the identifier and
	// creation timestamp, which are filled back into user.ID and
	// user.CreatedAt. Returns model.ErrAlreadyExists if the login is taken.
	Create(ctx context.Context, user *model.User) error
	// GetByLogin returns the user with the given login, or
	// model.ErrNotFound if it does not exist.
	GetByLogin(ctx context.Context, login string) (*model.User, error)
	// GetByID returns the user with the given identifier, or
	// model.ErrNotFound if it does not exist.
	GetByID(ctx context.Context, id string) (*model.User, error)
}
