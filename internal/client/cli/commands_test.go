package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/dmitriy/gophkeeper/internal/client/gateway"
	"github.com/dmitriy/gophkeeper/internal/client/model"
	"github.com/dmitriy/gophkeeper/internal/client/service"
	"github.com/stretchr/testify/require"
)

func TestRegisterCommand(t *testing.T) {
	auth := &fakeAuth{}
	app, out, errOut := newTestApp(auth, newFakeEntries())

	_, _, err := run(t, app, "register", "--login=alice", "--password=secret")
	require.NoError(t, err)
	require.Equal(t, [][2]string{{"alice", "secret"}}, auth.registered)
	require.Contains(t, out.String(), "registered and logged in")
	require.Empty(t, errOut.String())
}

func TestRegisterCommandPromptsForPassword(t *testing.T) {
	auth := &fakeAuth{}
	app, _, _ := newTestApp(auth, newFakeEntries())
	var prompted int
	app.PromptPassword = func() (string, error) {
		prompted++
		return "prompted-pass", nil
	}

	_, _, err := run(t, app, "register", "--login=alice")
	require.NoError(t, err)
	require.Equal(t, 1, prompted)
	require.Equal(t, [][2]string{{"alice", "prompted-pass"}}, auth.registered)
}

func TestRegisterCommandMissingLogin(t *testing.T) {
	app, _, errOut := newTestApp(&fakeAuth{}, newFakeEntries())

	_, _, err := run(t, app, "register", "--password=x")
	require.Error(t, err)
	require.Contains(t, errOut.String(), "required flag")
}

func TestRegisterCommandValidationError(t *testing.T) {
	// The real service validates credentials; the fake is fed the
	// same sentinel to verify the CLI maps it verbatim.
	app, _, errOut := newTestApp(&fakeAuth{registerErr: service.ErrEmptyLogin}, newFakeEntries())

	_, _, err := run(t, app, "register", "--login=", "--password=x")
	require.Error(t, err)
	require.Contains(t, errOut.String(), "gophkeeper: login must not be empty")
}

func TestLoginCommand(t *testing.T) {
	auth := &fakeAuth{}
	app, out, _ := newTestApp(auth, newFakeEntries())

	_, _, err := run(t, app, "login", "--login=alice", "--password=secret")
	require.NoError(t, err)
	require.Equal(t, [][2]string{{"alice", "secret"}}, auth.logins)
	require.Contains(t, out.String(), "logged in")
}

func TestLoginCommandPromptNotCalledWhenFlagGiven(t *testing.T) {
	auth := &fakeAuth{}
	app, _, _ := newTestApp(auth, newFakeEntries())
	app.PromptPassword = func() (string, error) {
		return "", errors.New("prompt must not be called")
	}

	_, _, err := run(t, app, "login", "--login=alice", "--password=secret")
	require.NoError(t, err)
	require.Len(t, auth.logins, 1)
}

func TestLoginCommandWrongCredentials(t *testing.T) {
	// On the login call Unauthenticated means bad credentials, not
	// "please log in".
	app, _, errOut := newTestApp(&fakeAuth{loginErr: gateway.ErrUnauthenticated}, newFakeEntries())

	_, _, err := run(t, app, "login", "--login=alice", "--password=wrong")
	require.Error(t, err)
	require.Contains(t, errOut.String(), "gophkeeper: invalid login or password")
}

func TestLoginCommandPassthroughError(t *testing.T) {
	app, _, errOut := newTestApp(&fakeAuth{loginErr: errors.New("server unavailable")}, newFakeEntries())

	_, _, err := run(t, app, "login", "--login=alice", "--password=x")
	require.Error(t, err)
	require.Contains(t, errOut.String(), "gophkeeper: server unavailable")
}

func TestLogoutCommand(t *testing.T) {
	auth := &fakeAuth{token: "saved"}
	app, out, _ := newTestApp(auth, newFakeEntries())

	_, _, err := run(t, app, "logout")
	require.NoError(t, err)
	require.Equal(t, 1, auth.logouts)
	require.Contains(t, out.String(), "logged out")
}

