package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/dmitriy/gophkeeper/internal/client/service"
)

// resolveEntryID expands a (possibly truncated) entry id — as shown
// in the list table — to the full server id. An id that matches a
// stored entry exactly passes through unchanged; a unique prefix is
// expanded; an ambiguous prefix is rejected; anything else (e.g. a
// full id of an entry that does not exist) is passed through so the
// server reports the not-found error.
func (a *App) resolveEntryID(ctx context.Context, id string) (string, error) {
	if id == "" {
		return "", errors.New("entry id must not be empty")
	}
	entries, err := a.entries.List(ctx)
	if err != nil && !errors.Is(err, service.ErrOffline) {
		// An offline List still returns the cached set: id
		// resolution works without the server.
		return "", err
	}
	var matches []string
	for _, entry := range entries {
		if entry.ID == id {
			return id, nil
		}
		if strings.HasPrefix(entry.ID, id) {
			matches = append(matches, entry.ID)
		}
	}
	switch len(matches) {
	case 0:
		return id, nil
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("ambiguous id %q: matches %d entries, use a longer prefix", id, len(matches))
	}
}
