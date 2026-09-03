// Package service implements the GophKeeper client business logic
// between the transport layers (CLI, TUI) and the gRPC gateway.
//
// # Lifecycle
//
// The GophKeeper CLI is one-shot per command: every invocation is a
// fresh process. The wiring is therefore:
//
//  1. the CLI constructs the token store, the gRPC Gateway (which
//     owns both gateways and persists tokens on login/register) and
//     the services;
//  2. the CLI calls AuthService.Restore to load a token persisted by
//     a previous invocation into the gateway — a missing token is a
//     normal state (the user is simply not logged in);
//  3. the command executes through the services, which validate
//     input client-side (fast-fail with sentinel errors) and
//     delegate to the gateways; gateway sentinels
//     (gateway.ErrNotFound, gateway.ErrConflict,
//     gateway.ErrUnauthenticated, ...) propagate unchanged so the
//     CLI can map them to friendly messages;
//  4. AuthService.Logout clears both the persisted token and the
//     in-memory one.
//
// The TUI follows the same lifecycle but keeps the process alive.
package service