func TestCommandRequiresAuthentication(t *testing.T) {
	for _, args := range [][]string{
		{"add", "--type=text", "--label=x", "--text=y"},
		{"list"},
		{"get", "--id=e1"},
		{"edit", "--id=e1", "--label=new"},
		{"delete", "--id=e1", "--yes"},
		{"sync"},
	} {
		app, _, errOut := newTestApp(&fakeAuth{token: ""}, newFakeEntries())
		_, _, err := run(t, app, args...)
		require.Error(t, err, "args: %v", args)
		require.Contains(t, errOut.String(), "not authenticated, run `gophkeeper login`", "args: %v", args)
	}
}

func TestRestoreErrorSurfaces(t *testing.T) {
	app, _, errOut := newTestApp(&fakeAuth{token: "x", restoreErr: errors.New("disk gone")}, newFakeEntries())

	_, _, err := run(t, app, "list")
	require.Error(t, err)
	require.Contains(t, errOut.String(), "restore saved token")
}

func TestUnauthenticatedMidSessionMapsToFriendly(t *testing.T) {
	entries := newFakeEntries()
	// The real stack surfaces the gateway sentinel wrapped by the
	// service layer; errors.Is reaches it either way.
	entries.listErr = gateway.ErrUnauthenticated
	app, _, errOut := newTestApp(&fakeAuth{token: "expired"}, entries)

	_, _, err := run(t, app, "list")
	require.Error(t, err)
	require.Contains(t, errOut.String(), "not authenticated, run `gophkeeper login`")
}

func TestAddCommandLogin(t *testing.T) {
	entries := newFakeEntries()
	app, out, _ := newTestApp(&fakeAuth{token: "x"}, entries)

	_, _, err := run(t, app, "add", "--type=login", "--label=github",
		"--username=alice", "--password=p", "--metadata=work")
	require.NoError(t, err)
	require.Len(t, entries.added, 1)
	require.Equal(t, model.EntryTypeLoginPassword, entries.added[0].Type)
	require.Equal(t, "github", entries.added[0].Label)
	require.Equal(t, "work", entries.added[0].Metadata)
	login, err := decodeLogin(entries.added[0].Data)
	require.NoError(t, err)
	require.Equal(t, "alice", login.Username)
	require.Equal(t, "p", login.Password)
	require.Contains(t, out.String(), "created entry entry-00 (version 1)")
}

func TestAddCommandTextFromStdin(t *testing.T) {
	entries := newFakeEntries()
	app, _, _ := newTestApp(&fakeAuth{token: "x"}, entries)
	app.In = strings.NewReader("note body\n")

	_, _, err := run(t, app, "add", "--type=text", "--label=note", "--text=-")
	require.NoError(t, err)
	require.Equal(t, []byte("note body\n"), entries.added[0].Data)
}

func TestAddCommandTextFromEmptyStdin(t *testing.T) {
	entries := newFakeEntries()
	app, _, errOut := newTestApp(&fakeAuth{token: "x"}, entries)
	app.In = strings.NewReader("")

	_, _, err := run(t, app, "add", "--type=text", "--label=note", "--text=-")
	require.Error(t, err)
	require.Contains(t, errOut.String(), "text entry data must not be empty")
	require.Empty(t, entries.added)
}

func TestAddCommandBinaryFile(t *testing.T) {
	entries := newFakeEntries()
	app, _, _ := newTestApp(&fakeAuth{token: "x"}, entries)
	path := writeFile(t, "blob.bin", "binary-content")

	_, _, err := run(t, app, "add", "--type=binary", "--label=blob", "--file="+path)
	require.NoError(t, err)
	require.Equal(t, []byte("binary-content"), entries.added[0].Data)
}

func TestAddCommandBinaryEmptyFile(t *testing.T) {
	entries := newFakeEntries()
	app, _, errOut := newTestApp(&fakeAuth{token: "x"}, entries)
	path := writeFile(t, "empty.bin", "")

	_, _, err := run(t, app, "add", "--type=binary", "--label=blob", "--file="+path)
	require.Error(t, err)
	require.Contains(t, errOut.String(), "binary entry data must not be empty")
	require.Empty(t, entries.added)
}

