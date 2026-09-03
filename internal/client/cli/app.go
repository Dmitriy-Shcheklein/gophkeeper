package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/dmitriy/gophkeeper/internal/client/config"
	"github.com/dmitriy/gophkeeper/internal/client/gateway"
	"github.com/dmitriy/gophkeeper/internal/client/model"
	"github.com/dmitriy/gophkeeper/internal/client/service"
	"github.com/dmitriy/gophkeeper/internal/client/token"
	"golang.org/x/term"
)

// authClient is the authentication API the CLI commands depend on.
// It mirrors the service.AuthService surface; the compile-time
// assertion below keeps the two in sync.
type authClient interface {
	// Register creates a new account and persists the token.
	Register(ctx context.Context, login, password string) error
	// Login authenticates an existing account and persists the token.
	Login(ctx context.Context, login, password string) error
	// Logout clears the persisted and in-memory token.
	Logout() error
	// Restore loads a persisted token into the client, if any.
	Restore() error
	// IsAuthenticated reports whether a token is available.
	IsAuthenticated() bool
}

// entryClient is the entry management API the CLI commands depend
// on. It mirrors the service.EntryService surface; the compile-time
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
// interfaces used by the commands.
var (
	_ authClient  = (*service.AuthService)(nil)
	_ entryClient = (*service.EntryService)(nil)
)

// connector builds the real service pair from the resolved
// configuration, returning a shutdown function for the underlying
// connection. Production wiring only; tests inject fakes instead.
type connector func(cfg *config.Config) (authClient, entryClient, func(), error)

// App is the shared dependency container for all CLI commands. The
// root command owns the persistent configuration flags (writing into
// serverFlag and tokenFlag) and every subcommand reads the shared
// services through the app.
//
// In production the app starts service-less and dials the server
// lazily on the first command that needs it (see initServices);
// tests construct an App with pre-set fake services and a nil
// connector. Use NewApp for the production wiring.
type App struct {
	// Version, BuildDate and Commit are build metadata reported by
	// the version command.
	Version   string
	BuildDate string
	Commit    string

	// In, Out and Err are the command I/O streams: In is read for
	// confirmation prompts and stdin ("-") payloads, Out receives
	// command output, Err receives prompts and diagnostics.
	In  io.Reader
	Out io.Writer
	Err io.Writer

	// PromptPassword asks the user for a password without echo. It
	// is used by register and login when --password is omitted.
	PromptPassword func() (string, error)

	// serverFlag and tokenFlag receive the values of the root
	// command's persistent flags (--server, --token-path); empty
	// when the flags are not set.
	serverFlag string
	tokenFlag  string

	// auth and entries are the services used by the commands: the
	// injected fakes in tests, or the lazily built real services.
	auth    authClient
	entries entryClient

	// connect builds the real services; nil in tests.
	connect connector

	// closeConn shuts the gateway connection down; set together with
	// the real services.
	closeConn func()
}

// NewApp returns an App wired to the real backend: services are
// created lazily from the --server/--token-path flags (or their
// environment/default fallbacks) on the first command that needs
// them, and the gRPC connection is closed when Execute returns.
func NewApp(version, buildDate, commit string) *App {
	return &App{
		Version:        version,
		BuildDate:      buildDate,
		Commit:         commit,
		In:             os.Stdin,
		Out:            os.Stdout,
		Err:            os.Stderr,
		PromptPassword: terminalPassword,
		connect:        connectReal,
	}
}

// connectReal builds the production service stack: a token store at
// cfg.TokenPath, a gRPC gateway to cfg.Server and the service layer
// on top of both. The returned function closes the connection.
func connectReal(cfg *config.Config) (authClient, entryClient, func(), error) {
	store, err := token.New(cfg.TokenPath)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("open token store: %w", err)
	}
	gw, err := gateway.New(cfg.Server, store)
	if err != nil {
		return nil, nil, nil, err
	}
	return service.NewAuthService(gw, gw.TokenStore()),
		service.NewEntryService(gw),
		func() { _ = gw.Close() },
		nil
}

// initServices makes the services available, dialing the server
// lazily on the production path (when no services were injected).
func (a *App) initServices() error {
	if a.auth != nil && a.entries != nil {
		return nil
	}
	if a.connect == nil {
		return errors.New("cli: no services configured")
	}
	cfg, err := config.Resolve(a.serverFlag, a.tokenFlag)
	if err != nil {
		return err
	}
	auth, entries, closeConn, err := a.connect(cfg)
	if err != nil {
		return err
	}
	a.auth, a.entries, a.closeConn = auth, entries, closeConn
	return nil
}

// requireAuth is the authentication guard of the commands that work
// with the user's data: it connects (if needed), restores the
// persisted token and fails with ErrNotAuthenticated when the user
// is not logged in.
func (a *App) requireAuth() error {
	if err := a.initServices(); err != nil {
		return err
	}
	if err := a.auth.Restore(); err != nil {
		return fmt.Errorf("restore saved token: %w", err)
	}
	if !a.auth.IsAuthenticated() {
		return ErrNotAuthenticated
	}
	return nil
}

// resolvePassword returns the explicitly provided password or asks
// for it interactively (the recommended path: command-line passwords
// are visible in shell history).
func (a *App) resolvePassword(flagValue string) (string, error) {
	if flagValue != "" {
		return flagValue, nil
	}
	if a.PromptPassword == nil {
		return "", errors.New("no password prompt available; use --password")
	}
	return a.PromptPassword()
}

// confirm asks the user for a yes/no confirmation, reading a single
// line from In. It returns true only for an explicit "y"/"yes"
// answer; EOF or any other answer is treated as "no".
func (a *App) confirm(question string) bool {
	_, _ = fmt.Fprintf(a.Err, "%s [y/N]: ", question)
	answer, err := a.readLine()
	if err != nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}

// readLine reads one line of input from In.
func (a *App) readLine() (string, error) {
	var line []byte
	buf := make([]byte, 1)
	for {
		n, err := a.In.Read(buf)
		if n > 0 {
			if buf[0] == '\n' {
				return string(line), nil
			}
			line = append(line, buf[0])
		}
		if err != nil {
			if errors.Is(err, io.EOF) && len(line) > 0 {
				return string(line), nil
			}
			return "", err
		}
	}
}

// shutdown releases the backend connection, if one was opened.
func (a *App) shutdown() {
	if a.closeConn != nil {
		a.closeConn()
	}
}

// terminalPassword is the default PromptPassword implementation: it
// prints the prompt to Err and reads the password from the terminal
// without echo. It fails when stdin is not a terminal, pointing the
// user at the --password flag.
func terminalPassword() (string, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return "", errors.New("password prompt requires an interactive terminal; use --password instead")
	}
	_, _ = fmt.Fprint(os.Stderr, "Password: ")
	password, err := term.ReadPassword(fd)
	_, _ = fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	return string(password), nil
}
