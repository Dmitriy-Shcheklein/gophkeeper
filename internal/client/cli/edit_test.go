package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dmitriy/gophkeeper/internal/client/model"
	"github.com/stretchr/testify/require"
)

// writeFile creates a temporary file with the given content and
// returns its path.
func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

// editApp returns an app for applyEditOptions with the given stdin.
func editApp(stdin string) *App {
	return &App{In: strings.NewReader(stdin), Out: &strings.Builder{}, Err: &strings.Builder{}}
}

func strp(s string) *string { return &s }

func providedOpts(values map[string]string) *editOptions {
	opts := &editOptions{provided: make(map[string]bool)}
	for name, value := range values {
		opts.provided[name] = true
		switch name {
		case "label":
			opts.label = value
		case "metadata":
			opts.metadata = value
		case "username":
			opts.username = value
		case "password":
			opts.password = value
		case "text":
			opts.text = value
		case "file":
			opts.file = value
		case "number":
			opts.number = value
		case "holder":
			opts.holder = value
		case "expiry":
			opts.expiry = value
		case "cvv":
			opts.cvv = value
		}
	}
	return opts
}

func loginEntry() *model.Entry {
	data, _ := encodeLogin("alice", "old-pass")
	return &model.Entry{
		ID: "entry-1", Type: model.EntryTypeLoginPassword, Label: "github",
		Metadata: "work", Data: data, Version: 3,
	}
}

func TestApplyEditOptionsNothingProvided(t *testing.T) {
	_, err := applyEditOptions(editApp(""), loginEntry(), &editOptions{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "nothing to update")
}

func TestApplyEditOptionsLabelAndMetadata(t *testing.T) {
	updated, err := applyEditOptions(editApp(""), loginEntry(),
		providedOpts(map[string]string{"label": "corp", "metadata": ""}))
	require.NoError(t, err)
	require.Equal(t, "corp", updated.Label)
	require.Equal(t, "", updated.Metadata) // explicit empty clears
	// Version carries over for the optimistic lock.
	require.Equal(t, int64(3), updated.Version)
	// Payload untouched.
	login, err := decodeLogin(updated.Data)
	require.NoError(t, err)
	require.Equal(t, "alice", login.Username)
	require.Equal(t, "old-pass", login.Password)
}

func TestApplyEditOptionsLoginMerge(t *testing.T) {
	updated, err := applyEditOptions(editApp(""), loginEntry(),
		providedOpts(map[string]string{"password": "new-pass"}))
	require.NoError(t, err)
	require.Equal(t, "github", updated.Label)
	login, err := decodeLogin(updated.Data)
	require.NoError(t, err)
	require.Equal(t, "alice", login.Username)
	require.Equal(t, "new-pass", login.Password)
}

func TestApplyEditOptionsLoginRejectsForeignFlags(t *testing.T) {
	_, err := applyEditOptions(editApp(""), loginEntry(),
		providedOpts(map[string]string{"text": "nope"}))
	require.Error(t, err)
	require.Contains(t, err.Error(), "flag --text does not belong to this entry type")

	_, err = applyEditOptions(editApp(""), loginEntry(),
		providedOpts(map[string]string{"file": "nope"}))
	require.Error(t, err)
	require.Contains(t, err.Error(), "flag --file does not belong to this entry type")
}

func TestApplyEditOptionsLoginEmptyRejected(t *testing.T) {
	_, err := applyEditOptions(editApp(""), loginEntry(),
		providedOpts(map[string]string{"username": ""}))
	require.Error(t, err)
	require.Contains(t, err.Error(), "non-empty username and password")
}

func TestApplyEditOptionsText(t *testing.T) {
	entry := &model.Entry{ID: "entry-2", Type: model.EntryTypeText, Label: "note", Data: []byte("old"), Version: 1}

	updated, err := applyEditOptions(editApp(""), entry,
		providedOpts(map[string]string{"text": "new text"}))
	require.NoError(t, err)
	require.Equal(t, []byte("new text"), updated.Data)

	updated, err = applyEditOptions(editApp("from stdin"), entry,
		providedOpts(map[string]string{"text": "-"}))
	require.NoError(t, err)
	require.Equal(t, []byte("from stdin"), updated.Data)

	// Empty replacement is rejected: payloads never become empty.
	_, err = applyEditOptions(editApp(""), entry,
		providedOpts(map[string]string{"text": ""}))
	require.Error(t, err)
	require.Contains(t, err.Error(), "must not be empty")
}

func TestApplyEditOptionsBinaryFile(t *testing.T) {
	path := writeFile(t, "bin", "file-bytes")
	entry := &model.Entry{ID: "entry-3", Type: model.EntryTypeBinary, Label: "blob", Data: []byte("old"), Version: 2}

	updated, err := applyEditOptions(editApp(""), entry,
		providedOpts(map[string]string{"file": path}))
	require.NoError(t, err)
	require.Equal(t, []byte("file-bytes"), updated.Data)
	require.Equal(t, int64(2), updated.Version)
}

func TestApplyEditOptionsBinaryMissingFile(t *testing.T) {
	entry := &model.Entry{ID: "entry-3", Type: model.EntryTypeBinary, Label: "blob", Data: []byte("old"), Version: 2}
	_, err := applyEditOptions(editApp(""), entry,
		providedOpts(map[string]string{"file": "/nonexistent/file"}))
	require.Error(t, err)
	require.Contains(t, err.Error(), "read --file")
}

func TestApplyEditOptionsCardMerge(t *testing.T) {
	data, err := encodeCard("4111111111111111", "Alice", "12/28", "123")
	require.NoError(t, err)
	entry := &model.Entry{ID: "entry-4", Type: model.EntryTypeCard, Label: "bank", Data: data, Version: 5}

	updated, err := applyEditOptions(editApp(""), entry,
		providedOpts(map[string]string{"holder": "Bob", "cvv": "999"}))
	require.NoError(t, err)
	card, err := decodeCard(updated.Data)
	require.NoError(t, err)
	require.Equal(t, "4111111111111111", card.Number) // kept
	require.Equal(t, "Bob", card.Holder)
	require.Equal(t, "12/28", card.Expiry) // kept
	require.Equal(t, "999", card.CVV)
}

func TestApplyEditOptionsCardEmptyNumberRejected(t *testing.T) {
	data, err := encodeCard("4111111111111111", "Alice", "12/28", "123")
	require.NoError(t, err)
	entry := &model.Entry{ID: "entry-4", Type: model.EntryTypeCard, Label: "bank", Data: data, Version: 5}

	_, err = applyEditOptions(editApp(""), entry,
		providedOpts(map[string]string{"number": ""}))
	require.Error(t, err)
	require.Contains(t, err.Error(), "non-empty number")
}

func TestApplyEditOptionsUnknownType(t *testing.T) {
	entry := &model.Entry{ID: "entry-5", Type: model.EntryTypeUnspecified, Label: "weird", Data: []byte("x"), Version: 1}
	_, err := applyEditOptions(editApp(""), entry,
		providedOpts(map[string]string{"label": "new"}))
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot edit payload")
}
