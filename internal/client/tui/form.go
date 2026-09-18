package tui

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbletea"

	"github.com/dmitriy/gophkeeper/internal/client/model"
	"github.com/dmitriy/gophkeeper/internal/client/render"
)

// selectableTypes is the order in which the form's type selector
// cycles through the supported entry types.
var selectableTypes = []model.EntryType{
	model.EntryTypeLoginPassword,
	model.EntryTypeText,
	model.EntryTypeBinary,
	model.EntryTypeCard,
}

// formField is a single labelled text input of the form.
type formField struct {
	name  string
	input textinput.Model
}

// formModel is the create/edit form. For new entries the first
// element is a type selector (left/right cycles through the types,
// rebuilding the type-specific fields); for edits the type is fixed
// and the fields are pre-filled from the entry being edited.
type formModel struct {
	// isNew is true when creating (Add) and false when editing
	// (Edit with the carried version).
	isNew bool
	// entryType is the selected entry type.
	entryType model.EntryType
	// fields are the form inputs: Label and Metadata first, then
	// the type-specific ones.
	fields []formField
	// focus is the focused element: 0 is the type selector for new
	// entries, otherwise an index into fields.
	focus int
	// err is the validation/save error shown inside the form.
	err string
	// source is the entry being edited; nil for creation.
	source *model.Entry
}

// secretFieldNames are the form fields whose input is echoed as
// asterisks (the password and the card CVV).
var secretFieldNames = map[string]bool{
	"Password": true,
	"CVV":      true,
}

// newInput builds a labelled text input with an optional value.
// Secret fields (password, CVV) are masked; everything else echoes
// in the clear.
func newInput(name, placeholder, value string) formField {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.SetValue(value)
	ti.CharLimit = 0
	if secretFieldNames[name] {
		ti.EchoMode = textinput.EchoPassword
		ti.EchoCharacter = '•'
	}
	return formField{name: name, input: ti}
}

// newForm returns an empty creation form starting at the login type.
func newForm() formModel {
	f := formModel{isNew: true, entryType: model.EntryTypeLoginPassword}
	f.fields = typeFields(f.entryType, "", "")
	f.clampFocus()
	return f
}

// newFormForEdit returns an edit form pre-filled from entry.
func newFormForEdit(entry *model.Entry) formModel {
	f := formModel{entryType: entry.Type, source: entry}

	var data []string
	switch entry.Type {
	case model.EntryTypeLoginPassword:
		if login, err := render.DecodeLogin(entry.Data); err == nil {
			data = []string{login.Username, login.Password}
		}
	case model.EntryTypeCard:
		if card, err := render.DecodeCard(entry.Data); err == nil {
			data = []string{card.Number, card.Holder, card.Expiry, card.CVV}
		}
	case model.EntryTypeText:
		data = []string{string(entry.Data)}
	}
	f.fields = typeFields(entry.Type, entry.Label, entry.Metadata, data...)
	f.clampFocus()
	return f
}

// typeFields builds the input list for the given type: Label and
// Metadata first, then the type-specific fields. data carries the
// pre-filled values of the type-specific fields (optional).
func typeFields(t model.EntryType, label, metadata string, data ...string) []formField {
	value := func(i int) string {
		if i < len(data) {
			return data[i]
		}
		return ""
	}

	fields := []formField{
		newInput("Label", "name shown in the list", label),
		newInput("Metadata", "optional notes", metadata),
	}
	switch t {
	case model.EntryTypeLoginPassword:
		fields = append(fields,
			newInput("Username", "login name", value(0)),
			newInput("Password", "password", value(1)))
	case model.EntryTypeText:
		fields = append(fields, newInput("Text", "note content (single line)", value(0)))
	case model.EntryTypeBinary:
		fields = append(fields, newInput("File path", "path to the file", value(0)))
	case model.EntryTypeCard:
		fields = append(fields,
			newInput("Number", "card number", value(0)),
			newInput("Holder", "cardholder name", value(1)),
			newInput("Expiry", "MM/YY", value(2)),
			newInput("CVV", "cvv", value(3)))
	}
	return fields
}

// elementCount is the number of focusable elements: the type
// selector (new entries only) plus the fields.
func (f formModel) elementCount() int {
	if f.isNew {
		return len(f.fields) + 1
	}
	return len(f.fields)
}

