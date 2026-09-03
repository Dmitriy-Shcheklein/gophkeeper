// Package tui implements the interactive terminal user interface of
// the GophKeeper client on top of bubbletea.
//
// The TUI consumes the same service layer as the CLI commands: it
// depends on small consumer-side interfaces (authClient, entryClient)
// and the compile-time assertions below pin them to the real
// *service.AuthService and *service.EntryService, so the two
// frontends cannot drift apart.
//
// Screens: the login/register screen (shown when no token is
// available), the entry list (with substring filtering), the detail
// view, the create/edit form and the delete confirmation. State
// transitions live in plain bubbletea models and are unit-tested
// without a terminal; see the *_test.go files.
package tui

import (
	"context"
	"fmt"

	"github.com/charmbracelet/bubbletea"

	"github.com/dmitriy/gophkeeper/internal/client/model"
	"github.com/dmitriy/gophkeeper/internal/client/service"
)

// authClient is the authentication API the TUI depends on. It
// mirrors the service.AuthService surface; the compile-time
// assertion below keeps the two in sync.
type authClient interface {
	// Restore loads a persisted token into the client, if any.
	Restore() error
	// IsAuthenticated reports whether a token is available.
	IsAuthenticated() bool
	// Register creates a new account and stores the token on success.
	Register(ctx context.Context, login, password string) error
	// Login authenticates and stores the token on success.
	Login(ctx context.Context, login, password string) error
}

// entryClient is the entry management API the TUI depends on. It
// mirrors the service.EntryService surface; the compile-time
// assertion below keeps the two in sync.
type entryClient interface {
	// Add stores a new entry and returns the server's copy.
	Add(ctx context.Context, entry *model.Entry) (*model.Entry, error)
	// List returns all entries of the authenticated user.
	List(ctx context.Context) ([]*model.Entry, error)
	// Get returns a single entry by id.
	Get(ctx context.Context, id string) (*model.Entry, error)
	// Edit replaces entry content based on the carried version.
	Edit(ctx context.Context, entry *model.Entry) (*model.Entry, error)
	// Remove deletes an entry by id.
	Remove(ctx context.Context, id string) error
	// Sync returns the full current set of the user's entries.
	Sync(ctx context.Context) ([]*model.Entry, error)
}

// Compile-time assertions: the real services satisfy the consumer
// interfaces used by the TUI (same pattern as cli/app.go).
var (
	_ authClient  = (*service.AuthService)(nil)
	_ entryClient = (*service.EntryService)(nil)
)

// Run starts the interactive TUI over the given services and blocks
// until the user quits or a fatal error occurs. A missing saved
// token is not an error: the program starts on the login/register
// screen instead. Per-operation errors (load failures, conflicts,
// validation, failed logins) are shown in the TUI status bar, not
// returned; only terminal (TTY) failures surface here.
func Run(auth authClient, entries entryClient) error {
	if err := auth.Restore(); err != nil {
		return fmt.Errorf("restore saved token: %w", err)
	}

	program := tea.NewProgram(newAppModel(auth, entries), tea.WithAltScreen())
	if _, err := program.Run(); err != nil {
		return fmt.Errorf("tui: %w", err)
	}
	return nil
}