func TestAddCommandBinaryMissingFile(t *testing.T) {
	app, _, errOut := newTestApp(&fakeAuth{token: "x"}, newFakeEntries())

	_, _, err := run(t, app, "add", "--type=binary", "--label=blob", "--file=/nonexistent")
	require.Error(t, err)
	require.Contains(t, errOut.String(), "no such file")
}

func TestAddCommandForeignFlagsRejected(t *testing.T) {
	app, _, errOut := newTestApp(&fakeAuth{token: "x"}, newFakeEntries())

	_, _, err := run(t, app, "add", "--type=text", "--label=n", "--text=x", "--username=u")
	require.Error(t, err)
	require.Contains(t, errOut.String(), "flag --username does not belong to this entry type")
}

func TestAddCommandMissingPayloadFlags(t *testing.T) {
	app, _, _ := newTestApp(&fakeAuth{token: "x"}, newFakeEntries())

	_, _, err := run(t, app, "add", "--type=login", "--label=x", "--username=u")
	require.Error(t, err)
	require.Contains(t, err.Error(), "type login requires --username and --password")
}

func TestAddCommandCard(t *testing.T) {
	entries := newFakeEntries()
	app, _, _ := newTestApp(&fakeAuth{token: "x"}, entries)

	_, _, err := run(t, app, "add", "--type=card", "--label=bank",
		"--number=4111111111111111", "--holder=Alice", "--expiry=12/28", "--cvv=123")
	require.NoError(t, err)
	card, err := decodeCard(entries.added[0].Data)
	require.NoError(t, err)
	require.Equal(t, "4111111111111111", card.Number)
	require.Equal(t, "123", card.CVV)
}

func TestAddCommandCardMissingNumber(t *testing.T) {
	app, _, _ := newTestApp(&fakeAuth{token: "x"}, newFakeEntries())

	_, _, err := run(t, app, "add", "--type=card", "--label=bank")
	require.Error(t, err)
	require.Contains(t, err.Error(), "type card requires --number")
}

func TestAddCommandInvalidType(t *testing.T) {
	app, _, errOut := newTestApp(&fakeAuth{token: "x"}, newFakeEntries())

	_, _, err := run(t, app, "add", "--type=nope", "--label=x")
	require.Error(t, err)
	require.Contains(t, errOut.String(), `invalid type "nope"`)
}

func TestListCommand(t *testing.T) {
	entries := newFakeEntries(
		&model.Entry{ID: "aaaaaaaa-1", Type: model.EntryTypeLoginPassword, Label: "github", Version: 1},
		&model.Entry{ID: "bbbbbbbb-2", Type: model.EntryTypeCard, Label: "bank", Version: 2},
	)
	app, out, _ := newTestApp(&fakeAuth{token: "x"}, entries)

	_, _, err := run(t, app, "list")
	require.NoError(t, err)
	text := out.String()
	require.Contains(t, text, "ID")
	require.Contains(t, text, "aaaaaaaa")
	require.Contains(t, text, "bbbbbbbb")

	out.Reset()
	_, _, err = run(t, app, "list", "--type=card")
	require.NoError(t, err)
	require.Contains(t, out.String(), "bbbbbbbb")
	require.NotContains(t, out.String(), "aaaaaaaa")
}

func TestListCommandInvalidType(t *testing.T) {
	app, _, errOut := newTestApp(&fakeAuth{token: "x"}, newFakeEntries())

	_, _, err := run(t, app, "list", "--type=nope")
	require.Error(t, err)
	require.Contains(t, errOut.String(), "invalid type")
}

func TestGetCommand(t *testing.T) {
	data, err := encodeLogin("alice", "pw")
	require.NoError(t, err)
	entries := newFakeEntries(&model.Entry{
		ID: "entry-9", Type: model.EntryTypeLoginPassword, Label: "github", Data: data, Version: 1,
	})
	app, out, _ := newTestApp(&fakeAuth{token: "x"}, entries)

	_, _, err = run(t, app, "get", "--id=entry-9")
	require.NoError(t, err)
	require.Contains(t, out.String(), "username: alice")
	require.Contains(t, out.String(), "password: pw")
}