// fieldIndex maps the focus position to an index into fields
// (creating starts at the type selector, one before the fields).
func (f formModel) fieldIndex() int {
	if f.isNew {
		return f.focus - 1
	}
	return f.focus
}

// atLast reports whether the focus is on the last element.
func (f formModel) atLast() bool {
	return f.focus == f.elementCount()-1
}

// clampFocus brings the focus back into range (e.g. after a type
// change rebuilt the fields) and syncs the input focus state.
func (f *formModel) clampFocus() {
	if f.focus > f.elementCount()-1 {
		f.focus = f.elementCount() - 1
	}
	if f.focus < 0 {
		f.focus = 0
	}
	f.syncFocus()
}

// moveFocus advances or rewinds the focus, wrapping around, and
// syncs the input focus state.
func (f *formModel) moveFocus(delta int) {
	f.focus += delta
	if f.focus > f.elementCount()-1 {
		f.focus = 0
	}
	if f.focus < 0 {
		f.focus = f.elementCount() - 1
	}
	f.syncFocus()
}

// syncFocus mirrors the focus position on the inputs: only the
// focused field's input accepts keystrokes (the type selector is not
// an input and is skipped by the fieldIndex offset).
func (f *formModel) syncFocus() {
	active := f.fieldIndex()
	for i := range f.fields {
		if i == active {
			f.fields[i].input.Focus()
		} else {
			f.fields[i].input.Blur()
		}
	}
}

// cycleType switches to the next (dir > 0) or previous entry type
// and rebuilds the type-specific fields, preserving the common
// values typed so far.
func (f *formModel) cycleType(dir int) {
	pos := 0
	for i, t := range selectableTypes {
		if t == f.entryType {
			pos = i
			break
		}
	}
	pos = (pos + dir + len(selectableTypes)) % len(selectableTypes)

	label, metadata := f.value(0), f.value(1)
	f.entryType = selectableTypes[pos]
	f.fields = typeFields(f.entryType, label, metadata)
	f.clampFocus()
}

// value returns the current value of fields[i], "" when out of range.
func (f formModel) value(i int) string {
	if i >= 0 && i < len(f.fields) {
		return f.fields[i].input.Value()
	}
	return ""
}

// buildEntry validates the form and assembles the entry to save.
// Binary payloads are not read here: when a file path is given, the
// returned entry has empty Data and filePath is that path — the caller
// streams the file to the server instead of loading it into memory.
// On validation failure it returns a nil entry and a user-facing
// error.
func (f formModel) buildEntry() (*model.Entry, string, error) {
	label := strings.TrimSpace(f.value(0))
	if label == "" {
		return nil, "", fmt.Errorf("label must not be empty")
	}
	metadata := f.value(1)

	entry := &model.Entry{Label: label, Metadata: metadata}
	if f.isNew {
		entry.Type = f.entryType
	} else {
		entry = cloneEntry(f.source)
		entry.Label, entry.Metadata = label, metadata
	}

	var data []byte
	binaryPath := ""
	var err error
	switch entry.Type {
	case model.EntryTypeLoginPassword:
		username, password := f.value(2), f.value(3)
		if username == "" || password == "" {
			return nil, "", fmt.Errorf("username and password must not be empty")
		}
		if data, err = render.EncodeLogin(username, password); err != nil {
			return nil, "", err
		}
	case model.EntryTypeText:
		text := f.value(2)
		if text == "" {
			return nil, "", fmt.Errorf("text must not be empty")
		}
		data = []byte(text)
	case model.EntryTypeBinary:
		path := strings.TrimSpace(f.value(2))
		if path == "" {
			if f.isNew {
				return nil, "", fmt.Errorf("file path must not be empty")
			}
			// Editing without a new path keeps the stored content.
			data = f.source.Data
		} else {
			info, statErr := os.Stat(path)
			if statErr != nil {
				return nil, "", fmt.Errorf("read file: %w", statErr)
			}
			if info.Size() == 0 {
				return nil, "", fmt.Errorf("file is empty")
			}
			// Stream the file on save; do not read it into memory.
			binaryPath = path
		}
	case model.EntryTypeCard:
		number := strings.TrimSpace(f.value(2))
		if number == "" {
			return nil, "", fmt.Errorf("card number must not be empty")
		}
		if data, err = render.EncodeCard(number, f.value(3), f.value(4), f.value(5)); err != nil {
			return nil, "", err
		}
	default:
		return nil, "", fmt.Errorf("unsupported entry type")
	}
	entry.Data = data
	return entry, binaryPath, nil
}

