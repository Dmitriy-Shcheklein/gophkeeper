package auth

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		secret  string
		ttl     time.Duration
		wantErr bool
	}{
		{"valid", "secret", time.Minute, false},
		{"empty secret", "", time.Minute, true},
		{"zero ttl", "secret", 0, true},
		{"negative ttl", "secret", -time.Minute, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr, err := New(tt.secret, tt.ttl)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.NotNil(t, mgr)
		})
	}
}

func TestGenerateVerifyRoundtrip(t *testing.T) {
	mgr, err := New("test-secret", time.Minute)
	require.NoError(t, err)

	token, err := mgr.Generate("user-1", "alice")
	require.NoError(t, err)
	assert.NotEmpty(t, token)

	claims, err := mgr.Verify(token)
	require.NoError(t, err)
	assert.Equal(t, "user-1", claims.UserID)
	assert.Equal(t, "alice", claims.Login)
}

func TestGenerateTokenPayloadUsesSnakeCaseKeys(t *testing.T) {
	mgr, err := New("test-secret", time.Minute)
	require.NoError(t, err)

	token, err := mgr.Generate("user-1", "alice")
	require.NoError(t, err)

	// Parse the payload without verification and check the JSON keys.
	parts := strings.Split(token, ".")
	require.Len(t, parts, 3)
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	require.NoError(t, err)

	var raw map[string]any
	require.NoError(t, json.Unmarshal(payload, &raw))
	assert.Equal(t, "user-1", raw["user_id"])
	assert.Equal(t, "alice", raw["login"])
	assert.NotContains(t, raw, "UserID")
	assert.NotContains(t, raw, "Login")
}

func TestVerifyExpired(t *testing.T) {
	mgr, err := New("test-secret", time.Minute)
	require.NoError(t, err)

	// Craft an already-expired token signed with the manager's secret.
	claims := jwt.MapClaims{
		"user_id": "user-1",
		"login":   "alice",
		"exp":     time.Now().Add(-time.Minute).Unix(),
		"iat":     time.Now().Add(-2 * time.Minute).Unix(),
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("test-secret"))
	require.NoError(t, err)

	_, err = mgr.Verify(token)
	assert.Error(t, err)
}

func TestVerifyGarbage(t *testing.T) {
	mgr, err := New("test-secret", time.Minute)
	require.NoError(t, err)

	_, err = mgr.Verify("not-a-token")
	assert.Error(t, err)
}

func TestVerifyWrongSecret(t *testing.T) {
	issuer, err := New("secret-one", time.Minute)
	require.NoError(t, err)
	verifier, err := New("secret-two", time.Minute)
	require.NoError(t, err)

	token, err := issuer.Generate("user-1", "alice")
	require.NoError(t, err)

	_, err = verifier.Verify(token)
	assert.Error(t, err)
}

func TestVerifyWrongSigningMethod(t *testing.T) {
	mgr, err := New("test-secret", time.Minute)
	require.NoError(t, err)

	// Craft a token signed with a different algorithm but the same secret.
	claims := jwt.MapClaims{
		"user_id": "user-1",
		"login":   "alice",
		"exp":     time.Now().Add(time.Minute).Unix(),
		"iat":     time.Now().Unix(),
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodNone, claims).SignedString(jwt.UnsafeAllowNoneSignatureType)
	require.NoError(t, err)

	_, err = mgr.Verify(token)
	assert.Error(t, err)
}

func TestVerifyHS512Rejected(t *testing.T) {
	mgr, err := New("test-secret", time.Minute)
	require.NoError(t, err)

	// Craft a token signed with HS512 and the correct secret: it must
	// still be rejected, as only HS256 is accepted.
	claims := jwt.MapClaims{
		"user_id": "user-1",
		"login":   "alice",
		"exp":     time.Now().Add(time.Minute).Unix(),
		"iat":     time.Now().Unix(),
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS512, claims).SignedString([]byte("test-secret"))
	require.NoError(t, err)

	_, err = mgr.Verify(token)
	assert.Error(t, err)
}
