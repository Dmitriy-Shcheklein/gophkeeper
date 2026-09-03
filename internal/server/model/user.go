// Package model contains domain models of the GophKeeper server.
//
// Models are transport-agnostic: they do not depend on protobuf or gRPC
// definitions; conversion to/from wire formats happens in the transport layer.
package model

import "time"

// User is a registered GophKeeper account.
type User struct {
	// ID is the unique user identifier (UUID).
	ID string
	// Login is the unique user login used for authentication.
	Login string
	// PassHash is the stored password hash (never the plain password).
	PassHash string
	// CreatedAt is the moment the account was created.
	CreatedAt time.Time
}
