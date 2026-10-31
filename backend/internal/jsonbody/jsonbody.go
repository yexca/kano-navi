// Package jsonbody bounds the entire body before parsing, including trailing
// whitespace, and rejects ambiguous duplicate keys at every object depth.
package jsonbody

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

const Limit = 32 << 10

var ErrInvalid = errors.New("invalid_json")
var ErrLarge = errors.New("request_too_large")

func Object(w http.ResponseWriter, r *http.Request) (map[string]json.RawMessage, error) {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, Limit))
	if err != nil {
		var large *http.MaxBytesError
		if errors.As(err, &large) {
			return nil, ErrLarge
		}
		return nil, ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(data))
	if err := value(d); err != nil {
		return nil, ErrInvalid
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, ErrInvalid
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(data, &object) != nil || object == nil {
		return nil, ErrInvalid
	}
	return object, nil
}

func value(d *json.Decoder) error {
	token, err := d.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok || seen[name] {
				return ErrInvalid
			}
			seen[name] = true
			if err := value(d); err != nil {
				return err
			}
		}
	case '[':
		for d.More() {
			if err := value(d); err != nil {
				return err
			}
		}
	default:
		return ErrInvalid
	}
	_, err = d.Token()
	return err
}
