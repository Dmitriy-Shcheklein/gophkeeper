package cli

import (
	"errors"

	"github.com/dmitriy/gophkeeper/internal/client/gateway"
	"github.com/dmitriy/gophkeeper/internal/client/service"
)

// ErrNotAuthenticated is returned by the commands that require a
// session when no (or no longer valid) saved token exists.
var ErrNotAuthenticated = errors.New("not authenticated, run `gophkeeper login`")

// ErrInvalidCredentials is returned by the login command when the
// server rejects the credentials: the user is already trying to log
// in, so the generic "not authenticated" hint would mislead.
var ErrInvalidCredentials = errors.New("invalid login or password")

// friendlyMessage maps an error returned by a command to a
// user-facing message. Gateway and service sentinels become
// actionable hints; anything else (including the validation
// sentinels, whose messages already name the offending field) is
// passed through unchanged.
func friendlyMessage(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrInvalidCredentials):
		return "invalid login or password"
	case errors.Is(err, ErrNotAuthenticated),
		errors.Is(err, gateway.ErrUnauthenticated):
		return "not authenticated, run `gophkeeper login`"
	case errors.Is(err, gateway.ErrConflict):
		return "conflict: entry was modified by another client, re-fetch with `gophkeeper get --id=...` and retry"
	case errors.Is(err, service.ErrOffline):
		return "server unreachable: this action is not available offline"
	case errors.Is(err, gateway.ErrNotFound):
		return "entry not found"
	case errors.Is(err, service.ErrEmptyLogin),
		errors.Is(err, service.ErrEmptyPassword):
		return err.Error()
	default:
		return err.Error()
	}
}
