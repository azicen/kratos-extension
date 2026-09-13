package uuid

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"uuid"
)

var (
	_ json.Marshaler       = (*UUID)(nil)
	_ json.Unmarshaler     = (*UUID)(nil)
	_ json.MarshalerTo     = (*UUID)(nil)
	_ json.UnmarshalerFrom = (*UUID)(nil)
)

// MarshalJSON 实现 json/v2 兼容编码接口
func (x *UUID) MarshalJSON() ([]byte, error) {
	if len(x.Data) == 0 {
		return json.Marshal("")
	}
	if len(x.Data) != len(uuid.UUID{}) {
		return nil, errors.New("uuid: value must be exactly 16 bytes")
	}
	return json.Marshal(uuid.UUID(x.Data).String())
}

// UnmarshalJSON 实现 json/v2 兼容解码接口
func (x *UUID) UnmarshalJSON(data []byte) error {
	var value *string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	if value == nil {
		return errors.New("uuid: JSON value must be a string")
	}
	if *value == "" {
		return nil
	}
	uid, err := uuid.Parse(*value)
	if err != nil {
		return err
	}
	x.Data = append(x.Data[:0], uid[:]...)
	return nil
}

// MarshalJSONTo 实现 json/v2 流式编码接口
func (x *UUID) MarshalJSONTo(enc *jsontext.Encoder) error {
	if len(x.Data) == 0 {
		return enc.WriteToken(jsontext.String(""))
	}
	if len(x.Data) != len(uuid.UUID{}) {
		return errors.New("uuid: value must be exactly 16 bytes")
	}
	uid := uuid.UUID(x.Data)
	return enc.WriteToken(jsontext.String(uid.String()))
}

// UnmarshalJSONFrom 实现 json/v2 流式解码接口
func (x *UUID) UnmarshalJSONFrom(dec *jsontext.Decoder) error {
	tok, err := dec.ReadToken()
	if err != nil {
		return err
	}
	if tok.Kind() != '"' {
		return errors.New("uuid: JSON value must be a string")
	}
	s := tok.String()
	if s == "" {
		return nil
	}
	uid, err := uuid.Parse(s)
	if err != nil {
		return err
	}
	x.Data = append(x.Data[:0], uid[:]...)
	return nil
}

// Unwrap 拆包
func (x *UUID) Unwrap() uuid.UUID {
	if x == nil || len(x.Data) == 0 {
		return uuid.Nil()
	}
	if len(x.Data) != len(uuid.UUID{}) {
		return uuid.Nil()
	}
	return uuid.UUID(x.Data)
}

// Wrap 包装
func Wrap(uid uuid.UUID) *UUID {
	return &UUID{
		Data: append([]byte(nil), uid[:]...),
	}
}
