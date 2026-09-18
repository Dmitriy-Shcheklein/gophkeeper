package cli

import (
	"fmt"
	"io"

	"github.com/dmitriy/gophkeeper/internal/client/model"
	"github.com/dmitriy/gophkeeper/internal/client/render"
)

// typeFlagLogin and friends are the user-facing names of the entry
// types, used by the --type flag of add and list and by the TYPE
// table column. They live in the shared render package; the names
// are re-declared here for local readability.
const (
	typeFlagLogin  = render.TypeLogin
	typeFlagText   = render.TypeText
	typeFlagBinary = render.TypeBinary
	typeFlagCard   = render.TypeCard
)

// loginData and cardData are the JSON payload types of the login and
// card entries; the codecs live in the shared render package.
type (
	loginData = render.LoginData
	cardData  = render.CardData
)

// parseTypeFlag converts a --type flag value into an entry type,
// rejecting everything that is not one of the supported names.
func parseTypeFlag(value string) (model.EntryType, error) {
	return render.ParseType(value)
}

// typeLabel returns the user-facing name of an entry type (the TYPE
// table column, get output). Unknown values render as "unknown".
func typeLabel(t model.EntryType) string {
	return render.TypeLabel(t)
}

// encodeLogin serializes a login/password pair into the entry
// payload; see render.EncodeLogin.
func encodeLogin(username, password string) ([]byte, error) {
	return render.EncodeLogin(username, password)
}

// decodeLogin parses a login entry payload produced by encodeLogin.
func decodeLogin(data []byte) (loginData, error) {
	return render.DecodeLogin(data)
}

// encodeCard serializes card fields into the entry payload; see
// render.EncodeCard.
func encodeCard(number, holder, expiry, cvv string) ([]byte, error) {
	return render.EncodeCard(number, holder, expiry, cvv)
}

// decodeCard parses a card entry payload produced by encodeCard.
func decodeCard(data []byte) (cardData, error) {
	return render.DecodeCard(data)
}

// readTextPayload resolves the text payload: a literal value, or the
// contents of stdin when value is "-".
func readTextPayload(value string, in io.Reader) (string, error) {
	if value != "-" {
		return value, nil
	}
	data, err := io.ReadAll(in)
	if err != nil {
		return "", fmt.Errorf("read text from stdin: %w", err)
	}
	return string(data), nil
}
