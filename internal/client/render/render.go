// Package render holds the presentation helpers shared by the CLI
// commands and the TUI screens: entry type naming, payload codecs and
// the plain-text rendering of entries and entry tables.
//
// The payload JSON formats (login and card data) are part of the
// format contract documented in the client package docs; the codecs
// live here so both frontends serialize and parse identically.
package render

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/dmitriy/gophkeeper/internal/client/model"
)

// typeLogin and friends are the user-facing names of the entry types,
// used by the --type flag of add and list, the TYPE table column and
// the TUI.
const (
	TypeLogin  = "login"
	TypeText   = "text"
	TypeBinary = "binary"
	TypeCard   = "card"
)

// TypeLabel returns the user-facing name of an entry type (the TYPE
// table column, get output). Unknown values render as "unknown".
func TypeLabel(t model.EntryType) string {
	switch t {
	case model.EntryTypeLoginPassword:
		return TypeLogin
	case model.EntryTypeText:
		return TypeText
	case model.EntryTypeBinary:
		return TypeBinary
	case model.EntryTypeCard:
		return TypeCard
	default:
		return "unknown"
	}
}

// ParseType converts a user-supplied type name into an entry type,
// rejecting everything that is not one of the supported names.
func ParseType(value string) (model.EntryType, error) {
	switch value {
	case TypeLogin:
		return model.EntryTypeLoginPassword, nil
	case TypeText:
		return model.EntryTypeText, nil
	case TypeBinary:
		return model.EntryTypeBinary, nil
	case TypeCard:
		return model.EntryTypeCard, nil
	default:
		return model.EntryTypeUnspecified,
			fmt.Errorf("invalid type %q: must be one of %s, %s, %s, %s",
				value, TypeLogin, TypeText, TypeBinary, TypeCard)
	}
}

// ShortID truncates an entry id to 8 characters for the table view;
// shorter ids are returned unchanged.
func ShortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// MaskCardNumber hides all but the last 4 digits of a card number
// with asterisks; shorter values are fully masked.
func MaskCardNumber(number string) string {
	if len(number) <= 4 {
		return strings.Repeat("*", len(number))
	}
	return strings.Repeat("*", len(number)-4) + number[len(number)-4:]
}

// MaskCVV hides a CVV value completely.
func MaskCVV(cvv string) string {
	return strings.Repeat("*", len(cvv))
}

// LoginData is the JSON payload of a login entry. The JSON keys are
// fixed by the format contract (see the package docs).
//
// Keep in sync: the payload-format tables in README.md and
// docs/protocol.md document these formats too.
type LoginData struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// CardData is the JSON payload of a card entry.
type CardData struct {
	Number string `json:"number"`
	Holder string `json:"holder"`
	Expiry string `json:"expiry"`
	CVV    string `json:"cvv"`
}

// EncodeLogin serializes a login/password pair into the entry
// payload: JSON {"username":...,"password":...}.
func EncodeLogin(username, password string) ([]byte, error) {
	data, err := json.Marshal(LoginData{Username: username, Password: password})
	if err != nil {
		return nil, fmt.Errorf("encode login data: %w", err)
	}
	return data, nil
}

// DecodeLogin parses a login entry payload produced by EncodeLogin.
func DecodeLogin(data []byte) (LoginData, error) {
	var login LoginData
	if err := json.Unmarshal(data, &login); err != nil {
		return login, fmt.Errorf("decode login data: %w", err)
	}
	return login, nil
}

// EncodeCard serializes card fields into the entry payload: JSON
// {"number":...,"holder":...,"expiry":...,"cvv":...}.
func EncodeCard(number, holder, expiry, cvv string) ([]byte, error) {
	data, err := json.Marshal(CardData{Number: number, Holder: holder, Expiry: expiry, CVV: cvv})
	if err != nil {
		return nil, fmt.Errorf("encode card data: %w", err)
	}
	return data, nil
}

// DecodeCard parses a card entry payload produced by EncodeCard.
func DecodeCard(data []byte) (CardData, error) {
	var card CardData
	if err := json.Unmarshal(data, &card); err != nil {
		return card, fmt.Errorf("decode card data: %w", err)
	}
	return card, nil
}

// table is a minimal fixed-width text table: columns are padded to
// the widest cell, cells are separated by two spaces.
type table struct {
	headers []string
	rows    [][]string
}

// newTable returns a table with the given column headers.
func newTable(headers ...string) *table {
	return &table{headers: headers}
}

// addRow appends a row; rows must have as many cells as the table
// has headers.
func (t *table) addRow(cells ...string) {
	t.rows = append(t.rows, cells)
}

