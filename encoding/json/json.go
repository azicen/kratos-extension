//go:build go1.27

package json

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"

	"encoding/json/jsontext"
	stdjson "encoding/json/v2"

	"github.com/go-kratos/kratos/v3/encoding"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Name is the name registered for the json codec.
const Name = "json"

var unmarshalOptions = stdjson.JoinOptions(
	stdjson.MatchCaseInsensitiveNames(true),
	stdjson.WithUnmarshalers(stdjson.JoinUnmarshalers(
		stdjson.UnmarshalFromFunc(unmarshalInt32),
		stdjson.UnmarshalFromFunc(unmarshalInt64),
		stdjson.UnmarshalFromFunc(unmarshalInt32Pointer),
		stdjson.UnmarshalFromFunc(unmarshalInt64Pointer),
	)),
)

func init() {
	Register()
}

// Register registers the JSON codec globally.
func Register() {
	encoding.RegisterCodec(codec{})
}

// NewCodec returns a JSON codec.
func NewCodec() encoding.Codec {
	return codec{}
}

// codec is a Codec implementation with json.
type codec struct{}

func (codec) Marshal(v any) ([]byte, error) {
	switch m := v.(type) {
	case proto.Message:
		return (protojson.MarshalOptions{Multiline: false}).Marshal(m)
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
	if message, ok, err := protoTarget(v); err != nil {
		return err
	} else if ok {
		normalized, err := normalizeProtoJSON(data, message.ProtoReflect().Descriptor())
		if err != nil {
			return err
		}
		return (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(normalized, message)
	}
	switch m := v.(type) {
	case stdjson.Unmarshaler:
		return m.UnmarshalJSON(data)
	default:
		return stdjson.Unmarshal(data, m, unmarshalOptions)
	}
}

func protoTarget(v any) (proto.Message, bool, error) {
	if message, ok := v.(proto.Message); ok {
		if isNil(message) {
			return nil, false, errors.New("json: cannot unmarshal into a nil proto message")
		}
		return message, true, nil
	}

	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() || rv.Elem().Kind() != reflect.Pointer {
		return nil, false, nil
	}
	if !rv.Elem().IsNil() {
		message, ok := rv.Elem().Interface().(proto.Message)
		if !ok {
			return nil, false, nil
		}
		return message, true, nil
	}
	elementType := rv.Elem().Type().Elem()
	if !reflect.PointerTo(elementType).Implements(reflect.TypeFor[proto.Message]()) && !elementType.Implements(reflect.TypeFor[proto.Message]()) {
		return nil, false, nil
	}
	allocated := reflect.New(elementType)
	message, ok := allocated.Interface().(proto.Message)
	if !ok {
		return nil, false, nil
	}
	rv.Elem().Set(allocated)
	return message, true, nil
}

func isNil(v any) bool {
	rv := reflect.ValueOf(v)
	return rv.Kind() == reflect.Pointer && rv.IsNil()
}

func normalizeProtoJSON(data []byte, descriptor protoreflect.MessageDescriptor) ([]byte, error) {
	decoder := jsontext.NewDecoder(bytes.NewReader(data))
	value, err := normalizeMessageValue(decoder, descriptor)
	if err != nil {
		return nil, err
	}
	value = append([]byte(nil), value...)
	if _, err := decoder.ReadToken(); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, fmt.Errorf("json: unexpected content after root value")
		}
		return nil, fmt.Errorf("json: invalid content after root value: %w", err)
	}
	return value, nil
}

