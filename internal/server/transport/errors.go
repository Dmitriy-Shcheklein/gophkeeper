package transport

import (
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/dmitriy/gophkeeper/internal/server/model"
	"github.com/dmitriy/gophkeeper/internal/server/service"
)

// invalidArgumentErrors lists the validation sentinels of the service
// layer, mapped to codes.InvalidArgument. The sentinel messages name
// the offending field, which makes them safe and useful to return to
// clients verbatim. Keep in sync with validation sentinels in
// service/auth.go and service/entry.go; unmapped sentinels fall
// through to Internal.
var invalidArgumentErrors = []error{
	service.ErrEmptyLogin,
	service.ErrLoginTooLong,
	service.ErrEmptyPassword,
	service.ErrPasswordTooShort,
	service.ErrPasswordTooLong,
	service.ErrEmptyUserID,
	service.ErrEmptyEntryID,
	service.ErrInvalidEntryType,
	service.ErrEmptyLabel,
	service.ErrLabelTooLong,
	service.ErrEmptyData,
	service.ErrMetadataTooLong,
	service.ErrInvalidVersion,
}

// toStatusError maps a service-layer error to a gRPC status error.
//
// Mapping decisions:
//   - model.ErrNotFound → NotFound;
//   - model.ErrAlreadyExists → AlreadyExists;
//   - model.ErrConflict → FailedPrecondition: it signals the
//     optimistic-locking precondition (matching version) was not met
//     and the client must re-fetch before retrying, which is exactly
//     the semantics FailedPrecondition documents (the call is valid in
//     principle but the system state does not allow it);
//   - model.ErrUnauthorized and service.ErrInvalidCredentials →
//     Unauthenticated;
//   - validation sentinels → InvalidArgument, with the sentinel
//     message identifying the offending field;
//   - anything else → Internal with a generic message, so internal
//     details (repository, database errors) never leak to clients.
func toStatusError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, service.ErrInvalidCredentials):
		return status.Error(codes.Unauthenticated, service.ErrInvalidCredentials.Error())
	case errors.Is(err, model.ErrUnauthorized):
		// Bare ErrUnauthorized currently never reaches clients
		// (Login wraps it with ErrInvalidCredentials, handled above);
		// this branch guards future authorization paths such as
		// cross-owner access checks.
		return status.Error(codes.Unauthenticated, "authentication required")
	case errors.Is(err, model.ErrNotFound):
		return status.Error(codes.NotFound, "entity not found")
	case errors.Is(err, model.ErrAlreadyExists):
		return status.Error(codes.AlreadyExists, "entity already exists")
	case errors.Is(err, model.ErrConflict):
		return status.Error(codes.FailedPrecondition, "conflict: the entry was modified, re-fetch and retry")
	}
	for _, sentinel := range invalidArgumentErrors {
		if errors.Is(err, sentinel) {
			return status.Error(codes.InvalidArgument, sentinel.Error())
		}
	}
	return status.Error(codes.Internal, "internal error")
}