// render writes the table to w: a header line, a dashed separator
// and the data rows.
func (t *table) render(w io.Writer) {
	widths := make([]int, len(t.headers))
	for i, h := range t.headers {
		widths[i] = len(h)
	}
	for _, row := range t.rows {
		for i, cell := range row {
			if i < len(widths) && len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}

	pad := func(cell string, width int) string {
		return cell + strings.Repeat(" ", width-len(cell))
	}

	header := make([]string, len(t.headers))
	for i, h := range t.headers {
		header[i] = pad(h, widths[i])
	}
	_, _ = fmt.Fprintln(w, strings.TrimRight(strings.Join(header, "  "), " "))

	separator := make([]string, len(widths))
	for i, width := range widths {
		separator[i] = strings.Repeat("-", width)
	}
	_, _ = fmt.Fprintln(w, strings.Join(separator, "  "))

	for _, row := range t.rows {
		cells := make([]string, len(row))
		for i, cell := range row {
			cells[i] = pad(cell, widths[i])
		}
		_, _ = fmt.Fprintln(w, strings.TrimRight(strings.Join(cells, "  "), " "))
	}
}

// Table prints the entries as the shared ID/TYPE/LABEL/VERSION/
// UPDATED table used by list and sync; it prints "no entries" when
// the selection is empty.
func Table(w io.Writer, entries []*model.Entry) {
	if len(entries) == 0 {
		_, _ = fmt.Fprintln(w, "no entries")
		return
	}
	t := newTable("ID", "TYPE", "LABEL", "VERSION", "UPDATED")
	for _, e := range entries {
		t.addRow(ShortID(e.ID), TypeLabel(e.Type), e.Label,
			fmt.Sprintf("%d", e.Version), e.UpdatedAt.Format(time.RFC3339))
	}
	t.render(w)
}

// Entry prints the full entry with type-specific rendering of
// the payload: login fields in the clear, card number masked except
// the last 4 digits and the CVV fully masked, text payloads verbatim
// and binary payloads as a size with a hint.
func Entry(w io.Writer, e *model.Entry) {
	_, _ = fmt.Fprintf(w, "ID:       %s\n", e.ID)
	_, _ = fmt.Fprintf(w, "Type:     %s\n", TypeLabel(e.Type))
	_, _ = fmt.Fprintf(w, "Label:    %s\n", e.Label)
	if e.Metadata != "" {
		_, _ = fmt.Fprintf(w, "Metadata: %s\n", e.Metadata)
	}
	_, _ = fmt.Fprintf(w, "Version:  %d\n", e.Version)
	_, _ = fmt.Fprintf(w, "Created:  %s\n", e.CreatedAt.Format(time.RFC3339))
	_, _ = fmt.Fprintf(w, "Updated:  %s\n", e.UpdatedAt.Format(time.RFC3339))
	_, _ = fmt.Fprintln(w, "Data:")

	switch e.Type {
	case model.EntryTypeLoginPassword:
		login, err := DecodeLogin(e.Data)
		if err != nil {
			_, _ = fmt.Fprintf(w, "  <undecodable login data: %v>\n", err)
			return
		}
		_, _ = fmt.Fprintf(w, "  username: %s\n", login.Username)
		_, _ = fmt.Fprintf(w, "  password: %s\n", login.Password)
	case model.EntryTypeCard:
		card, err := DecodeCard(e.Data)
		if err != nil {
			_, _ = fmt.Fprintf(w, "  <undecodable card data: %v>\n", err)
			return
		}
		_, _ = fmt.Fprintf(w, "  number: %s\n", MaskCardNumber(card.Number))
		_, _ = fmt.Fprintf(w, "  holder: %s\n", card.Holder)
		_, _ = fmt.Fprintf(w, "  expiry: %s\n", card.Expiry)
		_, _ = fmt.Fprintf(w, "  cvv:    %s\n", MaskCVV(card.CVV))
	case model.EntryTypeText:
		_, _ = fmt.Fprintf(w, "  %s\n", string(e.Data))
	case model.EntryTypeBinary:
		_, _ = fmt.Fprintf(w, "  binary data (%d bytes) — preview not available\n", len(e.Data))
	default:
		_, _ = fmt.Fprintf(w, "  <%d bytes of unknown type data>\n", len(e.Data))
	}
}

// EntryString is the string-returning variant of Entry for backends
// that build UI strings (the TUI detail screen).
func EntryString(e *model.Entry) string {
	var sb strings.Builder
	Entry(&sb, e)
	return sb.String()
}