// cloneEntry returns a shallow copy of e; Data is shared until the
// caller replaces it.
func cloneEntry(e *model.Entry) *model.Entry {
	copied := *e
	return &copied
}

// updateForm handles keys on the form screen: navigation between the
// fields, type cycling for new entries, typing into the focused
// input, and save (enter on the last element or ctrl+s anywhere).
func (m appModel) updateForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := m.form

	switch msg.String() {
	case "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "esc":
		m.popScreen()
		m.setStatus("cancelled")
		return m, nil
	case "ctrl+s":
		return m.trySave()
	case "enter":
		if f.atLast() {
			return m.trySave()
		}
		f.moveFocus(1)
	case "tab", "down":
		f.moveFocus(1)
	case "shift+tab", "up":
		f.moveFocus(-1)
	case "left", "right":
		if f.isNew && f.focus == 0 {
			if msg.String() == "right" {
				f.cycleType(1)
			} else {
				f.cycleType(-1)
			}
			m.form = f
			return m, nil
		}
		var nf formModel
		var cmd tea.Cmd
		nf, cmd = m.feedInput(msg)
		m.form = nf
		return m, cmd
	default:
		var nf formModel
		var cmd tea.Cmd
		nf, cmd = m.feedInput(msg)
		m.form = nf
		return m, cmd
	}

	m.form = f
	return m, nil
}

// feedInput forwards a key press to the currently focused input,
// returning the updated form and the input's command (cursor blink).
func (m appModel) feedInput(msg tea.KeyMsg) (formModel, tea.Cmd) {
	f := m.form
	idx := f.fieldIndex()
	if f.isNew && f.focus == 0 || idx < 0 || idx >= len(f.fields) {
		return f, nil
	}
	var cmd tea.Cmd
	f.fields[idx].input, cmd = f.fields[idx].input.Update(msg)
	return f, cmd
}

// trySave validates the form and, when it passes, issues the async
// save command; validation errors are shown inside the form. Binary
// entries with a file path go through the streaming upload instead.
func (m appModel) trySave() (tea.Model, tea.Cmd) {
	entry, binaryPath, err := m.form.buildEntry()
	if err != nil {
		m.form.err = err.Error()
		return m, nil
	}
	m.form.err = ""
	m.loading = true

	if binaryPath != "" {
		version := int64(0)
		if !m.form.isNew && m.form.source != nil {
			version = m.form.source.Version
		}
		return m, uploadCmd(m.entries, entry, version, binaryPath)
	}
	return m, saveCmd(m.entries, entry, m.form.isNew)
}

// viewForm renders the create/edit form.
func (m appModel) viewForm() string {
	f := m.form
	var sb strings.Builder

	title := "New entry"
	if !f.isNew && f.source != nil {
		title = fmt.Sprintf("Edit %q", f.source.Label)
	}
	sb.WriteString(titleStyle.Render(title) + "\n\n")

	if f.isNew {
		selector := fmt.Sprintf("  Type:    ◀ %s ▶", render.TypeLabel(f.entryType))
		if f.focus == 0 {
			sb.WriteString(selectedStyle.Render(selector) + "\n")
		} else {
			sb.WriteString(selector + "\n")
		}
	}

	active := f.fieldIndex()
	for i, field := range f.fields {
		prompt := fmt.Sprintf("  %-9s", field.name+":")
		line := prompt + field.input.View()
		if i == active {
			sb.WriteString(selectedStyle.Render("▸"+line[1:]) + "\n")
		} else {
			sb.WriteString("  " + line + "\n")
		}
	}

	if f.isNew && f.entryType == model.EntryTypeBinary {
		sb.WriteString(dimStyle.Render("  (the file is read on save)") + "\n")
	}
	if !f.isNew && f.entryType == model.EntryTypeBinary && f.value(2) == "" {
		sb.WriteString(dimStyle.Render("  (empty path keeps the current content)") + "\n")
	}

	if f.err != "" {
		sb.WriteString("\n" + statusErrStyle.Render("✗ "+f.err) + "\n")
	} else if m.statusErr && m.status != "" {
		sb.WriteString("\n" + statusErrStyle.Render("✗ "+m.status) + "\n")
	}

	sb.WriteString("\n" + helpStyle.Render("tab next field · shift+tab previous · ←/→ type · enter/ctrl+s save · esc cancel"))
	return sb.String()
}
