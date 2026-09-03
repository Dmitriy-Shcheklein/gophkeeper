// Package service implements the business logic of the GophKeeper
// server on top of repository interfaces and transport-agnostic models.
package service

import (
	"context"
	"errors"
	"fmt"

	"golang.org/x/crypto/bcrypt"

	"github.com/dmitriy/gophkeeper/internal/server/auth"
	"github.com/dmitriy/gophkeeper/internal/server/model"
	"github.com/dmitriy/gophkeeper/internal/server/repository"
)

// Input validation limits and errors of AuthService.
var (
	// ErrEmptyLogin is returned when the login is empty.
	ErrEmptyLogin = errors.New("login must not be empty")
	// ErrLoginTooLong is returned when the login exceeds maxLoginLen bytes.
	ErrLoginTooLong = errors.New("login is too long")
	// ErrEmptyPassword is returned when the password is empty.
	ErrEmptyPassword = errors.New("password must not be empty")
	// ErrPasswordTooShort is returned when the password is shorter than
	// minPasswordLen bytes.
	ErrPasswordTooShort = errors.New("password is too short")
	// ErrPasswordTooLong is returned when the password exceeds 72 bytes,
	// the maximum bcrypt operates on.
	ErrPasswordTooLong = errors.New("password is too long")
	// ErrInvalidCredentials is returned for failed logins. Unknown users
	// and wrong passwords yield the same error, wrapped together with
	// model.ErrUnauthorized, to avoid revealing whether a login exists.
	ErrInvalidCredentials = errors.New("invalid login or password")
)

const (
	// maxLoginLen is the maximum allowed login length in bytes.
	maxLoginLen = 255
	// minPasswordLen is the minimum allowed password length in bytes.
	minPasswordLen = 8
	// maxPasswordLen is the maximum password length bcrypt operates on.
	maxPasswordLen = 72
)

// dummyHash is a precomputed bcrypt hash of an arbitrary secret value.
// It is compared against the supplied password on the unknown-login path
// of Login to equalize the work done there with the wrong-password path
// (timing side-channel defense). Its value is intentionally fixed and
// corresponds to no real account.
var dummyHash = []byte("$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy")

// AuthService implements register/login business logic.
type AuthService struct {
	users repository.UserRepository
	jwt   *auth.JWTManager
}

// NewAuthService creates an AuthService backed by the given user
// repository and JWT manager.
func NewAuthService(users repository.UserRepository, jwt *auth.JWTManager) *AuthService {
	return &AuthService{users: users, jwt: jwt}
}

// Register validates the credentials, stores a bcrypt hash of the
// password and returns the created user together with a signed JWT
// for the new user. Returns model.ErrAlreadyExists if the login is
// taken, or one of the validation sentinels above for invalid input.
//
// The returned user is a copy with the password hash zeroed out, so
// the stored hash never leaves the service layer.
func (s *AuthService) Register(ctx context.Context, login, password string) (*model.User, string, error) {
	if err := validateLogin(login); err != nil {
		return nil, "", err
	}
	if err := validatePassword(password); err != nil {
		return nil, "", err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, "", fmt.Errorf("service: hash password: %w", err)
	}

	user := &model.User{Login: login, PassHash: string(hash)}
	if err := s.users.Create(ctx, user); err != nil {
		return nil, "", fmt.Errorf("service: create user: %w", err)
	}

	token, err := s.jwt.Generate(user.ID, user.Login)
	if err != nil {
		return nil, "", fmt.Errorf("service: generate token: %w", err)
	}
	// Like Login, never expose the password hash to the caller.
	returned := *user
	returned.PassHash = ""
	return &returned, token, nil
}

// Login validates the credentials, checks the password against the
// stored hash and returns the user together with a signed JWT.
// Unknown users and wrong passwords produce the same error
// (model.ErrUnauthorized wrapping ErrInvalidCredentials) to avoid
// revealing whether a login exists. Invalid input (e.g. empty login)
// returns the validation sentinels above.
//
// The returned user is a copy with the password hash zeroed out, so
// the stored hash never leaves the service layer.
func (s *AuthService) Login(ctx context.Context, login, password string) (*model.User, string, error) {
	if err := validateLogin(login); err != nil {
		return nil, "", err
	}
	if password == "" {
		return nil, "", ErrEmptyPassword
	}

	user, err := s.users.GetByLogin(ctx, login)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			// Timing side-channel defense: perform a bcrypt compare
			// against a fixed dummy hash so the not-found path takes
			// roughly as long as the wrong-password path, keeping the
			// returned error identical (no user enumeration).
			_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
			return nil, "", fmt.Errorf("service: login: %w: %w", model.ErrUnauthorized, ErrInvalidCredentials)
		}
		return nil, "", fmt.Errorf("service: get user: %w", err)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PassHash), []byte(password)); err != nil {
		return nil, "", fmt.Errorf("service: login: %w: %w", model.ErrUnauthorized, ErrInvalidCredentials)
	}

	token, err := s.jwt.Generate(user.ID, user.Login)
	if err != nil {
		return nil, "", fmt.Errorf("service: generate token: %w", err)
	}
	returned := *user
	returned.PassHash = ""
	return &returned, token, nil
}

// validateLogin checks the login against the allowed length limits.
func validateLogin(login string) error {
	if login == "" {
		return ErrEmptyLogin
	}
	if len(login) > maxLoginLen {
		return ErrLoginTooLong
	}
	return nil
}

// validatePassword checks the password against the allowed length
// limits, including the 72-byte bcrypt limit.
func validatePassword(password string) error {
	switch {
	case password == "":
		return ErrEmptyPassword
	case len(password) < minPasswordLen:
		return ErrPasswordTooShort
	case len(password) > maxPasswordLen:
		return ErrPasswordTooLong
	}
	return nil
}
