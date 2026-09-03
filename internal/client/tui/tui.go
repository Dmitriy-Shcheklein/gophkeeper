// Package tui implements the interactive terminal user interface of
// the GophKeeper client on top of bubbletea.
//
// The TUI consumes the same service layer as the CLI commands: it
// depends on small consumer-side interfaces (authClient, entryClient)
// and the compile-time assertions below pin them to the real
// *service.AuthService and *service.EntryService, so the two
// frontends cannot drift apart.
//
// Screens: the entry list (with substring filtering), the detail
// view, the create/edit form and the delete confirmation. State
// transitions live in plain bubbletea models and are unit-tested
// without a terminal; see the *_test.go files.
package tui

import (
	"context"
	"errors"
	"fmt"

	"github.com/charmbracelet/bubbletea"

	"github.com/dmitriy/gophkeeper/internal/client/model"
	"github.com/dmitriy/gophkeeper/internal/client/service"
)

// ErrNotAuthenticated is returned by Run when no (or no longer
// valid) saved token exists: the TUI works with the user's entries
// and is pointless without a session. The CLI maps it to a friendly
// message and exit code 1, consistent with the other data commands.
var ErrNotAuthenticated = errors.New("not authenticated, run `gophkeeper login`")

// authClient is the authentication API the TUI depends on. It
// mirrors the service.AuthService surface; the compile-time
// assertion below keeps the two in sync.
type authClient interface {
	// Restore loads a persisted token into the client, if any.
	Restore() error
	// IsAuthenticated reports whether a token is available.
	IsAuthenticated() bool
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
// until the user quits or a fatal error occurs. Per-operation errors
// (load failures, conflicts, validation) are shown in the TUI status
// bar, not returned; only the pre-flight authentication check and
// terminal failures surface here.
func Run(auth authClient, entries entryClient) error {
	if err := auth.Restore(); err != nil {
		return fmt.Errorf("restore saved token: %w", err)
	}
	if !auth.IsAuthenticated() {
		return ErrNotAuthenticated
	}

	program := tea.NewProgram(newAppModel(entries), tea.WithAltScreen())
	if _, err := program.Run(); err != nil {
		return fmt.Errorf("tui: %w", err)
	}
	return nil
}
