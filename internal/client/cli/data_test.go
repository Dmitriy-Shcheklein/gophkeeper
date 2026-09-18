package cli

import (
	"errors"
	"strings"
	"testing"

	"github.com/dmitriy/gophkeeper/internal/client/model"
	"github.com/stretchr/testify/require"
)

func TestParseTypeFlag(t *testing.T) {
	cases := []struct {
		value    string
		expected model.EntryType
		wantErr  bool
	}{
		{value: "login", expected: model.EntryTypeLoginPassword},
		{value: "text", expected: model.EntryTypeText},
		{value: "binary", expected: model.EntryTypeBinary},
		{value: "card", expected: model.EntryTypeCard},
		{value: "", wantErr: true},
		{value: "Login", wantErr: true},
		{value: "password", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.value, func(t *testing.T) {
			got, err := parseTypeFlag(tc.value)
			if tc.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), "invalid type")
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.expected, got)
		})
	}
}

func TestTypeLabel(t *testing.T) {
	require.Equal(t, "login", typeLabel(model.EntryTypeLoginPassword))
	require.Equal(t, "text", typeLabel(model.EntryTypeText))
	require.Equal(t, "binary", typeLabel(model.EntryTypeBinary))
	require.Equal(t, "card", typeLabel(model.EntryTypeCard))
	require.Equal(t, "unknown", typeLabel(model.EntryTypeUnspecified))
}

func TestLoginDataRoundTrip(t *testing.T) {
	data, err := encodeLogin("alice", "s3cret")
	require.NoError(t, err)

	login, err := decodeLogin(data)
	require.NoError(t, err)
	require.Equal(t, "alice", login.Username)
	require.Equal(t, "s3cret", login.Password)
}

func TestLoginDataJSONFormat(t *testing.T) {
	data, err := encodeLogin("alice", "s3cret")
	require.NoError(t, err)
	// The documented on-disk format: fixed key names.
	require.JSONEq(t, `{"username":"alice","password":"s3cret"}`, string(data))
}

func TestDecodeLoginInvalid(t *testing.T) {
	_, err := decodeLogin([]byte("not json"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "decode login data")
}

func TestCardDataRoundTrip(t *testing.T) {
	data, err := encodeCard("4111111111111111", "Alice Smith", "12/28", "123")
	require.NoError(t, err)
	require.JSONEq(t,
		`{"number":"4111111111111111","holder":"Alice Smith","expiry":"12/28","cvv":"123"}`,
		string(data))

	card, err := decodeCard(data)
	require.NoError(t, err)
	require.Equal(t, "4111111111111111", card.Number)
	require.Equal(t, "Alice Smith", card.Holder)
	require.Equal(t, "12/28", card.Expiry)
	require.Equal(t, "123", card.CVV)
}

func TestDecodeCardInvalid(t *testing.T) {
	_, err := decodeCard([]byte("[]"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "decode card data")
}

func TestReadTextPayload(t *testing.T) {
	t.Run("literal", func(t *testing.T) {
		text, err := readTextPayload("hello", strings.NewReader("ignored"))
		require.NoError(t, err)
		require.Equal(t, "hello", text)
	})
	t.Run("stdin", func(t *testing.T) {
		text, err := readTextPayload("-", strings.NewReader("from stdin\nsecond line"))
		require.NoError(t, err)
		require.Equal(t, "from stdin\nsecond line", text)
	})
	t.Run("stdin error", func(t *testing.T) {
		_, err := readTextPayload("-", errReader{})
		require.Error(t, err)
		require.Contains(t, err.Error(), "read text from stdin")
	})
}

// errReader always fails, simulating a stdin read error.
type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errReadFailed }

var errReadFailed = errors.New("read failed")
