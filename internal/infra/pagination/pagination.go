// Package pagination encodes opaque keyset cursors for list endpoints.
package pagination

import (
	"encoding/base64"
	"encoding/json"
	"errors"
)

var ErrInvalidCursor = errors.New("invalid pagination cursor")

type cursorValue struct {
	Sort string `json:"s"`
	ID   string `json:"i"`
}

// serializes the sort key + tie-breaker of the last returned row.
func Encode(sortKey, id string) string {
	encoded, err := json.Marshal(cursorValue{Sort: sortKey, ID: id})
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(encoded)
}

// pulls the sort key + tie-breaker back out of an opaque cursor.
func Decode(raw string) (string, string, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return "", "", ErrInvalidCursor
	}
	var value cursorValue
	if err := json.Unmarshal(decoded, &value); err != nil || value.Sort == "" || value.ID == "" {
		return "", "", ErrInvalidCursor
	}
	return value.Sort, value.ID, nil
}
