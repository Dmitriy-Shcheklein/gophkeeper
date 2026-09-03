package render

import (
	"bytes"
	"testing"
	"time"

	"github.com/dmitriy/gophkeeper/internal/client/model"
	"github.com/stretchr/testify/require"
)

func TestShortID(t *testing.T) {
	require.Equal(t, "", ShortID(""))
	require.Equal(t, "abc", ShortID("abc"))
	require.Equal(t, "12345678", ShortID("12345678"))
	require.Equal(t, "12345678", ShortID("1234567890abcdef"))
}

func TestMaskCardNumber(t *testing.T) {
	require.Equal(t, "", MaskCardNumber(""))
	require.Equal(t, "*", MaskCardNumber("1"))
	require.Equal(t, "****", MaskCardNumber("1234"))
	require.Equal(t, "*2345", MaskCardNumber("12345"))
	require.Equal(t, "************1111", MaskCardNumber("4111111111111111"))
}

func TestMaskCVV(t *testing.T) {
	require.Equal(t, "", MaskCVV(""))
	require.Equal(t, "***", MaskCVV("123"))
	require.Equal(t, "****", MaskCVV("1234"))
}

func TestTableRender(t *testing.T) {
	var out bytes.Buffer
	tbl := newTable("ID", "TYPE", "VERSION")
	tbl.addRow("abc12345", "login", "1")
	tbl.addRow("x", "card", "12")
	tbl.render(&out)

	lines := splitLines(out.String())
	require.Len(t, lines, 4) // header, separator, 2 rows
	// Columns align: ID column is 8 wide, TYPE 5, VERSION 7.
	require.Equal(t, "ID        TYPE   VERSION", lines[0])
	require.Equal(t, "--------  -----  -------", lines[1])
	require.Equal(t, "abc12345  login  1", lines[2])
	require.Equal(t, "x         card   12", lines[3])
}

func TestTableRenderEmpty(t *testing.T) {
	var out bytes.Buffer
	newTable("ID").render(&out)
	require.Equal(t, "ID\n--\n", out.String())
}

func TestRenderTableEmpty(t *testing.T) {
	var out bytes.Buffer
	Table(&out, nil)
	require.Equal(t, "no entries\n", out.String())
}

func TestRenderTable(t *testing.T) {
	updated := time.Date(2026, 9, 2, 11, 0, 0, 0, time.UTC)
	entries := []*model.Entry{
		{ID: "aaaaaaaa-1111", Type: model.EntryTypeLoginPassword, Label: "github", Version: 3, UpdatedAt: updated},
		{ID: "bbbbbbbb-2222", Type: model.EntryTypeCard, Label: "bank", Version: 1, UpdatedAt: updated},
	}
	var out bytes.Buffer
	Table(&out, entries)

	lines := splitLines(out.String())
	require.Len(t, lines, 4)
	require.Equal(t, "ID        TYPE   LABEL   VERSION  UPDATED", lines[0])
	require.Contains(t, lines[2], "aaaaaaaa")
	require.Contains(t, lines[2], "login")
	require.Contains(t, lines[2], "github")
	require.Contains(t, lines[2], "2026-09-02T11:00:00Z")
	require.Contains(t, lines[3], "card")
}

func TestRenderEntryLogin(t *testing.T) {
	data, err := EncodeLogin("alice", "s3cret")
	require.NoError(t, err)
	var out bytes.Buffer
	Entry(&out, &model.Entry{
		ID: "id-123456789", Type: model.EntryTypeLoginPassword, Label: "github",
		Metadata: "work", Version: 2,
		CreatedAt: time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 9, 2, 11, 0, 0, 0, time.UTC),
		Data:      data,
	})

	text := out.String()
	require.Contains(t, text, "ID:       id-123456789")
	require.Contains(t, text, "Type:     login")
	require.Contains(t, text, "Label:    github")
	require.Contains(t, text, "Metadata: work")
	require.Contains(t, text, "Version:  2")
	require.Contains(t, text, "username: alice")
	// Login passwords render in the clear: it is the user's own secret.
	require.Contains(t, text, "password: s3cret")
}

func TestRenderEntryCardMasks(t *testing.T) {
	data, err := EncodeCard("4111111111111111", "Alice Smith", "12/28", "123")
	require.NoError(t, err)
	var out bytes.Buffer
	Entry(&out, &model.Entry{
		ID: "id-1", Type: model.EntryTypeCard, Label: "bank", Version: 1, Data: data,
	})

	text := out.String()
	require.Contains(t, text, "number: ************1111")
	require.Contains(t, text, "holder: Alice Smith")
	require.Contains(t, text, "expiry: 12/28")
	// CVV never renders in the clear.
	require.Contains(t, text, "cvv:    ***")
	require.NotContains(t, text, "4111")
	require.NotContains(t, text, "123\n") // masked everywhere
}

func TestRenderEntryTextAndBinary(t *testing.T) {
	var out bytes.Buffer
	Entry(&out, &model.Entry{ID: "id-2", Type: model.EntryTypeText, Label: "note", Data: []byte("hello world")})
	require.Contains(t, out.String(), "hello world")

	out.Reset()
	Entry(&out, &model.Entry{ID: "id-3", Type: model.EntryTypeBinary, Label: "file", Data: []byte("0123456789")})
	require.Contains(t, out.String(), "binary data (10 bytes)")
}

func TestRenderEntryUndecodable(t *testing.T) {
	var out bytes.Buffer
	Entry(&out, &model.Entry{ID: "id-4", Type: model.EntryTypeLoginPassword, Label: "bad", Data: []byte("junk")})
	require.Contains(t, out.String(), "undecodable login data")
}

func TestRenderEntryString(t *testing.T) {
	e := &model.Entry{ID: "id-5", Type: model.EntryTypeText, Label: "note", Data: []byte("hello")}
	require.Contains(t, EntryString(e), "hello")
}

func TestTypeLabel(t *testing.T) {
	require.Equal(t, "login", TypeLabel(model.EntryTypeLoginPassword))
	require.Equal(t, "text", TypeLabel(model.EntryTypeText))
	require.Equal(t, "binary", TypeLabel(model.EntryTypeBinary))
	require.Equal(t, "card", TypeLabel(model.EntryTypeCard))
	require.Equal(t, "unknown", TypeLabel(model.EntryTypeUnspecified))
}

func TestParseType(t *testing.T) {
	got, err := ParseType("login")
	require.NoError(t, err)
	require.Equal(t, model.EntryTypeLoginPassword, got)

	got, err = ParseType("text")
	require.NoError(t, err)
	require.Equal(t, model.EntryTypeText, got)

	got, err = ParseType("binary")
	require.NoError(t, err)
	require.Equal(t, model.EntryTypeBinary, got)

	got, err = ParseType("card")
	require.NoError(t, err)
	require.Equal(t, model.EntryTypeCard, got)

	_, err = ParseType("nope")
	require.Error(t, err)
	require.Contains(t, err.Error(), `invalid type "nope"`)
}

// splitLines splits rendered output into non-empty lines.
func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			if i > start {
				lines = append(lines, s[start:i])
			}
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
