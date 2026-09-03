package cli

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/dmitriy/gophkeeper/internal/client/model"
)

// shortID truncates an entry id to 8 characters for the table view;
// shorter ids are returned unchanged.
func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// maskCardNumber hides all but the last 4 digits of a card number
// with asterisks; shorter values are fully masked.
func maskCardNumber(number string) string {
	if len(number) <= 4 {
		return strings.Repeat("*", len(number))
	}
	return strings.Repeat("*", len(number)-4) + number[len(number)-4:]
}

// maskCVV hides a CVV value completely.
func maskCVV(cvv string) string {
	return strings.Repeat("*", len(cvv))
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

// renderTable prints the entries as the shared ID/TYPE/LABEL/VERSION/
// UPDATED table used by list and sync; it prints "no entries" when
// the selection is empty.
func renderTable(w io.Writer, entries []*model.Entry) {
	if len(entries) == 0 {
		_, _ = fmt.Fprintln(w, "no entries")
		return
	}
	t := newTable("ID", "TYPE", "LABEL", "VERSION", "UPDATED")
	for _, e := range entries {
		t.addRow(shortID(e.ID), typeLabel(e.Type), e.Label,
			fmt.Sprintf("%d", e.Version), e.UpdatedAt.Format(time.RFC3339))
	}
	t.render(w)
}

// renderEntry prints the full entry with type-specific rendering of
// the payload: login fields in the clear, card number masked except
// the last 4 digits and the CVV fully masked, text payloads verbatim
// and binary payloads as a size with a hint.
func renderEntry(w io.Writer, e *model.Entry) {
	_, _ = fmt.Fprintf(w, "ID:       %s\n", e.ID)
	_, _ = fmt.Fprintf(w, "Type:     %s\n", typeLabel(e.Type))
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
		login, err := decodeLogin(e.Data)
		if err != nil {
			_, _ = fmt.Fprintf(w, "  <undecodable login data: %v>\n", err)
			return
		}
		_, _ = fmt.Fprintf(w, "  username: %s\n", login.Username)
		_, _ = fmt.Fprintf(w, "  password: %s\n", login.Password)
	case model.EntryTypeCard:
		card, err := decodeCard(e.Data)
		if err != nil {
			_, _ = fmt.Fprintf(w, "  <undecodable card data: %v>\n", err)
			return
		}
		_, _ = fmt.Fprintf(w, "  number: %s\n", maskCardNumber(card.Number))
		_, _ = fmt.Fprintf(w, "  holder: %s\n", card.Holder)
		_, _ = fmt.Fprintf(w, "  expiry: %s\n", card.Expiry)
		_, _ = fmt.Fprintf(w, "  cvv:    %s\n", maskCVV(card.CVV))
	case model.EntryTypeText:
		_, _ = fmt.Fprintf(w, "  %s\n", string(e.Data))
	case model.EntryTypeBinary:
		_, _ = fmt.Fprintf(w, "  binary data (%d bytes) — preview not available\n", len(e.Data))
	default:
		_, _ = fmt.Fprintf(w, "  <%d bytes of unknown type data>\n", len(e.Data))
	}
}