func normalizeMessageValue(decoder *jsontext.Decoder, descriptor protoreflect.MessageDescriptor) ([]byte, error) {
	if isWellKnown(descriptor) {
		value, err := decoder.ReadValue()
		return value, err
	}
	if decoder.PeekKind() != '{' {
		value, err := decoder.ReadValue()
		return value, err
	}
	if _, err := decoder.ReadToken(); err != nil {
		return nil, err
	}

	var output bytes.Buffer
	output.WriteByte('{')
	first := true
	seenJSONNames := make(map[string]struct{})
	seenFields := make(map[protoreflect.FieldNumber]string)
	for decoder.PeekKind() != '}' {
		nameToken, err := decoder.ReadToken()
		if err != nil {
			return nil, err
		}
		name := nameToken.String()
		if _, exists := seenJSONNames[name]; exists {
			return nil, fmt.Errorf("json: duplicate object member %q", name)
		}
		seenJSONNames[name] = struct{}{}

		field, ambiguous := matchProtoField(descriptor, name)
		if ambiguous {
			return nil, fmt.Errorf("json: ambiguous field name %q", name)
		}
		if field != nil {
			if previous, exists := seenFields[field.Number()]; exists {
				return nil, fmt.Errorf("json: fields %q and %q refer to %s", previous, name, field.FullName())
			}
			seenFields[field.Number()] = name
		}

		if field == nil {
			if err := decoder.SkipValue(); err != nil {
				return nil, err
			}
			continue
		}
		value, omit, err := normalizeFieldValue(decoder, field)
		if err != nil {
			return nil, fmt.Errorf("json: field %q: %w", name, err)
		}
		if omit {
			continue
		}
		if !first {
			output.WriteByte(',')
		}
		first = false
		encodedName, _ := stdjson.Marshal(field.JSONName())
		output.Write(encodedName)
		output.WriteByte(':')
		output.Write(value)
	}
	if _, err := decoder.ReadToken(); err != nil {
		return nil, err
	}
	output.WriteByte('}')
	return output.Bytes(), nil
}

func normalizeFieldValue(decoder *jsontext.Decoder, field protoreflect.FieldDescriptor) ([]byte, bool, error) {
	if field.IsList() {
		value, err := normalizeListValue(decoder, field)
		return value, false, err
	}
	if field.IsMap() {
		value, err := normalizeMapValue(decoder, field.MapValue())
		return value, false, err
	}
	if isSingularNumeric(field) && decoder.PeekKind() == '"' {
		value, err := decoder.ReadValue()
		if err != nil {
			return nil, false, err
		}
		if string(value) == `""` {
			return nil, true, nil
		}
		return value, false, nil
	}
	if (field.Kind() == protoreflect.BoolKind || field.Kind() == protoreflect.EnumKind) && decoder.PeekKind() == '"' {
		value, err := decoder.ReadValue()
		if err != nil {
			return nil, false, err
		}
		if string(value) == `""` {
			return nil, false, errors.New("empty string is not valid for this field")
		}
		return value, false, nil
	}
	if field.Kind() == protoreflect.MessageKind || field.Kind() == protoreflect.GroupKind {
		if decoder.PeekKind() == '"' && !isWellKnown(field.Message()) {
			value, err := decoder.ReadValue()
			if err != nil {
				return nil, false, err
			}
			if string(value) == `""` {
				return nil, false, errors.New("empty string is not valid for a message")
			}
			return value, false, nil
		}
		value, err := normalizeMessageValue(decoder, field.Message())
		return value, false, err
	}
	value, err := decoder.ReadValue()
	return value, false, err
}

func normalizeListValue(decoder *jsontext.Decoder, field protoreflect.FieldDescriptor) ([]byte, error) {
	if decoder.PeekKind() != '[' {
		value, err := decoder.ReadValue()
		return value, err
	}
	if _, err := decoder.ReadToken(); err != nil {
		return nil, err
	}
	var output bytes.Buffer
	output.WriteByte('[')
	first := true
	for decoder.PeekKind() != ']' {
		value, omit, err := normalizeElementValue(decoder, field)
		if err != nil {
			return nil, err
		}
		if omit {
			return nil, errors.New("empty string is not valid in a repeated numeric field")
		}
		if !first {
			output.WriteByte(',')
		}
		first = false
		output.Write(value)
	}
	if _, err := decoder.ReadToken(); err != nil {
		return nil, err
	}
	output.WriteByte(']')
	return output.Bytes(), nil
}

