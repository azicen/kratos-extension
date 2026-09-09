package uuid

import (
	jsonv1 "encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"

	"github.com/google/uuid"
)

// MarshalJSON 实现 json v1 编码接口。
func (x *UUID) MarshalJSON() ([]byte, error) {
	if len(x.Value) == 0 {
		return jsonv1.Marshal("")
	}
	uid, err := uuid.FromBytes(x.Value)
	if err != nil {
		return nil, err
	}
	return jsonv1.Marshal(uid.String())
}

// UnmarshalJSON 实现 json v1 解码接口。
func (x *UUID) UnmarshalJSON(data []byte) error {
	var value *string
	if err := jsonv1.Unmarshal(data, &value); err != nil {
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
	x.Value = uid[:]
	return nil
}

// MarshalJSONV2 实现 json/v2 流式编码接口
func (x *UUID) MarshalJSONV2(enc *jsontext.Encoder, opts jsonv2.Options) error {
	if len(x.Value) == 0 {
		return enc.WriteToken(jsontext.String(""))
	}
	uid, err := uuid.FromBytes(x.Value)
	if err != nil {
		return err // 字节不合法时抛出错误
	}
	return enc.WriteToken(jsontext.String(uid.String()))
}

// UnmarshalJSONV2 实现 json/v2 流式解码接口
func (x *UUID) UnmarshalJSONV2(dec *jsontext.Decoder, opts jsonv2.Options) error {
	tok, err := dec.ReadToken()
	if err != nil {
		return err
	}
	s := tok.String()
	if s == "" {
		return nil
	}
	uid, err := uuid.Parse(s)
	if err != nil {
		return err
	}
	x.Value = uid[:]
	return nil
}

// Unwrap 拆包
func (x *UUID) Unwrap() uuid.UUID {
	if x == nil || len(x.Value) == 0 {
		return uuid.Nil
	}
	uid, _ := uuid.FromBytes(x.Value)
	return uid
}

// Wrap 包装
func Wrap(uid uuid.UUID) *UUID {
	return &UUID{
		Value: uid[:],
	}
}
