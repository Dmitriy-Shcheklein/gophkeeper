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
// password and returns a signed JWT for the new user. Returns
// model.ErrAlreadyExists if the login is taken, or one of the validation
// sentinels above for invalid input.
func (s *AuthService) Register(ctx context.Context, login, password string) (string, error) {
	if err := validateLogin(login); err != nil {
		return "", err
	}
	if err := validatePassword(password); err != nil {
		return "", err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("service: hash password: %w", err)
	}

	user := &model.User{Login: login, PassHash: string(hash)}
	if err := s.users.Create(ctx, user); err != nil {
		return "", fmt.Errorf("service: create user: %w", err)
	}

	token, err := s.jwt.Generate(user.ID, user.Login)
	if err != nil {
		return "", fmt.Errorf("service: generate token: %w", err)
	}
	return token, nil
}

// Login validates the credentials, checks the password against the
// stored hash and returns a signed JWT. Unknown users and wrong
// passwords produce the same error (model.ErrUnauthorized wrapping
// ErrInvalidCredentials) to avoid revealing whether a login exists.
// Invalid input (e.g. empty login) returns the validation sentinels
// above.
func (s *AuthService) Login(ctx context.Context, login, password string) (string, error) {
	if err := validateLogin(login); err != nil {
		return "", err
	}
	if password == "" {
		return "", ErrEmptyPassword
	}

	user, err := s.users.GetByLogin(ctx, login)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return "", fmt.Errorf("service: login: %w: %w", model.ErrUnauthorized, ErrInvalidCredentials)
		}
		return "", fmt.Errorf("service: get user: %w", err)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PassHash), []byte(password)); err != nil {
		return "", fmt.Errorf("service: login: %w: %w", model.ErrUnauthorized, ErrInvalidCredentials)
	}

	token, err := s.jwt.Generate(user.ID, user.Login)
	if err != nil {
		return "", fmt.Errorf("service: generate token: %w", err)
	}
	return token, nil
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
