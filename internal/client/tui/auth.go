package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbletea"
)

// authModel is the login/register screen shown when the TUI starts
// without a saved token. The first focusable element is the mode
// selector (login ↔ register, cycled with the arrow keys); the other
// two are the credential inputs. The password is echoed as asterisks.
type authModel struct {
	// register selects the register mode instead of login.
	register bool
	// fields are the credential inputs: Login first, Password second.
	fields []formField
	// focus is the focused element: 0 is the mode selector, 1 Login,
	// 2 Password.
	focus int
	// err is the submit/validation error shown inside the screen.
	err string
}

// newAuthModel returns an auth screen in login mode with the login
// input focused.
func newAuthModel() authModel {
	login := newInput("Login", "account login", "")
	login.input.Prompt = ""
	password := newInput("Password", "password", "")
	password.input.Prompt = ""
	m := authModel{
		fields: []formField{login, password},
		focus:  1,
	}
	m.syncFocus()
	return m
}

// elementCount is the number of focusable elements: the mode
// selector plus the two inputs.
func (a authModel) elementCount() int {
	return len(a.fields) + 1
}

// syncFocus mirrors the focus position on the inputs: the mode
// selector (focus 0) is not an input.
func (a *authModel) syncFocus() {
	for i := range a.fields {
		if i == a.focus-1 {
			a.fields[i].input.Focus()
		} else {
			a.fields[i].input.Blur()
		}
	}
}

// moveFocus advances or rewinds the focus, wrapping around, and
// syncs the input focus state.
func (a *authModel) moveFocus(delta int) {
	a.focus += delta
	if a.focus > a.elementCount()-1 {
		a.focus = 0
	}
	if a.focus < 0 {
		a.focus = a.elementCount() - 1
	}
	a.syncFocus()
}

// toggleMode switches between login and register (keeping the typed
// values) and moves the focus to the login input.
func (a *authModel) toggleMode() {
	a.register = !a.register
	a.focus = 1
	a.syncFocus()
}

// modeLabel returns the human-readable name of the current mode.
func (a authModel) modeLabel() string {
	if a.register {
		return "register"
	}
	return "login"
}

// submit validates the inputs and calls the matching service method.
// Client-side validation errors (empty login/password) are returned
// for in-screen display; network errors are returned too.
func (a authModel) submit(ctx context.Context, auth authClient) error {
	login := strings.TrimSpace(a.fields[0].input.Value())
	password := a.fields[1].input.Value()
	if a.register {
		return auth.Register(ctx, login, password)
	}
	return auth.Login(ctx, login, password)
}

// authCmd submits the auth screen in the background.
func authCmd(auth authClient, a authModel) tea.Cmd {
	return func() tea.Msg {
		err := a.submit(context.Background(), auth)
		return authedMsg{err: err}
	}
}

// authedMsg carries the result of an async login/register.
type authedMsg struct{ err error }

// updateAuth handles keys on the auth screen: focus navigation,
// mode cycling on the selector, typing, submit and quit. esc quits
// the program: there is nothing to go back to before authentication.
func (m appModel) updateAuth(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	a := m.auth
	switch msg.String() {
	case "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "esc":
		m.quitting = true
		return m, tea.Quit
	case "ctrl+s":
		return m.tryAuth()
	case "enter":
		if a.focus == a.elementCount()-1 {
			return m.tryAuth()
		}
		a.moveFocus(1)
	case "tab", "down":
		a.moveFocus(1)
	case "shift+tab", "up":
		a.moveFocus(-1)
	case "left", "right":
		if a.focus == 0 {
			a.toggleMode()
			m.auth = a
			return m, nil
		}
		return m.feedAuthInput(msg)
	default:
		return m.feedAuthInput(msg)
	}

	m.auth = a
	return m, nil
}

// feedAuthInput forwards a key press to the focused input.
func (m appModel) feedAuthInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	a := m.auth
	if a.focus < 1 || a.focus > len(a.fields) {
		return m, nil
	}
	var cmd tea.Cmd
	a.fields[a.focus-1].input, cmd = a.fields[a.focus-1].input.Update(msg)
	m.auth = a
	return m, cmd
}

// tryAuth validates the inputs and issues the async submit; errors
// are shown inside the auth screen.
func (m appModel) tryAuth() (tea.Model, tea.Cmd) {
	a := m.auth
	login := strings.TrimSpace(a.fields[0].input.Value())
	password := a.fields[1].input.Value()
	if login == "" {
		a.err = "login must not be empty"
		m.auth = a
		return m, nil
	}
	if password == "" {
		a.err = "password must not be empty"
		m.auth = a
		return m, nil
	}
	a.err = ""
	m.loading = true
	m.auth = a
	return m, authCmd(m.authSvc, a)
}

// viewAuth renders the login/register screen.
func (m appModel) viewAuth() string {
	a := m.auth
	var sb strings.Builder

	title := "Login to GophKeeper"
	if a.register {
		title = "Register a new account"
	}
	sb.WriteString(titleStyle.Render(title) + "\n\n")

	selector := fmt.Sprintf("  Mode:    ◀ %s ▶", a.modeLabel())
	if a.focus == 0 {
		sb.WriteString(selectedStyle.Render(selector) + "\n")
	} else {
		sb.WriteString(selector + "\n")
	}

	for i, field := range a.fields {
		prompt := fmt.Sprintf("  %-9s", field.name+":")
		line := prompt + field.input.View()
		if i == a.focus-1 {
			sb.WriteString(selectedStyle.Render("▸"+line[1:]) + "\n")
		} else {
			sb.WriteString("  " + line + "\n")
		}
	}

	if a.register {
		sb.WriteString(dimStyle.Render("  (the account is created on the server)") + "\n")
	}

	if a.err != "" {
		sb.WriteString("\n" + statusErrStyle.Render("✗ "+a.err) + "\n")
	} else if m.loading {
		sb.WriteString("\n" + dimStyle.Render("  contacting server...") + "\n")
	}

	sb.WriteString("\n" + helpStyle.Render("tab next field · shift+tab previous · ←/→ login/register · enter/ctrl+s submit · esc quit"))
	return sb.String()
}
