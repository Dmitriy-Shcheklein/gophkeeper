// Package auth provides JWT-based authentication primitives of the
// GophKeeper server: token generation and verification, the claims
// carried by tokens, and helpers to pass those claims through contexts.
package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims are the custom JWT claims issued by JWTManager.
type Claims struct {
	// UserID is the unique identifier of the authenticated user.
	UserID string `json:"user_id"`
	// Login is the login of the authenticated user.
	Login string `json:"login"`
	// RegisteredClaims embed the standard JWT claims (exp, iat, ...).
	jwt.RegisteredClaims
}

// JWTManager issues and verifies HS256 JSON Web Tokens.
type JWTManager struct {
	secret []byte
	ttl    time.Duration
}

// New creates a JWTManager. The secret must be non-empty and the ttl
// positive, otherwise an error is returned.
func New(secret string, ttl time.Duration) (*JWTManager, error) {
	if secret == "" {
		return nil, errors.New("auth: secret must not be empty")
	}
	if ttl <= 0 {
		return nil, errors.New("auth: ttl must be positive")
	}
	return &JWTManager{secret: []byte(secret), ttl: ttl}, nil
}

// Generate issues a signed HS256 token for the given user, valid for the
// manager's TTL.
func (m *JWTManager) Generate(userID, login string) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID: userID,
		Login:  login,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(m.ttl)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(m.secret)
	if err != nil {
		return "", fmt.Errorf("auth: sign token: %w", err)
	}
	return signed, nil
}

// signingMethodHS256 is the only JWT signing algorithm accepted by
// JWTManager.Verify, pinned to prevent algorithm confusion attacks.
var signingMethodHS256 = jwt.SigningMethodHS256

// Verify parses and validates a token signed with HS256. Tokens signed
// with any other algorithm (including other HMAC variants such as
// HS512) are rejected to prevent algorithm confusion attacks, and the
// verified claims are returned on success.
func (m *JWTManager) Verify(token string) (*Claims, error) {
	parsed, err := jwt.ParseWithClaims(token, &Claims{}, func(t *jwt.Token) (any, error) {
		if t.Method != signingMethodHS256 {
			return nil, fmt.Errorf("unexpected signing method %v", t.Header["alg"])
		}
		return m.secret, nil
	})
	if err != nil {
		return nil, fmt.Errorf("auth: verify token: %w", err)
	}
	claims, ok := parsed.Claims.(*Claims)
	if !ok || !parsed.Valid {
		return nil, errors.New("auth: invalid token claims")
	}
	return claims, nil
}
