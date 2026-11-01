// Package pagination encodes opaque cursors (base64url JSON). Cursors carry
// only sort keys; every value is passed to SQL as a bound parameter, so a
// tampered cursor can at worst return a different page of public data.
package pagination

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
)

const maxLen = 300

var ErrInvalid = errors.New("invalid cursor")

func Encode(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Decode rejects oversized, malformed or unknown-field cursors.
func Decode(s string, dst any) error {
	if s == "" || len(s) > maxLen {
		return ErrInvalid
	}
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return ErrInvalid
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil || dec.More() {
		return ErrInvalid
	}
	return nil
}