func TestGetCommandNotFound(t *testing.T) {
	app, _, errOut := newTestApp(&fakeAuth{token: "x"}, newFakeEntries())

	_, _, err := run(t, app, "get", "--id=missing")
	require.Error(t, err)
	require.Contains(t, errOut.String(), "entry not found")
}

func TestGetCommandConflictMessage(t *testing.T) {
	entries := newFakeEntries()
	entries.getErr = gateway.ErrConflict
	app, _, errOut := newTestApp(&fakeAuth{token: "x"}, entries)

	_, _, err := run(t, app, "get", "--id=missing")
	require.Error(t, err)
	require.Contains(t, errOut.String(), "conflict: entry was modified by another client, re-fetch with `gophkeeper get --id=...` and retry")
}

func TestEditCommandGetModifyEdit(t *testing.T) {
	data, err := encodeLogin("alice", "pw")
	require.NoError(t, err)
	entries := newFakeEntries(&model.Entry{
		ID: "entry-1", Type: model.EntryTypeLoginPassword, Label: "github", Data: data, Version: 3,
	})
	app, out, _ := newTestApp(&fakeAuth{token: "x"}, entries)

	_, _, err = run(t, app, "edit", "--id=entry-1", "--password=newpw")
	require.NoError(t, err)

	// Get was called before Edit (fetch-modify-put).
	require.Equal(t, []string{"entry-1"}, entries.getCalls)
	require.Len(t, entries.edited, 1)
	saved := entries.edited[0]
	require.Equal(t, int64(4), saved.Version) // bumped by the fake on re-put
	login, err := decodeLogin(saved.Data)
	require.NoError(t, err)
	require.Equal(t, "alice", login.Username) // kept
	require.Equal(t, "newpw", login.Password) // changed
	require.Equal(t, "github", saved.Label)
	require.Contains(t, out.String(), "updated entry entry-1 (version 4)")
}

func TestEditCommandNothingToUpdate(t *testing.T) {
	entries := newFakeEntries(&model.Entry{ID: "e1", Type: model.EntryTypeText, Label: "n", Data: []byte("x"), Version: 1})
	app, _, errOut := newTestApp(&fakeAuth{token: "x"}, entries)

	_, _, err := run(t, app, "edit", "--id=e1")
	require.Error(t, err)
	require.Contains(t, errOut.String(), "nothing to update")
	// No get, no edit.
	require.Empty(t, entries.getCalls)
	require.Empty(t, entries.edited)
}

func TestEditCommandGetFailureAborts(t *testing.T) {
	entries := newFakeEntries()
	entries.getErr = gateway.ErrNotFound
	app, _, errOut := newTestApp(&fakeAuth{token: "x"}, entries)

	_, _, err := run(t, app, "edit", "--id=nope", "--label=x")
	require.Error(t, err)
	require.Contains(t, errOut.String(), "entry not found")
	require.Empty(t, entries.edited)
}

func TestDeleteCommandWithYes(t *testing.T) {
	entries := newFakeEntries(&model.Entry{ID: "entry-1", Type: model.EntryTypeText, Label: "n", Data: []byte("x"), Version: 1})
	app, out, _ := newTestApp(&fakeAuth{token: "x"}, entries)

	_, _, err := run(t, app, "delete", "--id=entry-1", "--yes")
	require.NoError(t, err)
	require.Equal(t, []string{"entry-1"}, entries.removed)
	require.Contains(t, out.String(), "deleted entry entry-1")
}

func TestDeleteCommandConfirmed(t *testing.T) {
	entries := newFakeEntries(&model.Entry{ID: "entry-1", Type: model.EntryTypeText, Label: "n", Data: []byte("x"), Version: 1})
	app, _, _ := newTestApp(&fakeAuth{token: "x"}, entries)
	app.In = strings.NewReader("y\n")

	_, _, err := run(t, app, "delete", "--id=entry-1")
	require.NoError(t, err)
	require.Equal(t, []string{"entry-1"}, entries.removed)
	// The prompt went to stderr.
	require.Contains(t, app.Err.(*bytes.Buffer).String(), "Delete entry entry-1?")
}

