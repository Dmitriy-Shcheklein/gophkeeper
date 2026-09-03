// Package cli implements the GophKeeper client command-line
// interface built on cobra.
//
// Commands are thin: they parse flags, build the entry payload and
// delegate to the service layer (service.AuthService and
// service.EntryService) through small consumer-side interfaces
// declared in this package, so every command is testable with fakes
// and no gRPC stack.
//
// # Data formats
//
// Structured entry payloads are stored as JSON:
//
//   - login type: {"username":"...","password":"..."}
//   - card type:  {"number":"...","holder":"...","expiry":"...","cvv":"..."}
//
// Text entries store the raw text bytes; binary entries store the
// raw file bytes. The same JSON encoding is used by add and edit,
// and decoded by get for rendering.
//
// # Authentication UX
//
// Commands other than register, login, logout, version and tui
// require an authenticated session: the persisted token is restored
// first and, when absent, the command fails with a hint to run
// `gophkeeper login`. Passwords passed as --password are visible in
// shell history, so the help texts recommend omitting the flag and
// entering the password at the hidden prompt instead.
package cli
