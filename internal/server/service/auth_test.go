package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/dmitriy/gophkeeper/internal/server/auth"
	"github.com/dmitriy/gophkeeper/internal/server/model"
)

// mockUserRepo is a minimal in-memory UserRepository for tests.
type mockUserRepo struct {
	users map[string]*model.User
	// createErr, if set, is returned by Create.
	createErr error
	// getByLoginErr, if set, is returned by GetByLogin.
	getByLoginErr error
}

func newMockUserRepo() *mockUserRepo {
	return &mockUserRepo{users: make(map[string]*model.User)}
}

func (m *mockUserRepo) Create(_ context.Context, user *model.User) error {
	if m.createErr != nil {
		return m.createErr
	}
	if _, ok := m.users[user.Login]; ok {
		return model.ErrAlreadyExists
	}
	user.ID = "generated-" + user.Login
	m.users[user.Login] = user
	return nil
}

func (m *mockUserRepo) GetByLogin(_ context.Context, login string) (*model.User, error) {
	if m.getByLoginErr != nil {
		return nil, m.getByLoginErr
	}
	user, ok := m.users[login]
	if !ok {
		return nil, model.ErrNotFound
	}
	return user, nil
}

func (m *mockUserRepo) GetByID(_ context.Context, id string) (*model.User, error) {
	for _, user := range m.users {
		if user.ID == id {
			return user, nil
		}
	}
	return nil, model.ErrNotFound
}

func newTestService(t *testing.T) (*AuthService, *mockUserRepo) {
	t.Helper()
	repo := newMockUserRepo()
	mgr, err := auth.New("test-secret", time.Minute)
	require.NoError(t, err)
	return NewAuthService(repo, mgr), repo
}

func TestRegisterSuccess(t *testing.T) {
	svc, repo := newTestService(t)

	token, err := svc.Register(context.Background(), "alice", "strong-password")
	require.NoError(t, err)
	assert.NotEmpty(t, token)

	user := repo.users["alice"]
	require.NotNil(t, user)
	assert.Equal(t, "alice", user.Login)
	// Password must be stored hashed, never in plaintext.
	assert.NotEqual(t, "strong-password", user.PassHash)
	assert.NoError(t, bcrypt.CompareHashAndPassword([]byte(user.PassHash), []byte("strong-password")))

	// The token must verify and carry the user identity.
	claims, err := svc.jwt.Verify(token)
	require.NoError(t, err)
	assert.Equal(t, user.ID, claims.UserID)
	assert.Equal(t, "alice", claims.Login)
}

func TestRegisterDuplicate(t *testing.T) {
	svc, _ := newTestService(t)

	_, err := svc.Register(context.Background(), "alice", "strong-password")
	require.NoError(t, err)

	_, err = svc.Register(context.Background(), "alice", "another-password")
	require.ErrorIs(t, err, model.ErrAlreadyExists)
}

func TestRegisterValidation(t *testing.T) {
	tests := []struct {
		name     string
		login    string
		password string
	}{
		{"empty login", "", "strong-password"},
		{"too long login", strings.Repeat("a", 256), "strong-password"},
		{"empty password", "alice", ""},
		{"short password", "alice", "short"},
		{"too long password", "alice", strings.Repeat("a", 73)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _ := newTestService(t)
			_, err := svc.Register(context.Background(), tt.login, tt.password)
			assert.Error(t, err)
		})
	}

	// 72 bytes is exactly the bcrypt limit and must be accepted.
	svc, _ := newTestService(t)
	_, err := svc.Register(context.Background(), "alice", strings.Repeat("a", 72))
	assert.NoError(t, err)
}

func TestLoginSuccess(t *testing.T) {
	svc, _ := newTestService(t)

	_, err := svc.Register(context.Background(), "alice", "strong-password")
	require.NoError(t, err)

	token, err := svc.Login(context.Background(), "alice", "strong-password")
	require.NoError(t, err)

	claims, err := svc.jwt.Verify(token)
	require.NoError(t, err)
	assert.Equal(t, "alice", claims.Login)
}

func TestLoginWrongPassword(t *testing.T) {
	svc, _ := newTestService(t)

	_, err := svc.Register(context.Background(), "alice", "strong-password")
	require.NoError(t, err)

	_, err = svc.Login(context.Background(), "alice", "wrong-password")
	require.ErrorIs(t, err, model.ErrUnauthorized)
	assert.ErrorIs(t, err, ErrInvalidCredentials)
}

func TestLoginUnknownUser(t *testing.T) {
	svc, _ := newTestService(t)

	_, err := svc.Login(context.Background(), "ghost", "strong-password")
	require.ErrorIs(t, err, model.ErrUnauthorized)
	assert.ErrorIs(t, err, ErrInvalidCredentials)
}

func TestLoginWrongPasswordMatchesUnknownUserError(t *testing.T) {
	svc, _ := newTestService(t)

	_, err := svc.Register(context.Background(), "alice", "strong-password")
	require.NoError(t, err)

	_, wrongPassErr := svc.Login(context.Background(), "alice", "wrong-password")
	_, unknownUserErr := svc.Login(context.Background(), "ghost", "strong-password")
	require.Error(t, wrongPassErr)
	require.Error(t, unknownUserErr)
	// Same error for both cases: do not reveal user existence.
	assert.Equal(t, wrongPassErr.Error(), unknownUserErr.Error())
}

func TestLoginValidation(t *testing.T) {
	svc, _ := newTestService(t)

	_, err := svc.Login(context.Background(), "", "strong-password")
	assert.Error(t, err)
}

func TestDummyHashIsWellFormed(t *testing.T) {
	// A malformed dummy hash would fail fast and defeat the timing
	// side-channel defense, so it must parse as a valid bcrypt hash at
	// the default cost (mismatch is expected and fine).
	err := bcrypt.CompareHashAndPassword(dummyHash, []byte("whatever"))
	require.Error(t, err)
	assert.ErrorIs(t, err, bcrypt.ErrMismatchedHashAndPassword)
}

func TestRegisterRepoErrorPropagates(t *testing.T) {
	repo := newMockUserRepo()
	repo.createErr = errors.New("boom")
	mgr, err := auth.New("test-secret", time.Minute)
	require.NoError(t, err)
	svc := NewAuthService(repo, mgr)

	_, err = svc.Register(context.Background(), "alice", "strong-password")
	assert.ErrorContains(t, err, "boom")
}