func TestDeleteCommandAborted(t *testing.T) {
	entries := newFakeEntries(&model.Entry{ID: "entry-1", Type: model.EntryTypeText, Label: "n", Data: []byte("x"), Version: 1})
	app, out, _ := newTestApp(&fakeAuth{token: "x"}, entries)
	app.In = strings.NewReader("n\n")

	_, _, err := run(t, app, "delete", "--id=entry-1")
	require.NoError(t, err)
	require.Empty(t, entries.removed)
	require.Contains(t, out.String(), "aborted")
}

func TestSyncCommand(t *testing.T) {
	entries := newFakeEntries(
		&model.Entry{ID: "aaaaaaaa-1", Type: model.EntryTypeText, Label: "a", Version: 1},
		&model.Entry{ID: "bbbbbbbb-2", Type: model.EntryTypeText, Label: "b", Version: 1},
	)
	app, out, _ := newTestApp(&fakeAuth{token: "x"}, entries)

	_, _, err := run(t, app, "sync")
	require.NoError(t, err)
	require.Equal(t, 1, entries.synced)
	text := out.String()
	require.Contains(t, text, "synced 2 entries")
	require.Contains(t, text, "replaces the local view with the server state")
	require.Contains(t, text, "aaaaaaaa")
	require.Contains(t, text, "bbbbbbbb")
}

func TestVersionCommand(t *testing.T) {
	app, out, _ := newTestApp(&fakeAuth{}, newFakeEntries())

	_, _, err := run(t, app, "version")
	require.NoError(t, err)
	text := out.String()
	require.Contains(t, text, "gophkeeper-client test-version")
	require.Contains(t, text, "built:    test-date")
	require.Contains(t, text, "commit:   test-commit")
	require.Contains(t, text, "platform: ")
}

func TestTUICommandRequiresAuthentication(t *testing.T) {
	app, _, errOut := newTestApp(&fakeAuth{}, newFakeEntries())

	_, _, err := run(t, app, "tui")
	require.Error(t, err)
	require.Contains(t, errOut.String(), "not authenticated, run `gophkeeper login`")
}

func TestTUICommandLaunchesWithServices(t *testing.T) {
	app, _, _ := newTestApp(&fakeAuth{token: "saved"}, newFakeEntries())
	var called bool
	var gotAuth authClient
	var gotEntries entryClient
	app.runTUI = func(auth authClient, entries entryClient) error {
		called = true
		gotAuth, gotEntries = auth, entries
		return nil
	}

	_, _, err := run(t, app, "tui")
	require.NoError(t, err)
	require.True(t, called)
	require.Same(t, app.auth, gotAuth)
	require.Same(t, app.entries, gotEntries)
}

func TestHelpAvailableWithoutServices(t *testing.T) {
	// Help must work without any services configured (main wires
	// them lazily).
	app, _, _ := newTestApp(nil, nil)
	app.connect = nil

	_, _, err := run(t, app, "--help")
	require.NoError(t, err)
	require.Contains(t, app.Out.(*bytes.Buffer).String(), "Usage:")
}

func TestFriendlyMessageMapping(t *testing.T) {
	cases := []struct {
		err      error
		expected string
	}{
		{nil, ""},
		{ErrInvalidCredentials, "invalid login or password"},
		{ErrNotAuthenticated, "not authenticated, run `gophkeeper login`"},
		{gateway.ErrUnauthenticated, "not authenticated, run `gophkeeper login`"},
		{gateway.ErrConflict, "conflict: entry was modified by another client, re-fetch with `gophkeeper get --id=...` and retry"},
		{gateway.ErrNotFound, "entry not found"},
		{service.ErrEmptyLogin, "login must not be empty"},
		{errors.New("boom"), "boom"},
	}
	for _, tc := range cases {
		require.Equal(t, tc.expected, friendlyMessage(tc.err))
	}
}