func normalizeMapValue(decoder *jsontext.Decoder, valueField protoreflect.FieldDescriptor) ([]byte, error) {
	if decoder.PeekKind() != '{' {
		value, err := decoder.ReadValue()
		return value, err
	}
	if _, err := decoder.ReadToken(); err != nil {
		return nil, err
	}
	var output bytes.Buffer
	output.WriteByte('{')
	first := true
	seen := make(map[string]struct{})
	for decoder.PeekKind() != '}' {
		key, err := decoder.ReadToken()
		if err != nil {
			return nil, err
		}
		keyName := key.String()
		if _, exists := seen[keyName]; exists {
			return nil, fmt.Errorf("duplicate map key %q", keyName)
		}
		seen[keyName] = struct{}{}
		value, omit, err := normalizeElementValue(decoder, valueField)
		if err != nil {
			return nil, err
		}
		if omit {
			return nil, errors.New("empty string is not valid for a numeric map value")
		}
		if !first {
			output.WriteByte(',')
		}
		first = false
		encodedKey, _ := stdjson.Marshal(keyName)
		output.Write(encodedKey)
		output.WriteByte(':')
		output.Write(value)
	}
	if _, err := decoder.ReadToken(); err != nil {
		return nil, err
	}
	output.WriteByte('}')
	return output.Bytes(), nil
}

func normalizeElementValue(decoder *jsontext.Decoder, field protoreflect.FieldDescriptor) ([]byte, bool, error) {
	if field.Kind() == protoreflect.MessageKind || field.Kind() == protoreflect.GroupKind {
		value, err := normalizeMessageValue(decoder, field.Message())
		return value, false, err
	}
	if isNumericKind(field.Kind()) && decoder.PeekKind() == '"' {
		value, err := decoder.ReadValue()
		return value, string(value) == `""`, err
	}
	value, err := decoder.ReadValue()
	return value, false, err
}

func matchProtoField(descriptor protoreflect.MessageDescriptor, name string) (protoreflect.FieldDescriptor, bool) {
	fields := descriptor.Fields()
	for i := 0; i < fields.Len(); i++ {
		field := fields.Get(i)
		if field.JSONName() == name {
			return field, false
		}
	}
	for i := 0; i < fields.Len(); i++ {
		field := fields.Get(i)
		if string(field.Name()) == name {
			return field, false
		}
	}
	normalized := normalizeName(name)
	var match protoreflect.FieldDescriptor
	for i := 0; i < fields.Len(); i++ {
		field := fields.Get(i)
		if normalizeName(field.JSONName()) != normalized && normalizeName(string(field.Name())) != normalized {
			continue
		}
		if match != nil && match.Number() != field.Number() {
			return nil, true
		}
		match = field
	}
	return match, false
}

func normalizeName(name string) string {
	var output strings.Builder
	output.Grow(len(name))
	for i := 0; i < len(name); i++ {
		character := name[i]
		if character == '_' || character == '-' {
			continue
		}
		if character >= 'A' && character <= 'Z' {
			character += 'a' - 'A'
		}
		output.WriteByte(character)
	}
	return output.String()
}

func isSingularNumeric(field protoreflect.FieldDescriptor) bool {
	if isNumericKind(field.Kind()) {
		return true
	}
	return field.Kind() == protoreflect.MessageKind && isNumericWrapper(field.Message())
}

func isNumericKind(kind protoreflect.Kind) bool {
	switch kind {
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind,
		protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind,
		protoreflect.Uint32Kind, protoreflect.Fixed32Kind, protoreflect.Uint64Kind,
		protoreflect.Fixed64Kind, protoreflect.FloatKind, protoreflect.DoubleKind:
		return true
	default:
		return false
	}
}

func isNumericWrapper(descriptor protoreflect.MessageDescriptor) bool {
	switch descriptor.FullName() {
	case "google.protobuf.DoubleValue", "google.protobuf.FloatValue", "google.protobuf.Int64Value",
		"google.protobuf.UInt64Value", "google.protobuf.Int32Value", "google.protobuf.UInt32Value":
		return true
	default:
		return false
	}
}

func isWellKnown(descriptor protoreflect.MessageDescriptor) bool {
	switch descriptor.FullName() {
	case "google.protobuf.Any",
		"google.protobuf.Timestamp",
		"google.protobuf.Duration",
		"google.protobuf.Struct",
		"google.protobuf.Value",
		"google.protobuf.ListValue",
		"google.protobuf.FieldMask",
		"google.protobuf.DoubleValue",
		"google.protobuf.FloatValue",
		"google.protobuf.Int64Value",
		"google.protobuf.UInt64Value",
		"google.protobuf.Int32Value",
		"google.protobuf.UInt32Value",
		"google.protobuf.BoolValue",
		"google.protobuf.StringValue",
		"google.protobuf.BytesValue",
		"google.protobuf.Empty":
		return true
	default:
		return false
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
