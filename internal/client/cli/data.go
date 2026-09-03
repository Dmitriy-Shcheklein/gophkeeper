package cli

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/dmitriy/gophkeeper/internal/client/model"
)

// typeFlagLogin and friends are the user-facing names of the entry
// types, used by the --type flag of add and list and by the TYPE
// table column.
const (
	typeFlagLogin  = "login"
	typeFlagText   = "text"
	typeFlagBinary = "binary"
	typeFlagCard   = "card"
)

// loginData is the JSON payload of a login entry. The JSON keys are
// fixed by the format contract (see the package docs).
type loginData struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// cardData is the JSON payload of a card entry.
type cardData struct {
	Number string `json:"number"`
	Holder string `json:"holder"`
	Expiry string `json:"expiry"`
	CVV    string `json:"cvv"`
}

// parseTypeFlag converts a --type flag value into an entry type,
// rejecting everything that is not one of the supported names.
func parseTypeFlag(value string) (model.EntryType, error) {
	switch value {
	case typeFlagLogin:
		return model.EntryTypeLoginPassword, nil
	case typeFlagText:
		return model.EntryTypeText, nil
	case typeFlagBinary:
		return model.EntryTypeBinary, nil
	case typeFlagCard:
		return model.EntryTypeCard, nil
	default:
		return model.EntryTypeUnspecified,
			fmt.Errorf("invalid type %q: must be one of %s, %s, %s, %s",
				value, typeFlagLogin, typeFlagText, typeFlagBinary, typeFlagCard)
	}
}

// typeLabel returns the user-facing name of an entry type (the TYPE
// table column, get output). Unknown values render as "unknown".
func typeLabel(t model.EntryType) string {
	switch t {
	case model.EntryTypeLoginPassword:
		return typeFlagLogin
	case model.EntryTypeText:
		return typeFlagText
	case model.EntryTypeBinary:
		return typeFlagBinary
	case model.EntryTypeCard:
		return typeFlagCard
	default:
		return "unknown"
	}
}

// encodeLogin serializes a login/password pair into the entry
// payload: JSON {"username":...,"password":...}.
func encodeLogin(username, password string) ([]byte, error) {
	data, err := json.Marshal(loginData{Username: username, Password: password})
	if err != nil {
		return nil, fmt.Errorf("encode login data: %w", err)
	}
	return data, nil
}

// decodeLogin parses a login entry payload produced by encodeLogin.
func decodeLogin(data []byte) (loginData, error) {
	var login loginData
	if err := json.Unmarshal(data, &login); err != nil {
		return login, fmt.Errorf("decode login data: %w", err)
	}
	return login, nil
}

// encodeCard serializes card fields into the entry payload: JSON
// {"number":...,"holder":...,"expiry":...,"cvv":...}.
func encodeCard(number, holder, expiry, cvv string) ([]byte, error) {
	data, err := json.Marshal(cardData{Number: number, Holder: holder, Expiry: expiry, CVV: cvv})
	if err != nil {
		return nil, fmt.Errorf("encode card data: %w", err)
	}
	return data, nil
}

// decodeCard parses a card entry payload produced by encodeCard.
func decodeCard(data []byte) (cardData, error) {
	var card cardData
	if err := json.Unmarshal(data, &card); err != nil {
		return card, fmt.Errorf("decode card data: %w", err)
	}
	return card, nil
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
