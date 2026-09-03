package model

import "errors"

// Sentinel errors returned by repositories and services.
var (
	// ErrNotFound is returned when the requested entity does not exist.
	ErrNotFound = errors.New("entity not found")
	// ErrAlreadyExists is returned when creating an entity that already
	// exists (e.g. a user with the given login).
	ErrAlreadyExists = errors.New("entity already exists")
	// ErrConflict is returned when an operation conflicts with the current
	// state of an entity (e.g. an outdated version during synchronization).
	ErrConflict = errors.New("conflict")
	// ErrUnauthorized is returned when authentication or authorization fails.
	ErrUnauthorized = errors.New("unauthorized")
)
