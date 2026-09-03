//go:build go1.27

package json

import (
	"errors"
	"strconv"

	"encoding/json/jsontext"
	stdjson "encoding/json/v2"

	"github.com/go-kratos/kratos/v3/encoding"
)

// Name is the name registered for the json codec.
const Name = "json"

var unmarshalOptions = stdjson.WithUnmarshalers(stdjson.JoinUnmarshalers(
	stdjson.UnmarshalFromFunc(unmarshalInt32),
	stdjson.UnmarshalFromFunc(unmarshalInt64),
	stdjson.UnmarshalFromFunc(unmarshalInt32Pointer),
	stdjson.UnmarshalFromFunc(unmarshalInt64Pointer),
))

func init() {
	encoding.RegisterCodec(codec{})
}

// codec is a Codec implementation with json.
type codec struct{}

func (codec) Marshal(v any) ([]byte, error) {
	switch m := v.(type) {
	case stdjson.Marshaler:
		return m.MarshalJSON()
	default:
		return stdjson.Marshal(m)
	}
}

func (codec) Unmarshal(data []byte, v any) error {
	if len(data) == 0 {
		return nil
	}
	switch m := v.(type) {
	case stdjson.Unmarshaler:
		return m.UnmarshalJSON(data)
	default:
		return stdjson.Unmarshal(data, m, unmarshalOptions)
	}
}

func (codec) Name() string {
	return Name
}

func unmarshalInt32(dec *jsontext.Decoder, value *int32) error {
	parsed, ok, err := readNumericString(dec, 32)
	if err != nil || !ok {
		return err
	}
	*value = int32(parsed)
	return nil
}

func unmarshalInt64(dec *jsontext.Decoder, value *int64) error {
	parsed, ok, err := readNumericString(dec, 64)
	if err != nil || !ok {
		return err
	}
	*value = parsed
	return nil
}

func unmarshalInt32Pointer(dec *jsontext.Decoder, value **int32) error {
	parsed, nilValue, ok, err := readNumericStringOrNull(dec, 32)
	if err != nil || !ok {
		return err
	}
	if nilValue {
		*value = nil
		return nil
	}
	parsedValue := int32(parsed)
	*value = &parsedValue
	return nil
}

func unmarshalInt64Pointer(dec *jsontext.Decoder, value **int64) error {
	parsed, nilValue, ok, err := readNumericStringOrNull(dec, 64)
	if err != nil || !ok {
		return err
	}
	if nilValue {
		*value = nil
		return nil
	}
	*value = &parsed
	return nil
}

func readNumericString(dec *jsontext.Decoder, bitSize int) (int64, bool, error) {
	if dec.PeekKind() != '"' {
		return 0, false, errors.ErrUnsupported
	}
	token, err := dec.ReadToken()
	if err != nil {
		return 0, false, err
	}
	parsed, err := strconv.ParseInt(token.String(), 10, bitSize)
	return parsed, true, err
}

func readNumericStringOrNull(dec *jsontext.Decoder, bitSize int) (int64, bool, bool, error) {
	switch dec.PeekKind() {
	case 'n':
		if _, err := dec.ReadToken(); err != nil {
			return 0, false, true, err
		}
		return 0, true, true, nil
	case '"':
		token, err := dec.ReadToken()
		if err != nil {
			return 0, false, true, err
		}
		if token.String() == "" {
			return 0, true, true, nil
		}
		parsed, err := strconv.ParseInt(token.String(), 10, bitSize)
		return parsed, false, true, err
	default:
		return 0, false, false, errors.ErrUnsupported
	}
}
